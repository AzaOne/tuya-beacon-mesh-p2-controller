package mqtt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"example.com/tuya-beacon-mesh-p2-controller/internal/config"
)

func TestMQTTLightNameIndependentOfDevice(t *testing.T) {
	opt := bridgeTestOptions()
	opt.LightName = "Light"
	for _, want := range []string{"Light", "Main light"} {
		if want == "Main light" {
			opt.LightName = "  Main light  "
		}
		b, err := discoveryJSON(opt)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err = json.Unmarshal(b, &doc); err != nil {
			t.Fatal(err)
		}
		if doc["name"] != want {
			t.Fatalf("name: got %v want %s", doc["name"], want)
		}
		device := doc["device"].(map[string]any)
		if device["name"] != "Smart bulb (test-lamp)" {
			t.Fatal("modified device name", device["name"])
		}
		if doc["unique_id"] != "tuya_p2_test-lamp" {
			t.Fatal("modified unique_id")
		}
	}
	opt.LightName = ""
	if lightName(opt) != "Light" {
		t.Fatal("incorrect fallback")
	}
}

func TestMQTTLightNameFromConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"mqtt":{"light_name":"Main light"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MQTT.LightName != "Main light" {
		t.Fatal(cfg.MQTT.LightName)
	}
	if cfg.MQTT.Debounce != "100ms" {
		t.Fatal("other MQTT defaults changed")
	}
}
