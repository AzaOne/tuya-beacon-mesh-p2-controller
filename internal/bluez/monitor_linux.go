//go:build linux

package bluez

import (
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"syscall"
	"unsafe"
)

const hciProtocol = 1

const hciMonitorChannel = 2

const hciDevNone = 0xffff

type rawSockaddrHCI struct {
	Family  uint16
	Dev     uint16
	Channel uint16
}

// Hidden privileged helper: emits length-prefixed Linux HCI monitor datagrams.
// No key/config/state access here; only the receiver requires CAP_NET_RAW.
func runHCIMonitorHelper(indexArg string) error {
	// Exit when sudo (or the original caller if sudo execs directly) dies.
	// This avoids leaving a privileged HCI reader orphaned after Ctrl+C.
	const prSetPdeathsig = 1
	_, _, _ = syscall.RawSyscall6(syscall.SYS_PRCTL, prSetPdeathsig, uintptr(syscall.SIGTERM), 0, 0, 0, 0)
	if os.Getppid() == 1 {
		return fmt.Errorf("HCI reader parent already exited")
	}
	index, err := strconv.Atoi(indexArg)
	if err != nil || index < 0 || index > 65534 {
		return fmt.Errorf("invalid HCI index: %q", indexArg)
	}
	fd, err := syscall.Socket(syscall.AF_BLUETOOTH, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, hciProtocol)
	if err != nil {
		return fmt.Errorf("open AF_BLUETOOTH HCI monitor (need root/CAP_NET_RAW): %w", err)
	}
	defer syscall.Close(fd)
	addr := rawSockaddrHCI{Family: syscall.AF_BLUETOOTH, Dev: hciDevNone, Channel: hciMonitorChannel}
	_, _, errno := syscall.RawSyscall(syscall.SYS_BIND, uintptr(fd), uintptr(unsafe.Pointer(&addr)), unsafe.Sizeof(addr))
	if errno != 0 {
		return fmt.Errorf("bind HCI monitor socket: %w", errno)
	}
	if _, err := os.Stdout.Write([]byte(MonitorReady)); err != nil {
		return err
	}
	buf := make([]byte, MaxMonitorPacket)
	for {
		n, err := syscall.Read(fd, buf)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if n < 6 {
			continue
		}
		if int(binary.LittleEndian.Uint16(buf[2:4])) != index ||
			binary.LittleEndian.Uint16(buf[0:2]) != 3 || // HCI_MON_EVENT_PKT
			n < 10 || buf[6] != 0x3e || // LE Meta Event
			(buf[8] != 0x02 && buf[8] != 0x0d) { // adv reports only
			continue
		}
		var h [4]byte
		binary.BigEndian.PutUint32(h[:], uint32(n))
		if _, err := os.Stdout.Write(h[:]); err != nil {
			return err
		}
		if _, err := os.Stdout.Write(buf[:n]); err != nil {
			return err
		}
	}
}
