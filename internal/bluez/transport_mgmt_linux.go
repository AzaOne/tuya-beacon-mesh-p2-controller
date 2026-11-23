//go:build linux

package bluez

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
)

const (
	afBluetooth              = 31
	btProtoHCI               = 1
	hciChannelControl        = 3
	mgmtAddAdvertising       = 0x003e
	mgmtRemoveAdvertising    = 0x003f
	mgmtReadAdvFeatures      = 0x003d
	mgmtAddExtendedParams    = 0x0054
	mgmtAddExtendedData      = 0x0055
	mgmtEventCommandComplete = 0x0001
	mgmtEventCommandStatus   = 0x0002
)

type rawHCISockaddr struct{ Family, Dev, Channel uint16 }

type mgmtSocket struct {
	fd    int
	index uint16
}

func newMGMT(index int) (*mgmtSocket, error) {
	if index < 0 || index > 65534 {
		return nil, errors.New("invalid adapter index")
	}
	fd, err := syscall.Socket(afBluetooth, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, btProtoHCI)
	if err != nil {
		return nil, fmt.Errorf("Bluetooth HCI MGMT socket: %w (root/CAP_NET_ADMIN required)", err)
	}
	addr := rawHCISockaddr{Family: afBluetooth, Dev: hciDevNone, Channel: hciChannelControl}
	_, _, errno := syscall.Syscall(syscall.SYS_BIND, uintptr(fd), uintptr(unsafe.Pointer(&addr)), unsafe.Sizeof(addr))
	if errno != 0 {
		syscall.Close(fd)
		return nil, fmt.Errorf("bind HCI_CHANNEL_CONTROL: %w", errno)
	}
	timeout := syscall.Timeval{Sec: 5}
	_ = syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &timeout)
	return &mgmtSocket{fd: fd, index: uint16(index)}, nil
}

func (m *mgmtSocket) Close() { _ = syscall.Close(m.fd) }

func (m *mgmtSocket) call(op uint16, data []byte) ([]byte, error) {
	if len(data) > 65535 {
		return nil, errors.New("MGMT payload too big")
	}
	wire := make([]byte, 6+len(data))
	binary.LittleEndian.PutUint16(wire[:2], op)
	binary.LittleEndian.PutUint16(wire[2:4], m.index)
	binary.LittleEndian.PutUint16(wire[4:6], uint16(len(data)))
	copy(wire[6:], data)
	n, err := syscall.Write(m.fd, wire)
	if err != nil {
		return nil, fmt.Errorf("MGMT write opcode %04x: %w", op, err)
	}
	if n != len(wire) {
		return nil, fmt.Errorf("MGMT short write %d/%d", n, len(wire))
	}
	buf := make([]byte, 65535)
	for attempts := 0; attempts < 100; attempts++ {
		n, err := syscall.Read(m.fd, buf)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("MGMT wait opcode %04x: %w", op, err)
		}
		if n < 6 {
			continue
		}
		event := binary.LittleEndian.Uint16(buf[:2])
		idx := binary.LittleEndian.Uint16(buf[2:4])
		size := int(binary.LittleEndian.Uint16(buf[4:6]))
		if idx != m.index || size != n-6 {
			continue
		}
		pl := buf[6:n]
		if event != mgmtEventCommandComplete && event != mgmtEventCommandStatus {
			continue
		}
		if len(pl) < 3 || binary.LittleEndian.Uint16(pl[:2]) != op {
			continue
		}
		if pl[2] != 0 {
			return nil, fmt.Errorf("MGMT %04x rejected, status=0x%02x", op, pl[2])
		}
		if event == mgmtEventCommandStatus {
			return []byte{}, nil
		}
		return append([]byte(nil), pl[3:]...), nil
	}
	return nil, fmt.Errorf("MGMT response %04x not received", op)
}

func (m *mgmtSocket) chooseInstance() (byte, error) {
	result, err := m.call(mgmtReadAdvFeatures, nil)
	if err != nil {
		return 0, err
	}
	if len(result) < 8 {
		return 0, fmt.Errorf("short MGMT advertising features: %x", result)
	}
	max := int(result[6])
	count := int(result[7])
	if max == 0 {
		return 0, errors.New("adapter supports no advertising instances")
	}
	if len(result) < 8+count {
		return 0, errors.New("truncated MGMT advertising instance list")
	}
	used := make(map[byte]bool, count)
	for _, id := range result[8 : 8+count] {
		used[id] = true
	}
	for i := 1; i <= max && i < 255; i++ {
		if !used[byte(i)] {
			return byte(i), nil
		}
	}
	return 0, errors.New("no free BlueZ advertising instance; will not overwrite another application")
}

func (m *mgmtSocket) add(instance byte, adv []byte) error {
	if len(adv) != 31 || !bytes.Equal(adv[3:5], []byte{0x1b, 0x03}) {
		return errors.New("expected exact Tuya legacy 31-byte ADV")
	}
	// MGMT flags=bit0 connectable, NO auto flags insertion. The 31-byte
	// advertising data already contains the complete 02 01 xx 1b 03 ... ADs.
	// Duration/Timeout=0: manually remove instance at each dwell boundary.
	payload := make([]byte, 11+len(adv))
	payload[0] = instance
	binary.LittleEndian.PutUint32(payload[1:5], 1)
	payload[9] = byte(len(adv))
	payload[10] = 0 // scan response len
	copy(payload[11:], adv)
	result, err := m.call(mgmtAddAdvertising, payload)
	if err != nil {
		return err
	}
	if len(result) < 1 || result[0] != instance {
		return fmt.Errorf("MGMT add response unexpected: %x", result)
	}
	return nil
}

func extendedAdvParams(instance byte, interval time.Duration) []byte {
	// Linux MGMT ignores the interval fields unless MGMT_ADV_PARAM_INTERVALS is set.
	const connectable = 1 << 0
	const intervals = 1 << 14
	params := make([]byte, 18)
	params[0] = instance
	binary.LittleEndian.PutUint32(params[1:5], connectable|intervals)
	slots := uint32(interval / (625 * time.Microsecond))
	binary.LittleEndian.PutUint32(params[9:13], slots)
	binary.LittleEndian.PutUint32(params[13:17], slots)
	return params
}

// addFast uses BlueZ MGMT 0x0054/0x0055 and forces a short advertising
// event interval. The legacy 0x003e operation above does not allow interval
// selection and may inherit the kernel's ~1.28 second default.
// This is still a legacy 31-byte on-air payload, not a long/secondary-PHY PDU.
func (m *mgmtSocket) addFast(instance byte, adv []byte, interval time.Duration) error {
	if len(adv) != 31 || !bytes.Equal(adv[:2], []byte{0x02, 0x01}) || !bytes.Equal(adv[3:5], []byte{0x1b, 0x03}) {
		return errors.New("expected exact 31-byte Tuya advertising payload")
	}
	if interval < 20*time.Millisecond || interval > time.Second || interval%(625*time.Microsecond) != 0 {
		return errors.New("invalid advertising interval")
	}
	params := extendedAdvParams(instance, interval)
	result, err := m.call(mgmtAddExtendedParams, params)
	if err != nil {
		return fmt.Errorf("MGMT Add Extended Advertising Parameters: %w", err)
	}
	if len(result) < 4 || result[0] != instance || result[2] < 31 {
		_ = m.remove(instance)
		return fmt.Errorf("MGMT extended advertising payload capacity too small: %x", result)
	}
	data := append([]byte{instance, byte(len(adv)), 0}, adv...)
	response, err := m.call(mgmtAddExtendedData, data)
	if err != nil {
		_ = m.remove(instance)
		return fmt.Errorf("MGMT Add Extended Advertising Data: %w", err)
	}
	if len(response) < 1 || response[0] != instance {
		_ = m.remove(instance)
		return fmt.Errorf("unexpected MGMT extended data response: %x", response)
	}
	return nil
}

func (m *mgmtSocket) remove(instance byte) error {
	_, err := m.call(mgmtRemoveAdvertising, []byte{instance})
	return err
}

func runMGMTSendHelper(args []string) error {
	if len(args) != 5 {
		return errors.New("internal MGMT helper arguments")
	}
	index, err := strconv.Atoi(args[0])
	if err != nil {
		return err
	}
	ms, err := strconv.Atoi(args[1])
	if err != nil || ms < 100 || ms > 10000 {
		return errors.New("bad dwell ms")
	}
	gap, err := strconv.Atoi(args[2])
	if err != nil || gap < 0 || gap > 5000 {
		return errors.New("bad gap ms")
	}
	intervalSlots, err := strconv.Atoi(args[4])
	if err != nil || (intervalSlots != 0 && (intervalSlots < 32 || intervalSlots > 1600)) {
		return errors.New("invalid advertising interval slots")
	}
	expected, err := strconv.Atoi(args[3])
	if err != nil || expected < 1 || expected > 100 {
		return errors.New("bad frame count")
	}
	sock, err := newMGMT(index)
	if err != nil {
		return err
	}
	defer sock.Close()
	instance, err := sock.chooseInstance()
	if err != nil {
		return err
	}
	// Pipe is bounded (max 100 frames). Nothing in this helper handles keys
	// or accesses the user's state/config.
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, int64(expected*31+1)))
	if err != nil {
		return err
	}
	if len(raw) != expected*31 {
		return fmt.Errorf("expected %d ADV bytes, got %d", expected*31, len(raw))
	}
	active := false
	defer func() {
		if active {
			_ = sock.remove(instance)
		}
	}()
	for i := 0; i < expected; i++ {
		if active {
			if err := sock.remove(instance); err != nil {
				return fmt.Errorf("remove prior advertising: %w", err)
			}
			active = false
		}
		var addErr error
		if intervalSlots == 0 {
			addErr = sock.add(instance, raw[i*31:(i+1)*31])
		} else {
			addErr = sock.addFast(instance, raw[i*31:(i+1)*31], time.Duration(intervalSlots)*(625*time.Microsecond))
		}
		if addErr != nil {
			return fmt.Errorf("frame %d/%d: %w", i+1, expected, addErr)
		}
		active = true
		fmt.Printf("MGMT: active instance=%d frame=%d/%d interval=%s\n", instance, i+1, expected, time.Duration(intervalSlots)*(625*time.Microsecond))
		time.Sleep(time.Duration(ms) * time.Millisecond)
		if i+1 < expected && gap > 0 {
			time.Sleep(time.Duration(gap) * time.Millisecond)
		}
	}
	if err := sock.remove(instance); err != nil {
		return err
	}
	active = false
	return nil
}

func sendAllMGMT(ctx context.Context, adapter string, frames []p2.WireFrame, dwell, gap, interval time.Duration) error {
	index, err := ParseAdapterIndex(adapter)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if _, err := exec.LookPath("sudo"); err != nil {
		return fmt.Errorf("sudo not installed: %w", err)
	}
	// Keep state/config processing unprivileged. A single root helper sends
	// all frames using ONE long-lived MGMT socket.
	cmd := exec.CommandContext(ctx, "sudo", "-n", exe, "__mgmt-send", strconv.Itoa(index), strconv.FormatInt(dwell.Milliseconds(), 10), strconv.FormatInt(gap.Milliseconds(), 10), strconv.Itoa(len(frames)), strconv.FormatInt(int64(interval/(625*time.Microsecond)), 10))
	var input bytes.Buffer
	for _, f := range frames {
		input.Write(f.Adv[:])
	}
	cmd.Stdin = &input
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("BlueZ MGMT helper: %w (try sudo -v or --transport btmgmt)", err)
	}
	return nil
}
