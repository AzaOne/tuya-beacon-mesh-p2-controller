package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCounterPersistsAndNeverRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seq.json")
	st, err := Open(path, "device1", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if !st.FirstRun || st.State.LastReserved != uint32(capturedLastSeq) {
		t.Fatal("wrong bootstrap")
	}
	seq, err := st.Reserve(4)
	if err != nil {
		t.Fatal(err)
	}
	if seq[0] != 0x008f || seq[3] != 0x0092 {
		t.Fatalf("seq = %v", seq)
	}
	st.Close()
	st, err = Open(path, "device1", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if st.FirstRun || st.State.LastReserved != 0x0092 {
		t.Fatal("state not persisted")
	}
	if _, err := st.SyncTo(0x0090); err == nil {
		t.Fatal("rollback accepted")
	}
	changed, err := st.SyncTo(0x0100)
	if err != nil || !changed {
		t.Fatalf("sync: %v", err)
	}
	seq, err = st.Reserve(4)
	if err != nil || seq[0] != 0x0101 || seq[3] != 0x0104 {
		t.Fatalf("next seq %+v %v", seq, err)
	}
	st.Close()
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("state permissions: %v", err)
	}
	if _, err := Open(path, "device1", "other-hash"); err == nil {
		t.Fatal("different device fingerprint accepted")
	}
}

func TestSeqWrapRejected(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.json"), "d", "hash")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.SyncTo(0xfffe); err != nil {
		t.Fatal(err)
	}
	seq, err := st.Reserve(1)
	if err != nil || len(seq) != 1 || seq[0] != 0xffff {
		t.Fatalf("last sequence: %v %v", seq, err)
	}
	if _, err := st.Reserve(1); err == nil {
		t.Fatal("wrapped")
	}
}

func TestDefaultStatePath(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_STATE_HOME", base)
	for id, name := range map[string]string{"device9": "state-device9.json", "../../": "state-lamp.json"} {
		want := filepath.Join(base, "tuya-beacon-mesh-p2-controller", name)
		if got := DefaultStatePath(id); got != want {
			t.Errorf("DefaultStatePath(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestLegacyStateMigration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "old"))
	dev, fp := "device9", "test-fp"
	old, err := legacyStatePath(dev)
	if err != nil {
		t.Fatal(err)
	}
	st, err := Open(old, dev, fp)
	if err != nil {
		t.Fatal(err)
	}
	on := true
	st.State.Light = &LightState{Power: &on}
	st.State.SceneTemplates = map[string]string{"3": "sample"}
	if _, err := st.SyncTo(0x0190); err != nil {
		t.Fatal(err)
	}
	st.Close()
	newPath := filepath.Join(home, "app", "state-device9.json")
	ok, err := MigrateLegacyState(newPath, dev, fp)
	if err != nil || !ok {
		t.Fatalf("migrate: %t, %v", ok, err)
	}
	fresh, err := Open(newPath, dev, fp)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.State.Light == nil || fresh.State.Light.Power == nil || !*fresh.State.Light.Power || fresh.State.SceneTemplates["3"] != "sample" {
		t.Fatal("incomplete migrated snapshot")
	}
	if fresh.State.LastReserved != 0x0190 {
		t.Fatalf("last seq = %04x", fresh.State.LastReserved)
	}
	fresh.Close()
	ok, err = MigrateLegacyState(newPath, dev, fp)
	if err != nil || ok {
		t.Fatalf("second migration should be noop: %t, %v", ok, err)
	}
}
