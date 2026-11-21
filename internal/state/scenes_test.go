package state

import (
	"bytes"
	"path/filepath"
	"testing"

	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
	"example.com/tuya-beacon-mesh-p2-controller/internal/testutil"
)

func TestSceneLearnedFromPhoneOverridesInference(t *testing.T) {
	keys1, keys2 := testutil.SampleKeys()
	path := filepath.Join(t.TempDir(), "state.json")
	fp := Fingerprint("test-device", 0x7a59, 3, keys1, keys2)
	scene13 := []byte{0x0d, 0x00, 0x02, 0x01, 0x4c, 0x64, 0x01, 0x02}
	cmd := p2.MustDP("scene13 from phone", 73, 0, scene13)
	frame, err := p2.BuildFrame(0x7a59, 3, 0x0412, cmd, keys1, keys2, 1)
	if err != nil {
		t.Fatal(err)
	}
	obs, ok := p2.DecodeObservedCommand(frame.Frame[:], keys1, keys2, 0x7a59, 3, -60)
	if !ok {
		t.Fatal("failed decode synthetic phone scene")
	}
	changed, last, err := SyncObservedFull(path, "test-device", fp, obs, "phone")
	if err != nil || !changed || last != 0x0412 {
		t.Fatalf("sync failed %v %d %v", changed, last, err)
	}
	st, err := Open(path, "test-device", fp)
	if err != nil {
		t.Fatal(err)
	}
	if st.State.Light == nil || st.State.Light.Scene == nil || st.State.Light.Scene.ID != 13 {
		t.Fatal("scene not reflected in state")
	}
	if got := st.State.SceneTemplates["13"]; got != "0d0002014c640102" {
		t.Fatalf("learned raw %s", got)
	}
	cmdOverride, _, inferred, err := p2.SceneCommands([]string{"13"}, st.State.SceneTemplates)
	if err != nil || inferred {
		t.Fatalf("override not used err=%v inferred=%t", err, inferred)
	}
	dps, err := p2.ParseDPBlock(cmdOverride[0].Plain[:])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dps[0].Value, scene13) {
		t.Fatalf("actual phone payload not preserved: %x", dps[0].Value)
	}
	st.Close()
	// Do not replace fresh learned scene with an older (but authenticated) packet.
	older, _ := p2.BuildFrame(0x7a59, 3, 0x0411, p2.MustDP("stale", 73, 0, []byte{0x0d, 0, 2, 1, 10, 20, 1, 2}), keys1, keys2, 1)
	oldObs, _ := p2.DecodeObservedCommand(older.Frame[:], keys1, keys2, 0x7a59, 3, -60)
	_, _, err = SyncObservedFull(path, "test-device", fp, oldObs, "phone")
	if err == nil {
		t.Fatal("stale seq was accepted")
	}
	st, err = Open(path, "test-device", fp)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if got := st.State.SceneTemplates["13"]; got != "0d0002014c640102" {
		t.Fatalf("older command overwrote template: %s", got)
	}
}
