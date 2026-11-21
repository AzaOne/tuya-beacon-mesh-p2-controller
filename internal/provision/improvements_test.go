package provision

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
	"example.com/tuya-beacon-mesh-p2-controller/internal/testutil"
)

func TestLearnSyntheticKnownPlaintext(t *testing.T) {
	k1, k2 := testutil.SampleKeys()
	var frames [][]byte
	for i, c := range []p2.DPCommand{p2.BoolDP(true), p2.BoolDP(false), p2.ValueDP("bright", 3, 500)} {
		f, err := p2.BuildFrame(0x7a59, 3, uint16(0x101+i), c, k1, k2, 1)
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, append([]byte(nil), f.Frame[:]...))
	}
	got, err := recoverKey(frames, k1, 0x7a59, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got.BeaconKey != hex.EncodeToString(k2) || got.VerifiedFrames != 3 {
		t.Fatalf("bad recovered candidate %+v", got)
	}
	file := filepath.Join(t.TempDir(), "recovered.json")
	if err := saveLearnResult(file, got); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(file)
	if info.Mode().Perm() != 0600 {
		t.Fatal("secret permissions wrong")
	}
	if err := saveLearnResult(file, got); err == nil {
		t.Fatal("overwrote existing recovered key")
	}
}
