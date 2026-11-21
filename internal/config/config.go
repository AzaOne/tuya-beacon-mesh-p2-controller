package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	Controller ControllerSettings `json:"controller"`
	Learn      learnSettings      `json:"learn"`
	QR         qrSettings         `json:"qr"`
	Cloud      cloudSettings      `json:"cloud"`
	MQTT       mqttSettings       `json:"mqtt"`
	Device     struct {
		Name      string `json:"name"`
		Model     string `json:"model"`
		Firmware  string `json:"firmware"`
		ProductID string `json:"product_id"`
		Hardware  string `json:"hardware"` // legacy config alias; product_id takes precedence
		DeviceID  string `json:"device_id"`
		LocalKey  string `json:"local_key"`
		BeaconKey string `json:"beacon_key"`
	} `json:"device"`
}

func ParseKey(s string) ([]byte, error) {
	switch len(s) {
	case 16:
		for i := 0; i < len(s); i++ {
			if s[i] > 127 {
				return nil, errors.New("16-byte key must be ASCII")
			}
		}
		return []byte(s), nil
	case 32:
		b, err := hex.DecodeString(s)
		if err != nil {
			return nil, errors.New("32-character key must be hex")
		}
		return b, nil
	default:
		return nil, errors.New("expected 16 ASCII characters or 32 hex characters")
	}
}

func LoadDevice(path string) (Config, []byte, []byte, error) {
	c, err := read(path, false)
	if err != nil {
		return c, nil, nil, err
	}
	k1, k2, err := c.DeviceKeys()
	return c, k1, k2, err
}

func (c Config) DeviceKeys() ([]byte, []byte, error) {
	k1, err := ParseKey(c.Device.LocalKey)
	if err != nil {
		return nil, nil, fmt.Errorf("device.local_key: %w", err)
	}
	k2, err := ParseKey(c.Device.BeaconKey)
	if err != nil {
		return nil, nil, fmt.Errorf("device.beacon_key: %w", err)
	}
	return k1, k2, nil
}

func DefaultPath() string {
	if p := os.Getenv("TUYA_BEACON_MESH_P2_CONTROLLER_CONFIG"); p != "" {
		return p
	}
	if p := os.Getenv("TUYA_P2_CONFIG"); p != "" {
		return p
	}
	const filename = "config.json"
	paths := []string{filename}
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		dir := filepath.Dir(exe)
		paths = append(paths, filepath.Join(dir, filename), filepath.Join(dir, "tuya-lamp-re-verified.json"), filepath.Join(dir, "tuya-lamp-re.json"))
	}
	paths = append(paths, "tuya-lamp-re-verified.json", "tuya-lamp-re.json", "../tuya-lamp-re-verified.json", "../tuya-lamp-re.json")
	if dir, err := os.UserConfigDir(); err == nil {
		paths = append(paths, filepath.Join(dir, "tuya-beacon-mesh-p2-controller", "config.json"), filepath.Join(dir, "tuya-p2", "config.json"))
	}
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return filename
}
