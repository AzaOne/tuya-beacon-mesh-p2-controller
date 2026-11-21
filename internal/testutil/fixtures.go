package testutil

import (
	"encoding/binary"
)

func SampleKeys() ([]byte, []byte) {
	return []byte("abcdefghijklmnop"), []byte("pqrstuvwxyz12345")
}

func EncodeLegacyEvent(adv []byte, rssi int8) []byte {
	p := []byte{0x02, 0x01, 0x00, 0x01, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, byte(len(adv))}
	p = append(p, adv...)
	p = append(p, byte(rssi))
	return append([]byte{0x3e, byte(len(p))}, p...)
}

func EncodeExtendedEvent(adv []byte, rssi int8) []byte {
	p := make([]byte, 2+24)
	p[0], p[1] = 0x0d, 0x01
	// 24-byte LE Extended Advertising report; per-report RSSI at offset 13.
	p[2+13] = byte(rssi)
	p[2+23] = byte(len(adv))
	p = append(p, adv...)
	return append([]byte{0x3e, byte(len(p))}, p...)
}

func EncodeMonitorEvent(index uint16, evt []byte) []byte {
	p := make([]byte, 6, 6+len(evt))
	binary.LittleEndian.PutUint16(p[0:2], 3)
	binary.LittleEndian.PutUint16(p[2:4], index)
	binary.LittleEndian.PutUint16(p[4:6], uint16(len(evt)))
	return append(p, evt...)
}
