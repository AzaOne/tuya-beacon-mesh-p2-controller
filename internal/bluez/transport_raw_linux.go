//go:build linux

package bluez

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
)

const hciChannelRaw = 0

type rawHCI struct {
	fd       int
	extended bool
}

// Linux struct hci_filter has four-byte alignment and therefore sizeof=16:
// type_mask uint32, event_mask[2] uint32, opcode uint16, 2 bytes padding.
// The kernel copies the full sizeof(struct hci_ufilter) from setsockopt; using
// 14 bytes fails with EINVAL on current Linux kernels.
const (
	hciSOL             = 0
	hciFilterOption    = 2
	hciFilterStructLen = 16
)

func rawHCIFilter(opcode uint16) [hciFilterStructLen]byte {
	var filter [hciFilterStructLen]byte
	binary.LittleEndian.PutUint32(filter[0:4], 1<<4)                // HCI_EVENT_PKT
	binary.LittleEndian.PutUint32(filter[4:8], (1<<0x0e)|(1<<0x0f)) // Command Complete, Command Status
	binary.LittleEndian.PutUint16(filter[12:14], opcode)
	return filter
}

func setRawHCIFilter(fd int, opcode uint16) error {
	filter := rawHCIFilter(opcode)
	return syscall.SetsockoptString(fd, hciSOL, hciFilterOption, string(filter[:]))
}

func newRawHCI(index int) (*rawHCI, error) {
	if index < 0 || index > 65534 {
		return nil, errors.New("invalid HCI adapter index")
	}
	fd, err := syscall.Socket(afBluetooth, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, btProtoHCI)
	if err != nil {
		return nil, fmt.Errorf("open HCI raw socket: %w (CAP_NET_RAW/root required)", err)
	}
	addr := rawHCISockaddr{Family: afBluetooth, Dev: uint16(index), Channel: hciChannelRaw}
	_, _, errno := syscall.Syscall(syscall.SYS_BIND, uintptr(fd), uintptr(unsafe.Pointer(&addr)), unsafe.Sizeof(addr))
	if errno != 0 {
		syscall.Close(fd)
		return nil, fmt.Errorf("bind raw hci%d: %w", index, errno)
	}
	// HCI_FILTER expects the native 16-byte struct (including 2 bytes of
	// alignment padding after opcode), not the 14 bytes of field data.
	if err := setRawHCIFilter(fd, 0); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("HCI filter: %w", err)
	}
	if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &syscall.Timeval{Sec: 5}); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	return &rawHCI{fd: fd}, nil
}

func (h *rawHCI) close() { _ = syscall.Close(h.fd) }

func (h *rawHCI) command(ocf uint16, params []byte, ignoreDisallowed bool) ([]byte, error) {
	if len(params) > 255 {
		return nil, errors.New("HCI payload too long")
	}
	opcode := uint16(0x2000) | ocf
	// HCI socket filters must match the current command opcode. An opcode=0
	// filter hides Command Complete events on Linux; set it for every OCF.
	if err := setRawHCIFilter(h.fd, opcode); err != nil {
		return nil, fmt.Errorf("HCI opcode filter 0x%04x: %w", opcode, err)
	}
	request := append([]byte{0x01, byte(opcode), byte(opcode >> 8), byte(len(params))}, params...)
	n, err := syscall.Write(h.fd, request)
	if err != nil {
		return nil, fmt.Errorf("HCI command 0x%04x: %w", opcode, err)
	}
	if n != len(request) {
		return nil, errors.New("short HCI command write")
	}
	packet := make([]byte, 512)
	for tries := 0; tries < 64; tries++ {
		n, err := syscall.Read(h.fd, packet)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("HCI response 0x%04x: %w", opcode, err)
		}
		b := packet[:n]
		if len(b) < 7 || b[0] != 0x04 {
			continue
		}
		if b[1] == 0x0e && binary.LittleEndian.Uint16(b[4:6]) == opcode {
			status := b[6]
			if status != 0 && !(ignoreDisallowed && status == 0x0c) {
				return nil, fmt.Errorf("HCI 0x%04x status 0x%02x", opcode, status)
			}
			return append([]byte(nil), b[7:]...), nil
		}
		if b[1] == 0x0f && binary.LittleEndian.Uint16(b[5:7]) == opcode {
			status := b[3]
			if status != 0 && !(ignoreDisallowed && status == 0x0c) {
				return nil, fmt.Errorf("HCI 0x%04x status 0x%02x", opcode, status)
			}
			return nil, nil
		}
	}
	return nil, fmt.Errorf("HCI response timeout 0x%04x", opcode)
}

func (h *rawHCI) init() error {
	// Enable may already be running; the helper is intentionally a dedicated
	// adapter operation. Never issue a Bluetooth adapter reset here.
	if _, err := h.command(0x03, nil, false); err != nil {
		return err
	} // LE Read Local Supported Features
	sets, err := h.command(0x3b, nil, false) // LE Read Number of Supported Advertising Sets
	h.extended = err == nil && len(sets) > 0 && sets[0] > 0
	return nil
}

func (h *rawHCI) stop() error {
	if h.extended {
		_, err := h.command(0x39, []byte{0, 0}, true)
		return err
	}
	_, err := h.command(0x0a, []byte{0}, true)
	return err
}

func (h *rawHCI) begin(adv []byte, interval time.Duration) error {
	if len(adv) != 31 || !bytes.Equal(adv[:2], []byte{2, 1}) || !bytes.Equal(adv[3:5], []byte{0x1b, 0x03}) {
		return errors.New("expected 31-byte Tuya legacy advertising packet")
	}
	if interval < 20*time.Millisecond || interval > time.Second || interval%(625*time.Microsecond) != 0 {
		return errors.New("invalid HCI advertising interval")
	}
	slots := uint32(interval / (625 * time.Microsecond))
	address := make([]byte, 6)
	if _, err := rand.Read(address); err != nil {
		return err
	}
	address[5] = (address[5] & 0x3f) | 0xc0 // BLE static random address, little-endian
	if h.extended {
		params := []byte{1, 0x13, 0, 0, 0, 0, 0, 0, 0, 7, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0x7f, 1, 0, 1, 0, 0}
		params[3], params[4], params[5] = byte(slots), byte(slots>>8), byte(slots>>16)
		params[6], params[7], params[8] = byte(slots), byte(slots>>8), byte(slots>>16)
		if _, err := h.command(0x36, params, false); err != nil {
			return fmt.Errorf("LE Set Extended Advertising Parameters: %w", err)
		}
		if _, err := h.command(0x35, append([]byte{1}, address...), false); err != nil {
			return err
		}
		if _, err := h.command(0x37, append([]byte{1, 3, 1, 31}, adv...), false); err != nil {
			return err
		}
		if _, err := h.command(0x39, []byte{1, 1, 1, 0, 0, 0}, false); err != nil {
			return err
		}
	} else {
		if _, err := h.command(0x05, address, false); err != nil {
			return err
		}
		params := []byte{0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 7, 0}
		binary.LittleEndian.PutUint16(params[0:2], uint16(slots))
		binary.LittleEndian.PutUint16(params[2:4], uint16(slots))
		if _, err := h.command(0x06, params, false); err != nil {
			return err
		}
		if _, err := h.command(0x09, make([]byte, 32), false); err != nil {
			return err
		}
		if _, err := h.command(0x08, append([]byte{31}, adv...), false); err != nil {
			return err
		}
		if _, err := h.command(0x0a, []byte{1}, false); err != nil {
			return err
		}
	}
	return nil
}

func runRawHCISendHelper(args []string) error {
	if len(args) != 5 {
		return errors.New("internal RAW HCI helper arguments")
	}
	values := make([]int, 5)
	for i, s := range args {
		n, e := strconv.Atoi(s)
		if e != nil {
			return e
		}
		values[i] = n
	}
	index, dwell, gap, expected, slots := values[0], values[1], values[2], values[3], values[4]
	if dwell < 100 || dwell > 10000 || gap < 0 || gap > 5000 || expected < 1 || expected > 100 || slots < 32 || slots > 1600 {
		return errors.New("invalid RAW HCI helper parameters")
	}
	bytesIn, err := io.ReadAll(io.LimitReader(os.Stdin, int64(expected*31+1)))
	if err != nil {
		return err
	}
	if len(bytesIn) != expected*31 {
		return errors.New("invalid frame input size")
	}
	h, err := newRawHCI(index)
	if err != nil {
		return err
	}
	defer h.close()
	if err := h.init(); err != nil {
		return err
	}
	// Raw HCI LE commands change controller-wide advertising state. Do not
	// leave advertising active on errors, interrupts or normal completion.
	if err := h.stop(); err != nil {
		return err
	}
	defer h.stop()
	interval := time.Duration(slots) * (625 * time.Microsecond)
	fmt.Printf("Raw HCI mode=%s adv_interval=%s\n", map[bool]string{true: "extended", false: "legacy"}[h.extended], interval)
	for i := 0; i < expected; i++ {
		if i > 0 {
			if err := h.stop(); err != nil {
				return err
			}
		}
		if err := h.begin(bytesIn[i*31:(i+1)*31], interval); err != nil {
			return fmt.Errorf("frame %d/%d: %w", i+1, expected, err)
		}
		fmt.Printf("Raw HCI: active frame=%d/%d\n", i+1, expected)
		time.Sleep(time.Duration(dwell) * time.Millisecond)
		if i+1 < expected && gap > 0 {
			time.Sleep(time.Duration(gap) * time.Millisecond)
		}
	}
	return h.stop()
}

func canSendRawHCI() bool {
	if os.Geteuid() == 0 {
		return true
	}
	status, _ := os.ReadFile("/proc/self/status")
	for _, line := range strings.Split(string(status), "\n") {
		if value, ok := strings.CutPrefix(line, "CapEff:"); ok {
			mask, err := strconv.ParseUint(strings.TrimSpace(value), 16, 64)
			const capNetRaw = 1 << 13
			return err == nil && mask&capNetRaw != 0
		}
	}
	return false
}

func sendAllRawHCI(ctx context.Context, adapter string, frames []p2.WireFrame, dwell, gap, interval time.Duration) error {
	index, err := ParseAdapterIndex(adapter)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"__raw-hci-send", strconv.Itoa(index), strconv.FormatInt(dwell.Milliseconds(), 10), strconv.FormatInt(gap.Milliseconds(), 10), strconv.Itoa(len(frames)), strconv.FormatInt(int64(interval/(625*time.Microsecond)), 10)}
	if !canSendRawHCI() {
		args = append([]string{"-n", exe}, args...)
		exe = "sudo"
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	var input bytes.Buffer
	for _, fr := range frames {
		input.Write(fr.Adv[:])
	}
	cmd.Stdin = &input
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Raw HCI helper: %w", err)
	}
	return nil
}
