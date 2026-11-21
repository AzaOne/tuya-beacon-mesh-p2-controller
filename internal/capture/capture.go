package capture

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
)

// lastSeqFromCapture reads a standard BTSnoop H4 trace (.cfa, .log, .btsnoop)
// and finds the highest observed P2 TX seq for this specific mesh src/dst.
// Only the HCI host->controller LE Set Extended Advertising Data opcode 0x2037
// is trusted; received lamp advertisements are not counted.
func lastSeqFromCapture(path string, src, dst uint16) (uint16, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false, err
	}
	defer f.Close()
	header := make([]byte, 16)
	if _, err := io.ReadFull(f, header); err != nil {
		return 0, false, err
	}
	if !bytes.Equal(header[:8], []byte("btsnoop\x00")) {
		return 0, false, errors.New("not a BTSnoop H4 file")
	}
	var last uint16
	found := false
	for {
		var hdr [24]byte
		_, err := io.ReadFull(f, hdr[:])
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, false, fmt.Errorf("truncated record header: %w", err)
		}
		n := binary.BigEndian.Uint32(hdr[4:8])
		if n > 1024*1024 {
			return 0, false, fmt.Errorf("unreasonable HCI record size %d", n)
		}
		raw := make([]byte, int(n))
		if _, err := io.ReadFull(f, raw); err != nil {
			return 0, false, fmt.Errorf("truncated HCI record: %w", err)
		}
		if len(raw) < 8 || raw[0] != 0x01 || raw[1] != 0x37 || raw[2] != 0x20 {
			continue
		}
		// H4 command: [0x01][opcode 2LE][plen 1][params]
		if int(raw[3]) != len(raw)-4 {
			continue
		}
		params := raw[4:]
		if len(params) < 4 {
			continue
		}
		// LE Set Extended Advertising Data:
		// [handle][operation][fragment preference][len][advertising data]
		dataLength := int(params[3])
		if len(params) < 4+dataLength {
			continue
		}
		data := params[4 : 4+dataLength]
		for off := 0; off < len(data); {
			l := int(data[off])
			if l == 0 || off+1+l > len(data) {
				break
			}
			if l == 27 && data[off+1] == 0x03 {
				p2 := data[off+2 : off+28]
				if (p2[0] == 0x0b || p2[0] == 0x13) &&
					binary.BigEndian.Uint16(p2[1:3]) == src &&
					binary.BigEndian.Uint16(p2[3:5]) == dst {
					seq := binary.BigEndian.Uint16(p2[5:7])
					if !found || seq > last {
						last = seq
					}
					found = true
				}
			}
			off += 1 + l
		}
	}
	return last, found, nil
}

// Unlike the legacy seq sync, verify the P2 crypto and both CRCs before
// advancing persistent state from an imported untrusted HCI log.
func LastVerifiedSeqFromCapture(path string, src, dst uint16, k1, k2 []byte) (uint16, bool, error) {
	frames, err := CaptureTXFrames(path)
	if err != nil {
		return 0, false, err
	}
	var last uint16
	found := false
	for _, f := range frames {
		obs, ok := p2.DecodeObservedCommand(f, k1, k2, src, dst, 0)
		if !ok {
			continue
		}
		if !found || obs.Seq > last {
			last = obs.Seq
			found = true
		}
	}
	return last, found, nil
}
