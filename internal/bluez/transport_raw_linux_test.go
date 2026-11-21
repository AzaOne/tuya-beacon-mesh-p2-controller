//go:build linux

package bluez

import (
	"encoding/binary"
	"testing"
	"unsafe"
)

func TestRawHCIFilterStructABI(t *testing.T) {
	// Match the C ABI in <bluetooth/hci.h> (trailing 2-byte alignment).
	type linuxHCIFilter struct {
		TypeMask  uint32
		EventMask [2]uint32
		Opcode    uint16
	}
	if got, want := int(unsafe.Sizeof(linuxHCIFilter{})), hciFilterStructLen; got != want {
		t.Fatalf("native HCI_FILTER size: %d, want %d", got, want)
	}
	for _, opcode := range []uint16{0, 0x2003, 0x2036, 0x2039} {
		f := rawHCIFilter(opcode)
		if len(f) != hciFilterStructLen {
			t.Fatalf("length: %d", len(f))
		}
		if got := binary.LittleEndian.Uint32(f[0:4]); got != 1<<4 {
			t.Errorf("type mask 0x%x", got)
		}
		if got := binary.LittleEndian.Uint32(f[4:8]); got != (1<<0x0e)|(1<<0x0f) {
			t.Errorf("event mask 0x%x", got)
		}
		if got := binary.LittleEndian.Uint32(f[8:12]); got != 0 {
			t.Errorf("second event mask 0x%x", got)
		}
		if got := binary.LittleEndian.Uint16(f[12:14]); got != opcode {
			t.Errorf("opcode got 0x%x, want 0x%x", got, opcode)
		}
		if f[14] != 0 || f[15] != 0 {
			t.Errorf("nonzero tail padding: %x", f[14:])
		}
	}
}
