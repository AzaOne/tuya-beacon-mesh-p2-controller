package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalKeyConfigUpdateBackupAndUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conf.json")
	old := []byte(`{"other":{"preserve":42},"device":{"device_id":"lamp","local_key":"abcdefghijklmnop","beacon_key":"00112233445566778899aabbccddeeff","ext":123}}`)
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := UpdateLocalKeyConfig(path, "lamp", "ABCDEFGHIJKLMNOP")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("config must be private")
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	device := cfg["device"].(map[string]any)
	if device["local_key"] != "ABCDEFGHIJKLMNOP" || device["beacon_key"] != "" || device["ext"] != float64(123) {
		t.Fatalf("config mutated unexpectedly: %v", device)
	}
	if cfg["other"].(map[string]any)["preserve"] != float64(42) {
		t.Fatal("lost unknown top-level field")
	}
	files, err := filepath.Glob(path + ".bak-*")
	if err != nil || len(files) != 1 {
		t.Fatalf("backup count %d %v", len(files), err)
	}
	savedOld, err := os.ReadFile(files[0])
	if err != nil || string(savedOld) != string(old) {
		t.Fatal("backup mismatch")
	}
	changed, err = UpdateLocalKeyConfig(path, "lamp", "ABCDEFGHIJKLMNOP")
	if err != nil || changed {
		t.Fatal("no-op must not rewrite")
	}
	if _, err = UpdateLocalKeyConfig(path, "another", "1234567890abcdef"); err == nil {
		t.Fatal("must reject ID mismatch")
	}
}
