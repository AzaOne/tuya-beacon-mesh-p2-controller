package p2

import (
	"bytes"
	"testing"

	"example.com/tuya-beacon-mesh-p2-controller/internal/testutil"
)

func TestPackMultiDP(t *testing.T) {
	samples := []struct {
		command string
		args    []string
		lengths []int
	}{
		{"rgb", []string{"100", "232", "111"}, []int{9}},
		{"temp", []string{"4000"}, []int{9}},
		{"white", []string{"5000", "80"}, []int{15}},
		{"on", nil, []int{3}},
	}
	for _, tt := range samples {
		cmds, err := CommandForArgs(tt.command, tt.args, 2700, 6500, false)
		if err != nil {
			t.Fatal(err)
		}
		packed, err := PackCommands(cmds)
		if err != nil {
			t.Fatal(err)
		}
		if len(packed) != len(tt.lengths) {
			t.Errorf("%s expected %d blocks got %d", tt.command, len(tt.lengths), len(packed))
			continue
		}
		for i, block := range packed {
			dps, err := ParseDPBlock(block.Plain[:])
			if err != nil {
				t.Fatal(err)
			}
			total := 0
			for _, dp := range dps {
				total += 2 + len(dp.Value)
			}
			if total != tt.lengths[i] {
				t.Errorf("%s: bytes=%d want %d", tt.command, total, tt.lengths[i])
			}
		}
	}
}

func TestMultiDPEncryptedRoundTrip(t *testing.T) {
	k1, k2 := testutil.SampleKeys()
	commands, err := CommandForArgs("white", []string{"4000", "80"}, 2700, 6500, false)
	if err != nil {
		t.Fatal(err)
	}
	packed, err := PackCommands(commands)
	if err != nil {
		t.Fatal(err)
	}
	f, err := BuildFrame(0x7a59, 3, 0x0983, packed[0], k1, k2, 1)
	if err != nil {
		t.Fatal(err)
	}
	decoded, ok := DecodeObservedCommand(f.Frame[:], k1, k2, 0x7a59, 3, -80)
	if !ok || len(decoded.DPs) != 3 {
		t.Fatalf("cannot decode packed DPs %+v", decoded)
	}
	for i, dp := range decoded.DPs {
		if dp.ID != []byte{2, 4, 3}[i] {
			t.Fatalf("wrong DP order %v", decoded.DPs)
		}
	}
	// Ciphertext cannot depend on seq (outer header only).
	second, _ := BuildFrame(0x7a59, 3, 0x0984, packed[0], k1, k2, 1)
	if !bytes.Equal(f.Frame[9:25], second.Frame[9:25]) {
		t.Fatal("ciphertext unexpectedly depends on seq")
	}
}

func TestPackSplitsAtDPBoundaries(t *testing.T) {
	cmds := []DPCommand{BoolDP(true), ValueDP("brightness", 3, 123), HsvDP(23, 45, 67), ValueDP("temp", 4, 444)}
	got, err := PackCommands(cmds)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(got))
	}
	first, _ := ParseDPBlock(got[0].Plain[:])
	second, _ := ParseDPBlock(got[1].Plain[:])
	if len(first) != 3 || len(second) != 1 {
		t.Fatalf("unexpected DP grouping: %+v, %+v", first, second)
	}
}
