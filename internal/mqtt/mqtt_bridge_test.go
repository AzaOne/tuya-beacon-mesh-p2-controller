package mqtt

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
)

func bridgeTestOptions() Options {
	return Options{
		Prefix: "tuya-beacon-mesh-p2-controller/test-lamp", DeviceID: "test-lamp",
		ClientID: "tuya-beacon-mesh-p2-controller", DeviceName: "Smart Bulb 2", Firmware: "1.2",
		ProductID: "wbl39qpt", Adapter: "hci0", Backend: "raw-hci",
		Discovery: true, DiscoveryPrefix: "homeassistant", KelvinMin: 2700, KelvinMax: 6500,
	}
}

func TestMQTTBridgeZ2MStyleTopics(t *testing.T) {
	opt := bridgeTestOptions()
	if got := rootPrefix(opt); got != "tuya-beacon-mesh-p2-controller" {
		t.Fatal(got)
	}
	for _, check := range []struct{ actual, want string }{
		{bridgeStateTopic(opt), "tuya-beacon-mesh-p2-controller/bridge/state"},
		{bridgeInfoTopic(opt), "tuya-beacon-mesh-p2-controller/bridge/info"},
		{bridgeDevicesTopic(opt), "tuya-beacon-mesh-p2-controller/bridge/devices"},
	} {
		if check.actual != check.want {
			t.Fatalf("got %s want %s", check.actual, check.want)
		}
	}
	b, err := bridgeInfoJSON(opt)
	if err != nil {
		t.Fatal(err)
	}
	var info map[string]any
	if err := json.Unmarshal(b, &info); err != nil {
		t.Fatal(err)
	}
	if info["version"] != softwareVersion || info["adapter"] != "hci0" || info["transport"] != "raw-hci" {
		t.Fatal(info)
	}
	b, err = bridgeDevicesJSON(opt)
	if err != nil {
		t.Fatal(err)
	}
	var devices []map[string]any
	if err := json.Unmarshal(b, &devices); err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0]["product_id"] != "wbl39qpt" {
		t.Fatal(devices)
	}
	if string(bridgeStateJSON("offline")) != `{"state":"offline"}` {
		t.Fatal("invalid bridge LWT state")
	}
}

func TestMQTTBridgeDiagnosticsAndDeviceLink(t *testing.T) {
	opt := bridgeTestOptions()
	entries, err := discoveryEntries(opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 6 {
		t.Fatalf("expected bridge state, light, speed, 3 diagnostics, got %d", len(entries))
	}
	topics := make(map[string]bool)
	for _, entry := range entries {
		if topics[entry.Topic] {
			t.Fatal("duplicate topic", entry.Topic)
		}
		topics[entry.Topic] = true
		var payload map[string]any
		if err := json.Unmarshal(entry.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		dev := payload["device"].(map[string]any)
		if strings.Contains(entry.Topic, "/sensor/") || strings.Contains(entry.Topic, "/binary_sensor/") {
			if dev["name"] != "tuya-beacon-mesh-p2-controller Bridge" || dev["model"] != "Bridge" {
				t.Fatal(dev)
			}
			if payload["entity_category"] != "diagnostic" {
				t.Fatal(payload)
			}
		} else {
			if dev["via_device"] != controllerID(opt) {
				t.Fatal(dev)
			}
			if !reflect.DeepEqual(dev["identifiers"], []any{"tuya_p2_test-lamp"}) {
				t.Fatal(dev)
			}
		}
	}
	lightJSON, _ := discoveryJSON(opt)
	var light map[string]any
	_ = json.Unmarshal(lightJSON, &light)
	if light["name"] != "Light" {
		t.Fatalf("main light should be named Light instead of duplicating device name, got %v", light["name"])
	}
	for _, entity := range []string{"version", "adapter", "transport"} {
		expected := "homeassistant/sensor/" + controllerID(opt) + "_" + entity + "/config"
		if !topics[expected] {
			t.Fatalf("missing %s", expected)
		}
	}
}

// Verify MQTT Last Will uses the new bridge/state topic (not a stale per-device
// availability that could incorrectly mark every light online after a crash).
func TestMQTTConnectBridgeLWT(t *testing.T) {
	opt := bridgeTestOptions()
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		_ = server.SetDeadline(time.Now().Add(2 * time.Second))
		k, packet, err := readPacket(bufio.NewReader(server))
		if err == nil {
			if k != 0x10 || !strings.Contains(string(packet), "tuya-beacon-mesh-p2-controller/bridge/state") || !strings.Contains(string(packet), `{"state":"offline"}`) {
				err = errors.New("CONNECT does not contain bridge LWT")
			} else {
				_, err = server.Write([]byte{0x20, 0x02, 0x00, 0x00})
			}
		}
		done <- err
	}()
	_, err := connect(client, opt)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestMQTTBridgeDevicePrefixParsing(t *testing.T) {
	for _, tt := range []struct{ prefix, id, want string }{
		{"myroot/abc", "abc", "myroot"},
		{"tuya-beacon-mesh-p2-controller/test-lamp", "test-lamp", "tuya-beacon-mesh-p2-controller"},
		{"mysystem", "abc", "mysystem"},
	} {
		if got := rootPrefix(Options{Prefix: tt.prefix, DeviceID: tt.id}); got != tt.want {
			t.Fatal(got, tt)
		}
	}
}
