package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// updateLocalKeyConfig preserves unknown JSON fields, makes a private backup
// when the key changes, and clears the old beacon_key (which needs relearning).
// It never prints keys and never overwrites on-air state.
func UpdateLocalKeyConfig(path, deviceID, localKey string) (changed bool, err error) {
	if _, err := ParseKey(localKey); err != nil {
		return false, err
	}
	var raw map[string]json.RawMessage
	var original []byte
	info, statErr := os.Lstat(path)
	switch {
	case statErr == nil:
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return false, errors.New("config must be a regular non-symlink file")
		}
		original, err = os.ReadFile(path)
		if err != nil {
			return false, err
		}
		if err = json.Unmarshal(original, &raw); err != nil {
			return false, err
		}
	case errors.Is(statErr, os.ErrNotExist):
		raw = make(map[string]json.RawMessage)
	default:
		return false, statErr
	}
	var device map[string]json.RawMessage
	if b, ok := raw["device"]; ok {
		if err = json.Unmarshal(b, &device); err != nil {
			return false, err
		}
	}
	if device == nil {
		device = make(map[string]json.RawMessage)
	}
	var oldID, oldLocal string
	_ = json.Unmarshal(device["device_id"], &oldID)
	_ = json.Unmarshal(device["local_key"], &oldLocal)
	if oldID != "" && oldID != deviceID {
		return false, fmt.Errorf("config belongs to different device %s", oldID)
	}
	if oldLocal == localKey {
		return false, nil
	}
	setStr := func(field, v string) { b, _ := json.Marshal(v); device[field] = b }
	setStr("device_id", deviceID)
	setStr("local_key", localKey)
	if oldLocal != "" && oldLocal != localKey {
		setStr("beacon_key", "")
	}
	db, err := json.Marshal(device)
	if err != nil {
		return false, err
	}
	raw["device"] = db
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return false, err
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return false, err
	}
	// Backup first; never overwrite a pre-existing backup.
	if len(original) > 0 {
		backup := path + ".bak-" + time.Now().UTC().Format("20060102T150405.000000000")
		f, e := os.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return false, e
		}
		if _, e = f.Write(original); e == nil {
			e = f.Sync()
		}
		if c := f.Close(); e == nil {
			e = c
		}
		if e != nil {
			return false, e
		}
	}
	tmp, err := os.CreateTemp(dir, ".tuya-beacon-mesh-p2-controller-config-*.tmp")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())
	if err = tmp.Chmod(0600); err != nil {
		tmp.Close()
		return false, err
	}
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return false, err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return false, err
	}
	if err = tmp.Close(); err != nil {
		return false, err
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return false, err
	}
	return true, nil
}
