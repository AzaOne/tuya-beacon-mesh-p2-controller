package p2

import (
	"bytes"
	"encoding/hex"
	"testing"

	"example.com/tuya-beacon-mesh-p2-controller/internal/testutil"
)

func TestSceneCatalogCompleteAndEncodable(t *testing.T) {
	if len(ScenePresets) != 28 {
		t.Fatalf("got %d scenes", len(ScenePresets))
	}
	keys1, keys2 := testutil.SampleKeys()
	for i, p := range ScenePresets {
		if int(p.ID) != i {
			t.Fatalf("catalog out of order at %d: id=%d", i, p.ID)
		}
		raw, err := hex.DecodeString(p.Raw)
		if err != nil || !validSceneRaw(p.ID, raw) {
			t.Fatalf("scene %d invalid raw %s: %v", i, p.Raw, err)
		}
		if !p.Captured {
			t.Errorf("scene %d unexpectedly inferred", i)
		}
		commands, req, inferred, err := SceneCommands([]string{p.Name}, nil)
		if err != nil {
			t.Fatalf("scene %d name %q: %v", i, p.Name, err)
		}
		if req.Preset.ID != p.ID || inferred != !p.Captured {
			t.Fatalf("scene %d incorrect metadata", i)
		}
		if len(commands) != 1 {
			t.Fatalf("scene %d expected only DP73 (same as Smart Life)", i)
		}
		dps, err := ParseDPBlock(commands[0].Plain[:])
		if err != nil || len(dps) != 1 || dps[0].ID != 73 || dps[0].Type != 0 || !bytes.Equal(dps[0].Value, raw) {
			t.Fatalf("scene %d TLV mismatch %+v %v", i, dps, err)
		}
		packed, err := PackCommands(commands)
		if err != nil {
			t.Fatal(err)
		}
		wantBlocks := 1 // Scene raw fits in one 16-byte DP-TLV P2 block.
		if len(packed) != wantBlocks {
			t.Fatalf("scene %d packed blocks=%d want %d", i, len(packed), wantBlocks)
		}
		for n, c := range packed {
			fr, err := BuildFrame(0x7a59, 3, uint16(0x200+i*2+n), c, keys1, keys2, 1)
			if err != nil {
				t.Fatal(err)
			}
			observed, ok := DecodeObservedCommand(fr.Frame[:], keys1, keys2, 0x7a59, 3, -60)
			if !ok {
				t.Fatalf("scene %d frame %d not decodable", i, n)
			}
			if len(observed.DPs) == 0 {
				t.Fatalf("scene %d frame %d empty", i, n)
			}
		}
	}
}

func TestSceneIDsNamesAndModifiers(t *testing.T) {
	selectors := []string{"13", "0x0D", "Red & blue gradient", "red-blue-gradient", "red blue gradient"}
	for _, selector := range selectors {
		scene, err := FindScene(selector)
		if err != nil || scene.ID != 13 {
			t.Errorf("selector %q -> %+v %v", selector, scene, err)
		}
	}
	for _, selector := range []string{"08", "Warm light breath", "warm_light_breath"} {
		scene, err := FindScene(selector)
		if err != nil || scene.ID != 8 {
			t.Errorf("selector %q -> %+v %v", selector, scene, err)
		}
	}
	cmds, _, _, err := SceneCommands([]string{"8", "--speed", "25", "--brightness=50"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	dp, err := ParseDPBlock(cmds[0].Plain[:])
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(dp[0].Value); got != "0800020019324080" {
		t.Fatalf("scene modification got %s", got)
	}
	for _, args := range [][]string{
		{"29"}, {"-1"}, {"unknown"}, {"8", "--speed", "0"},
		{"8", "--brightness", "101"}, {"8", "--speed"},
		{"8", "--speed=20", "--speed=30"}, {"8", "--unknown", "3"},
	} {
		if _, _, _, err := SceneCommands(args, nil); err == nil {
			t.Errorf("bad args accepted: %q", args)
		}
	}
}
