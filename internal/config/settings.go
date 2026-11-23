package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Runtime settings are optional. Missing values use compiled defaults, while
// explicit CLI flags override JSON. Keep the device keys in the existing JSON.
type ControllerSettings struct {
	State       string `json:"state"`
	Adapter     string `json:"adapter"`
	Src         string `json:"src"`
	PhoneSrc    string `json:"phone_src"`
	Dst         string `json:"dst"`
	Transport   string `json:"transport"`
	AdvInterval string `json:"adv_interval"`
	MultiDP     bool   `json:"multi_dp"`
	Frames      int    `json:"frames"`
	Duration    string `json:"duration"`
	Gap         string `json:"gap"`
	Flags       string `json:"flags"`
	KelvinMin   int    `json:"kelvin_min"`
	KelvinMax   int    `json:"kelvin_max"`
	InvertTemp  bool   `json:"invert_temp"`
	Scan        bool   `json:"scan"`
	ClearAll    bool   `json:"clear_all"`
	DryRun      bool   `json:"dry_run"`
}

type learnSettings struct {
	Save    bool   `json:"save"`
	Out     string `json:"out"`
	Auto    bool   `json:"auto"`
	Timeout string `json:"timeout"`
}

type qrSettings struct {
	UserCode string `json:"user_code"`
	App      string `json:"app"`
	Timeout  string `json:"timeout"`
	Save     bool   `json:"save"`
}

type cloudSettings struct {
	Region   string `json:"region"`
	DeviceID string `json:"device_id"`
	Save     bool   `json:"save"`
}

type mqttSettings struct {
	Enabled            bool   `json:"enabled"`
	Broker             string `json:"broker"`
	Username           string `json:"username"`
	Password           string `json:"password"`
	PasswordEnv        string `json:"password_env"`
	ClientID           string `json:"client_id"`
	TopicPrefix        string `json:"topic_prefix"`
	HADiscoveryEnabled bool   `json:"ha_discovery_enabled"`
	HADiscoveryPrefix  string `json:"ha_discovery_prefix"`
	LightName          string `json:"light_name"`
	Debounce           string `json:"debounce"`
	MaxWait            string `json:"max_wait"`
	DedupWindow        string `json:"dedup_window"`
	TxDuration         string `json:"tx_duration"`
}

func Defaults() Config {
	return Config{
		Controller: ControllerSettings{
			Adapter: "hci0", Src: "0x7a59", PhoneSrc: "0x7a59", Dst: "0x0003",
			Transport: "mgmt", AdvInterval: "20ms", MultiDP: true, Frames: 1, Duration: "500ms", Gap: "120ms",
			Flags: "01", KelvinMin: 2700, KelvinMax: 6500, Scan: true,
		},
		Learn: learnSettings{Save: true, Auto: true, Timeout: "120s"},
		QR:    qrSettings{App: "smartlife", Timeout: "4m", Save: true},
		Cloud: cloudSettings{Region: "eu"},
		MQTT: mqttSettings{
			Enabled: true,
			Broker:  "tcp://localhost:1883", TopicPrefix: "tuya-beacon-mesh-p2-controller",
			HADiscoveryEnabled: true, HADiscoveryPrefix: "homeassistant", LightName: "Light",
			Debounce: "100ms", MaxWait: "450ms", DedupWindow: "350ms", TxDuration: "250ms",
		},
	}
}

// Read defaults without insisting on keys: login and learn can obtain them.
func Read(path string) (Config, error) { return read(path, true) }

func Load(path string) (Config, error) { return read(path, false) }

func read(path string, optional bool) (Config, error) {
	c := Defaults()
	b, err := os.ReadFile(path)
	if optional && errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("settings %s: %w", path, err)
	}
	// Fail on malformed explicit duration before using any radio operations.
	for name, value := range map[string]string{
		"controller.duration": c.Controller.Duration, "controller.gap": c.Controller.Gap, "controller.adv_interval": c.Controller.AdvInterval,
		"learn.timeout": c.Learn.Timeout, "qr.timeout": c.QR.Timeout,
		"mqtt.debounce": c.MQTT.Debounce, "mqtt.max_wait": c.MQTT.MaxWait, "mqtt.dedup_window": c.MQTT.DedupWindow, "mqtt.tx_duration": c.MQTT.TxDuration,
	} {
		if _, err := time.ParseDuration(value); err != nil {
			return c, fmt.Errorf("%s: %w", name, err)
		}
	}
	return c, nil
}

// The --config option must be discoverable before the rest of the FlagSet is
// constructed. Standard Go flags are only accepted before the command.
func PathFromArgs(args []string) string {
	fallback := DefaultPath()
	for i, arg := range args {
		if arg == "--config" {
			if i+1 < len(args) {
				return args[i+1]
			}
		}
		if strings.HasPrefix(arg, "--config=") {
			return strings.TrimPrefix(arg, "--config=")
		}
	}
	return fallback
}
