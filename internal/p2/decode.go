package p2

import (
	"encoding/binary"
	"fmt"
	"strings"
)

type ObservedCommand struct {
	Source      uint16
	Target      uint16
	Seq         uint16
	ID          byte
	Type        byte
	Value       []byte
	Plain       [16]byte
	Description string
	DPs         []ParsedDP
	RSSI        int8
}

// Find 26-byte Tuya P2 frames embedded in the complete-list-of-16-bit-UUIDs
// AD structure (type 0x03). We deliberately do not infer commands from names,
// Bluetooth MACs, or unverified plaintext patterns.
func P2FramesFromAdvertisement(data []byte) [][]byte {
	var frames [][]byte
	for i := 0; i < len(data); {
		n := int(data[i])
		if n == 0 {
			break
		}
		if n < 1 || i+1+n > len(data) {
			break
		}
		if n == 27 && data[i+1] == 0x03 {
			frames = append(frames, append([]byte(nil), data[i+2:i+28]...))
		}
		i += n + 1
	}
	return frames
}

func decryptXXTEABE(ciphertext, key []byte) [16]byte {
	var v, k [4]uint32
	for i := 0; i < 4; i++ {
		v[i] = binary.BigEndian.Uint32(ciphertext[4*i : 4*i+4])
		k[i] = binary.BigEndian.Uint32(key[4*i : 4*i+4])
	}
	sum := uint32(uint64(19) * uint64(delta) & 0xffffffff)
	y := v[0]
	for round := 0; round < 19; round++ {
		e := (sum >> 2) & 3
		for p := 3; p > 0; p-- {
			z := v[p-1]
			v[p] -= mx(sum, y, z, k, p, e)
			y = v[p]
		}
		z := v[3]
		v[0] -= mx(sum, y, z, k, 0, e)
		y = v[0]
		sum -= delta
	}
	var result [16]byte
	for i := 0; i < 4; i++ {
		binary.BigEndian.PutUint32(result[i*4:i*4+4], v[i])
	}
	return result
}

func DecodeObservedCommand(frame, k1, k2 []byte, expectedSrc, expectedDst uint16, rssi int8) (ObservedCommand, bool) {
	var out ObservedCommand
	if len(frame) != 26 || len(k1) != 16 || len(k2) != 16 || frame[0] != 0x0b || frame[7] != 0x05 {
		return out, false
	}
	src, dst := binary.BigEndian.Uint16(frame[1:3]), binary.BigEndian.Uint16(frame[3:5])
	if src != expectedSrc || dst != expectedDst {
		return out, false
	}

	// The final transport CRC is computed over the still-unmasked XXTEA
	// output, not over transmitted XOR-masked bytes.
	clean := append([]byte(nil), frame...)
	for i := 0; i < 16; i++ {
		clean[9+i] ^= k2[i]
	}
	crcInput := make([]byte, 0, 25)
	crcInput = append(crcInput, clean[0]&0xfc)
	crcInput = append(crcInput, clean[1:25]...)
	if Crc16hi(crcInput) != clean[25] {
		return out, false
	}

	plain := decryptXXTEABE(clean[9:25], k1)
	if Crc16hi(plain[:]) != frame[8] {
		return out, false
	}
	dps, err := ParseDPBlock(plain[:])
	if err != nil {
		return out, false
	}
	out.Source, out.Target, out.Seq = src, dst, binary.BigEndian.Uint16(frame[5:7])
	out.ID, out.Type, out.Value = dps[0].ID, dps[0].Type, dps[0].Value
	out.DPs = dps
	out.RSSI = rssi
	out.Plain = plain
	parts := make([]string, 0, len(dps))
	for _, dp := range dps {
		parts = append(parts, DescribeDP(dp.ID, dp.Type, dp.Value))
	}
	out.Description = strings.Join(parts, " + ")
	return out, true
}

func DescribeDP(id, typ byte, value []byte) string {
	if id == 1 && typ == 1 && len(value) == 1 {
		if value[0] == 1 {
			return "ON"
		}
		if value[0] == 0 {
			return "OFF"
		}
	}
	if id == 2 && typ == 4 && len(value) == 1 {
		if value[0] < 3 {
			return []string{"mode white", "mode colour", "mode scene"}[value[0]]
		}
		return fmt.Sprintf("mode raw %d (unverified)", value[0])
	}
	if (id == 3 || id == 4) && typ == 2 && len(value) == 4 {
		v := binary.BigEndian.Uint32(value)
		if id == 3 {
			return fmt.Sprintf("brightness DP3=%d", v)
		}
		return fmt.Sprintf("temperature DP4=%d", v)
	}
	if id == 11 && typ == 0 && len(value) == 4 {
		return fmt.Sprintf("hsv %d %d %d", binary.BigEndian.Uint16(value[:2]), value[2], value[3])
	}
	if id == 73 && typ == 0 && len(value) >= 7 && len(value) <= 14 {
		if preset, ok := GetScene(value[0]); ok {
			return fmt.Sprintf("scene %d %q speed=%d brightness=%d", value[0], preset.Name, value[4], value[5])
		}
	}
	return fmt.Sprintf("DP%d type=%d value=%x", id, typ, value)
}
