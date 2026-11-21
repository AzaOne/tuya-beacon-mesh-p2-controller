package mqtt

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"example.com/tuya-beacon-mesh-p2-controller/internal/state"
)

func TestMQTTEffectSpeedDiscoveryAndTopics(t *testing.T) {
	o := Options{DeviceID: "lamp-001", DeviceName: "Smart Bulb 2", Prefix: "lights/lamp-001", DiscoveryPrefix: "homeassistant"}
	if got := speedDiscoveryTopic(o); got != "homeassistant/number/tuya_p2_lamp-001_effect_speed/config" {
		t.Fatalf("discovery topic %q", got)
	}
	b, err := speedDiscoveryJSON(o)
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err = json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	if d["min"] != float64(1) || d["max"] != float64(100) || d["step"] != float64(1) || d["mode"] != "slider" {
		t.Fatalf("speed number options: %v", d)
	}
	if d["command_topic"] != "lights/lamp-001/effect/speed/set" || d["state_topic"] != "lights/lamp-001/effect/speed/state" {
		t.Fatalf("MQTT speed topic %v", d)
	}
	dev := d["device"].(map[string]any)
	if !reflect.DeepEqual(dev["identifiers"], []any{"tuya_p2_lamp-001"}) {
		t.Fatalf("different HA device %v", dev)
	}
	if _, err := commandForTopic(o.Prefix, o.Prefix+"/effect/speed/set", "57"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"0", "101", "hello", "-4"} {
		if _, err := commandForTopic(o.Prefix, o.Prefix+"/effect/speed/set", bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestMQTTEffectSpeedKeepsActiveSceneAndBrightness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	o := Options{StatePath: path, DeviceID: "bulb", Fingerprint: "fp", Prefix: "lights/bulb"}
	if _, err := effectSpeedCommand(o, "50"); err == nil {
		t.Fatal("speed sent without active scene")
	}
	st, err := state.Open(path, o.DeviceID, o.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	mode := byte(2)
	st.State.Light = &state.LightState{Mode: &mode, Scene: &state.SceneStatus{ID: 13, Name: "Red & blue gradient", Speed: 80, Brightness: 26}}
	if err := st.Write(); err != nil {
		t.Fatal(err)
	}
	st.Close()
	cmd, err := effectSpeedCommand(o, "60")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"scene", "13", "--speed", "60", "--brightness", "26"}
	if !reflect.DeepEqual(cmd, want) {
		t.Errorf("%v != %v", cmd, want)
	}
	got, err := state.Read(path, o.DeviceID, o.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	topics := stateTopics(o, got)
	if topics[o.Prefix+"/effect/speed/state"] != "80" {
		t.Fatalf("wrong speed %v", topics)
	}
	mode = byte(0)
	st, err = state.Open(path, o.DeviceID, o.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	st.State.Light.Mode = &mode
	if err = st.Write(); err != nil {
		t.Fatal(err)
	}
	st.Close()
	if _, err := effectSpeedCommand(o, "60"); err == nil {
		t.Fatal("speed command outside scene mode")
	}
	after, err := state.Read(path, o.DeviceID, o.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if topics = stateTopics(o, after); topics[o.Prefix+"/effect/speed/state"] != "unknown" {
		t.Errorf("stale HA speed not cleared %v", topics)
	}
}
