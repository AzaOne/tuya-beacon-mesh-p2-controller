package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// updateLocalKeyConfig preserves unknown JSON fields, makes a private backup
// when the key changes, and clears the old beacon_key (which needs relearning).
// It never prints keys and never overwrites on-air state.
func UpdateLocalKeyConfig(path, deviceID, localKey string) (changed bool, err error) {
	if _, err := ParseKey(localKey); err != nil {
		return false, err
	}
	return updateDevice(path, func(device map[string]json.RawMessage) (bool, error) {
		var oldID, oldLocal string
		_ = json.Unmarshal(device["device_id"], &oldID)
		_ = json.Unmarshal(device["local_key"], &oldLocal)
		if oldID != "" && oldID != deviceID {
			return false, fmt.Errorf("config belongs to different device %s", oldID)
		}
		if oldLocal == localKey {
			return false, nil
		}

		setDeviceString(device, "device_id", deviceID)
		setDeviceString(device, "local_key", localKey)
		if oldLocal != "" && oldLocal != localKey {
			setDeviceString(device, "beacon_key", "")
		}
		return true, nil
	})
}

func setDeviceString(device map[string]json.RawMessage, field, value string) {
	b, _ := json.Marshal(value)
	device[field] = b
}

func UpdateBeaconKeyConfig(path, deviceID, localKey, beaconKey string) (bool, error) {
	if _, err := ParseKey(beaconKey); err != nil {
		return false, err
	}
	return updateDevice(path, func(device map[string]json.RawMessage) (bool, error) {
		var id, local, beacon string
		_ = json.Unmarshal(device["device_id"], &id)
		_ = json.Unmarshal(device["local_key"], &local)
		_ = json.Unmarshal(device["beacon_key"], &beacon)
		if id != deviceID || local == "" || local != localKey {
			return false, errors.New("device or local_key changed during learning")
		}
		if beacon == beaconKey {
			return false, nil
		}
		setDeviceString(device, "beacon_key", beaconKey)
		return true, nil
	})
}

func updateDevice(path string, update func(map[string]json.RawMessage) (bool, error)) (changed bool, err error) {
	dir := filepath.Dir(path)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return false, err
	}
	lock, err := os.Open(dir)
	if err != nil {
		return false, err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return false, err
	}
	var raw map[string]json.RawMessage
	var original []byte
	var info os.FileInfo
	file, openErr := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	switch {
	case openErr == nil:
		defer file.Close()
		info, err = file.Stat()
		if err != nil {
			return false, err
		}
		if !info.Mode().IsRegular() {
			return false, errors.New("config must be a regular file")
		}
		original, err = io.ReadAll(file)
		if err != nil {
			return false, err
		}
		if err = json.Unmarshal(original, &raw); err != nil {
			return false, err
		}
	case errors.Is(openErr, os.ErrNotExist):
		raw = make(map[string]json.RawMessage)
	default:
		return false, openErr
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
	if raw == nil {
		return false, errors.New("config must contain a JSON object")
	}
	if changed, err = update(device); !changed || err != nil {
		return false, err
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
	// sudo learn live must leave the private config readable by its original owner.
	preserveOwner := func(f *os.File) error {
		if info != nil && os.Geteuid() == 0 {
			owner := info.Sys().(*syscall.Stat_t)
			return f.Chown(int(owner.Uid), int(owner.Gid))
		}
		return nil
	}
	// Backup first; never overwrite a pre-existing backup.
	if len(original) > 0 {
		backup := path + ".bak-" + time.Now().UTC().Format("20060102T150405.000000000")
		f, e := os.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return false, e
		}
		if e = preserveOwner(f); e == nil {
			_, e = f.Write(original)
		}
		if e == nil {
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
	if err = preserveOwner(tmp); err != nil {
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
