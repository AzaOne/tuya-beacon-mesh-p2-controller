package mqtt

import (
	"strings"
	"testing"
)

func TestZ2MModelField(t *testing.T) {
	opt := Options{DeviceID: "test-lamp", DeviceModel: "Smart Bulb 2 (Tuya Beacon Mesh P2)", ProductID: "wbl39qpt", Firmware: "1.2"}
	d := deviceMetadata(opt)
	if d["model"] != "Smart Bulb 2 (Tuya Beacon Mesh P2) (wbl39qpt)" || d["hw_version"] != "wbl39qpt" || d["manufacturer"] != "Tuya" {
		t.Fatalf("bad metadata: %v", d)
	}
	opt.DeviceModel = "Smart Bulb 2 (wbl39qpt)"
	if got := deviceMetadata(opt)["model"]; got != "Smart Bulb 2 (wbl39qpt)" {
		t.Fatalf("duplicated product ID: %v", got)
	}
	opt.DeviceID = "other-device"
	opt.DeviceModel = "Custom"
	opt.ProductID = ""
	if got := deviceMetadata(opt)["model"]; got != "Custom" {
		t.Fatalf("lost model: %v", got)
	}
	opt.DeviceModel = ""
	if _, ok := deviceMetadata(opt)["model"]; ok {
		t.Fatal("invented model for unknown device")
	}
}

func TestJSONLightNotAssumedState(t *testing.T) {
	opt := Options{DeviceID: "test-lamp", Prefix: "tuya-beacon-mesh-p2-controller/test-lamp", DiscoveryPrefix: "homeassistant", KelvinMin: 2700, KelvinMax: 6500}
	raw, err := discoveryJSON(opt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"optimistic":false`) {
		t.Fatalf("HA light should have one standard ON/OFF toggle: %s", raw)
	}
}
