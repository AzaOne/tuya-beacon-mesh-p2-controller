package capture

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"

	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
)

// captureTXFrames extracts Android -> controller LE Set Extended Advertising
// Data records (0x2037), then finds 26-byte P2 service-list frames. This is
// NOT a generic pcap parser; it expects Android's standard BTSnoop H4 format.
func CaptureTXFrames(path string) ([][]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	h := make([]byte, 16)
	if _, err = io.ReadFull(file, h); err != nil {
		return nil, err
	}
	if !bytes.Equal(h[:8], []byte("btsnoop\x00")) {
		return nil, errors.New("expected btsnoop H4 file")
	}
	var frames [][]byte
	for {
		var hdr [24]byte
		_, err := io.ReadFull(file, hdr[:])
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		size := binary.BigEndian.Uint32(hdr[4:8])
		if size > 1<<20 {
			return nil, errors.New("oversized btsnoop record")
		}
		data := make([]byte, int(size))
		if _, err = io.ReadFull(file, data); err != nil {
			return nil, err
		}
		if len(data) < 8 || data[0] != 1 || data[1] != 0x37 || data[2] != 0x20 || int(data[3]) != len(data)-4 {
			continue
		}
		params := data[4:]
		if len(params) < 4 {
			continue
		}
		n := int(params[3])
		if len(params) < 4+n {
			continue
		}
		for _, f := range p2.P2FramesFromAdvertisement(params[4 : 4+n]) {
			frames = append(frames, f)
		}
	}
	return frames, nil
}
