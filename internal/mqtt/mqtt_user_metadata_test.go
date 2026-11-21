package mqtt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"example.com/tuya-beacon-mesh-p2-controller/internal/config"
)

func TestTuyaDeviceMetadataAndBridge(t *testing.T) {
	opt := Options{
		DeviceID: "test-lamp", DeviceName: "Smart Bulb 2",
		ClientID: "tuya-beacon-mesh-p2-controller", Prefix: "tuya-beacon-mesh-p2-controller/test-lamp",
		DiscoveryPrefix: "homeassistant", KelvinMin: 2700, KelvinMax: 6500,
	}
	bridgeID := "tuya_p2_controller_tuya-beacon-mesh-p2-controller"
	if got := controllerID(opt); got != bridgeID {
		t.Fatalf("bridge ID: got %q want %q", got, bridgeID)
	}
	if got := controllerDiscoveryTopic(opt); got != "homeassistant/binary_sensor/"+bridgeID+"_connection/config" {
		t.Fatalf("bridge discovery topic: %q", got)
	}
	parent, err := controllerDiscoveryJSON(opt)
	if err != nil {
		t.Fatal(err)
	}
	var bridge map[string]any
	if err := json.Unmarshal(parent, &bridge); err != nil {
		t.Fatal(err)
	}
	d := bridge["device"].(map[string]any)
	for key, want := range map[string]any{
		"name": "tuya-beacon-mesh-p2-controller Bridge", "manufacturer": "AzaOne",
		"model": "Bridge", "sw_version": softwareVersion,
	} {
		if d[key] != want {
			t.Errorf("parent %s=%v, want %v", key, d[key], want)
		}
	}
	if !reflect.DeepEqual(d["identifiers"], []any{bridgeID}) {
		t.Errorf("parent identifiers=%v", d["identifiers"])
	}
	if bridge["state_topic"] != "tuya-beacon-mesh-p2-controller/bridge/state" || bridge["value_template"] != "{{ value_json.state }}" || bridge["payload_on"] != "online" || bridge["payload_off"] != "offline" {
		t.Errorf("bridge connectivity configuration: %v", bridge)
	}
	if bridge["entity_category"] != "diagnostic" || bridge["device_class"] != "connectivity" {
		t.Errorf("bridge status sensor not diagnostic: %v", bridge)
	}

	for _, fn := range []func(Options) ([]byte, error){discoveryJSON, speedDiscoveryJSON} {
		blob, err := fn(opt)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]any
		if err := json.Unmarshal(blob, &result); err != nil {
			t.Fatal(err)
		}
		dev := result["device"].(map[string]any)
		for key, want := range map[string]any{
			"name":         "Smart bulb (test-lamp)",
			"manufacturer": "Tuya",
			"sw_version":   "1.2", "hw_version": "wbl39qpt",
			"via_device": bridgeID,
		} {
			if dev[key] != want {
				t.Errorf("light/number %s=%v want %v", key, dev[key], want)
			}
		}
		if result["availability_topic"] != "tuya-beacon-mesh-p2-controller/bridge/state" || result["availability_template"] != "{{ value_json.state }}" {
			t.Errorf("child availability not linked to bridge: %v", result)
		}
		if got := dev["model"]; got != "Smart Bulb 2 (Tuya Beacon Mesh P2) (wbl39qpt)" {
			t.Errorf("unexpected model text: %v", got)
		}
		if !reflect.DeepEqual(dev["identifiers"], []any{"tuya_p2_test-lamp"}) {
			t.Errorf("light/number identifiers %v", dev["identifiers"])
		}
		origin := result["origin"].(map[string]any)
		if origin["name"] != "tuya-beacon-mesh-p2-controller" || origin["sw"] != softwareVersion {
			t.Errorf("origin %v", origin)
		}
	}
}

func TestTuyaFirmwareFromConfigAndUnknownDevice(t *testing.T) {
	opt := Options{DeviceID: "other-lamp", DeviceModel: "Another Lamp", Firmware: "2.7", ProductID: "other-board", DiscoveryPrefix: "homeassistant", KelvinMin: 2700, KelvinMax: 6500}
	d := deviceMetadata(opt)
	if d["sw_version"] != "2.7" || d["hw_version"] != "other-board" || d["model"] != "Another Lamp (other-board)" {
		t.Fatal(d)
	}
	opt.Firmware, opt.ProductID, opt.DeviceModel = "", "", ""
	d = deviceMetadata(opt)
	if _, ok := d["sw_version"]; ok {
		t.Fatalf("invented firmware for unknown device: %v", d)
	}
	if _, ok := d["hw_version"]; ok {
		t.Fatalf("invented hardware for unknown device: %v", d)
	}
	if _, ok := d["model"]; ok {
		t.Fatalf("invented model for unknown device: %v", d)
	}

	// Backward compatible reading of legacy device.hardware, with precedence for product_id.
	opt.Hardware = "legacy-board"
	if got := deviceMetadata(opt)["hw_version"]; got != "legacy-board" {
		t.Fatalf("legacy hardware fallback: %v", got)
	}
	opt.ProductID = "new-product-id"
	if got := deviceMetadata(opt)["hw_version"]; got != "new-product-id" {
		t.Fatalf("product_id must override legacy hardware: %v", got)
	}

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"device":{"name":"Lamp","model":"Example Model","device_id":"other-lamp","firmware":"9.0","product_id":"my-product"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Device.Firmware != "9.0" || cfg.Device.ProductID != "my-product" || cfg.Device.Model != "Example Model" {
		t.Fatalf("device firmware/product_id not loaded from config: %+v", cfg.Device)
	}
}
