package capture

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestSequenceFromBTSnoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.cfa")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte("btsnoop\x00\x00\x00\x00\x01\x00\x00\x03\xe9"))
	for _, seq := range []uint16{0x1234, 0x1236, 0x1235} {
		b := make([]byte, 26)
		b[0] = 0x0b
		binary.BigEndian.PutUint16(b[1:3], 0x7a59)
		binary.BigEndian.PutUint16(b[3:5], 0x0003)
		binary.BigEndian.PutUint16(b[5:7], seq)
		b[7] = 0x05
		adv := append([]byte{2, 1, 1, 27, 3}, b...)
		raw := append([]byte{1, 0x37, 0x20, byte(4 + len(adv)), 0, 3, 0, byte(len(adv))}, adv...)
		var hdr [24]byte
		binary.BigEndian.PutUint32(hdr[0:4], uint32(len(raw)))
		binary.BigEndian.PutUint32(hdr[4:8], uint32(len(raw)))
		_, _ = f.Write(hdr[:])
		_, _ = f.Write(raw)
	}
	_ = f.Close()
	got, ok, err := lastSeqFromCapture(path, 0x7a59, 0x0003)
	if err != nil || !ok || got != 0x1236 {
		t.Fatalf("got=%04x found=%t err=%v", got, ok, err)
	}
	_, ok, err = lastSeqFromCapture(path, 0x7df9, 0x0005)
	if err != nil || ok {
		t.Fatalf("unexpected found=%t err=%v", ok, err)
	}
}
