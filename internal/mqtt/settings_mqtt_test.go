package mqtt

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"example.com/tuya-beacon-mesh-p2-controller/internal/config"
	"example.com/tuya-beacon-mesh-p2-controller/internal/state"
)

func TestDefaultFrameCountAndJSONOverrides(t *testing.T) {
	c := config.Defaults()
	if c.Controller.Frames != 1 {
		t.Fatalf("default frames=%d want 1", c.Controller.Frames)
	}
	if !c.MQTT.Enabled || !c.MQTT.HADiscoveryEnabled {
		t.Fatalf("default mqtt settings wrong %+v", c.MQTT)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"controller":{"frames":3,"duration":"700ms","multi_dp":false,"invert_temp":true,"scan":false,"src":"0x0042"},"mqtt":{"enabled":true,"broker":"tcp://127.0.0.1:1883","topic_prefix":"bledom","client_id":"unique-controller","ha_discovery_enabled":false}}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Controller.Frames != 3 || loaded.Controller.MultiDP || !loaded.Controller.InvertTemp || loaded.Controller.Scan || loaded.Controller.Src != "0x0042" {
		t.Fatalf("incorrect JSON controller %+v", loaded.Controller)
	}
	if loaded.Controller.Gap != "120ms" {
		t.Fatalf("missing controller option should use default: %v", loaded.Controller.Gap)
	}
	if loaded.MQTT.HADiscoveryEnabled || loaded.MQTT.TopicPrefix != "bledom" || loaded.MQTT.ClientID != "unique-controller" {
		t.Fatalf("incorrect JSON mqtt %+v", loaded.MQTT)
	}
	if got := config.PathFromArgs([]string{"-frames", "4", "-config", path, "on"}); got != path {
		t.Fatalf("flag path %q", got)
	}
	if got := DevicePrefix("bledom", "example-device"); got != "bledom/example-device" {
		t.Fatalf("prefix %q", got)
	}
	if got := DevicePrefix("bledom/example-device", "example-device"); got != "bledom/example-device" {
		t.Fatalf("duplicate device suffix %q", got)
	}
	bad := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(bad, []byte(`{"controller":{"duration":"not-a-duration"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Read(bad); err == nil {
		t.Fatal("invalid duration accepted")
	}
}

func TestHADiscoveryAllScenesAndTopics(t *testing.T) {
	o := Options{DeviceID: "bulb-device", DeviceName: "Smart Bulb 2", Prefix: "bledom/bulb-device", Discovery: true, DiscoveryPrefix: "homeassistant", KelvinMin: 2700, KelvinMax: 6500}
	b, err := discoveryJSON(o)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"state_topic":       "bledom/bulb-device/ha/state",
		"command_topic":     "bledom/bulb-device/ha/set",
		"schema":            "json",
		"effect":            true,
		"brightness_scale":  float64(100),
		"color_temp_kelvin": true,
	}
	for k, v := range want {
		if !reflect.DeepEqual(data[k], v) {
			t.Fatalf("discovery %s=%v want %v", k, data[k], v)
		}
	}
	effects, ok := data["effect_list"].([]any)
	if !ok || len(effects) != 28 {
		t.Fatalf("expected 28 effects got %v", data["effect_list"])
	}
	if effects[13] != "Red & blue gradient" {
		t.Fatalf("scene13 %v", effects[13])
	}
	if topic := discoveryTopic(o); topic != "homeassistant/light/tuya_p2_bulb-device/config" {
		t.Fatalf("topic %q", topic)
	}
	for _, test := range []struct {
		topic, value string
		want         []string
	}{
		{"power/set", "ON", []string{"on"}},
		{"brightness/set", "51", []string{"brightness", "51"}},
		{"color/set", "100,232,111", []string{"rgb", "100", "232", "111"}},
		{"temperature/set", "4000", []string{"temp", "4000"}},
		{"effect/set", "Red & blue gradient", []string{"scene", "13"}},
		{"set", "scene 8 --speed 80", []string{"scene", "8", "--speed", "80"}},
	} {
		got, err := commandForTopic(o.Prefix, o.Prefix+"/"+test.topic, test.value)
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Errorf("topic %q value %q => %v, %v", test.topic, test.value, got, err)
		}
	}
	for _, test := range []struct{ topic, value string }{
		{"color/set", "not,rgb"}, {"power/set", "maybe"}, {"brightness/set", "300"}, {"effect/set", "Unknown Scene"},
	} {
		if _, err := commandForTopic(o.Prefix, o.Prefix+"/"+test.topic, test.value); err == nil {
			t.Errorf("accepted %q %q", test.topic, test.value)
		}
	}
	for _, input := range []string{"tcp://localhost:1883", "localhost:1883"} {
		host, err := addressHost(input)
		if err != nil || host != "localhost:1883" {
			t.Fatalf("address %q: %q %v", input, host, err)
		}
	}
	for _, input := range []string{"ssl://localhost:8883", "tcp://localhost:1883/a"} {
		if _, err := addressHost(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	if strings.Contains(string(b), "beacon_key") || strings.Contains(string(b), "local_key") {
		t.Fatal("discovery contains secrets")
	}
}

func TestMQTTStateIsOnlyReportedWhenKnown(t *testing.T) {
	o := Options{Prefix: "t/dev", KelvinMin: 2700, KelvinMax: 6500}
	if v := stateTopics(o, state.Snapshot{}); len(v) != 0 {
		t.Fatalf("invented initial state %+v", v)
	}
	on := true
	bright := uint32(800)
	rawTemp := uint32(500)
	hsv := [3]int{120, 100, 100}
	scene := &state.SceneStatus{Name: "Red breathing", ID: 15}
	st := state.Snapshot{Light: &state.LightState{Power: &on, BrightnessRaw: &bright, TemperatureRaw: &rawTemp, HSV: &hsv, Scene: scene}}
	got := stateTopics(o, st)
	checks := map[string]string{"t/dev/power/state": "ON", "t/dev/brightness/state": "80", "t/dev/color/state": "0,255,0", "t/dev/temperature/state": "4600", "t/dev/effect/state": "Red breathing"}
	for k, v := range checks {
		if got[k] != v {
			t.Errorf("%s = %q want %q", k, got[k], v)
		}
	}
}

func TestMQTTSceneAndColourBrightnessRouting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	opt := Options{StatePath: path, DeviceID: "test-light", Fingerprint: "fp", Prefix: "lights/test-light", KelvinMin: 2700, KelvinMax: 6500}
	st, err := state.Open(path, opt.DeviceID, opt.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	sceneMode := byte(2)
	st.State.Light = &state.LightState{Mode: &sceneMode, Scene: &state.SceneStatus{ID: 13, Speed: 80, Brightness: 56, Name: "Red & blue gradient"}}
	if err := st.Write(); err != nil {
		t.Fatal(err)
	}
	st.Close()
	cmd, err := brightnessCommand(opt, "70")
	if err != nil || !reflect.DeepEqual(cmd, []string{"scene", "13", "--speed", "80", "--brightness", "70"}) {
		t.Fatalf("scene brightness cmd %v %v", cmd, err)
	}
	st, err = state.Open(path, opt.DeviceID, opt.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	colourMode := byte(1)
	st.State.Light = &state.LightState{Mode: &colourMode, HSV: &[3]int{120, 80, 56}}
	if err := st.Write(); err != nil {
		t.Fatal(err)
	}
	st.Close()
	cmd, err = brightnessCommand(opt, "75")
	if err != nil || !reflect.DeepEqual(cmd, []string{"hsv", "120", "80", "75"}) {
		t.Fatalf("colour brightness cmd %v %v", cmd, err)
	}
}

func TestMQTTConnectWillAndRetainedDiscovery(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	opt := Options{DeviceID: "demo", ClientID: "tuya-beacon-mesh-p2-controller-demo", User: "me", Password: "secret", Prefix: "tuya-beacon-mesh-p2-controller/demo", DiscoveryPrefix: "homeassistant", KelvinMin: 2700, KelvinMax: 6500}
	errorsC := make(chan error, 1)
	go func() {
		rd := bufio.NewReader(server)
		kind, body, err := readPacket(rd)
		if err != nil {
			errorsC <- err
			return
		}
		if kind != 0x10 || !bytes.Contains(body, []byte("offline")) || !bytes.Contains(body, []byte("tuya-beacon-mesh-p2-controller-demo")) {
			errorsC <- fmt.Errorf("invalid CONNECT %x: %q", kind, body)
			return
		}
		if err := writePacket(server, 0x20, []byte{0, 0}); err != nil {
			errorsC <- err
			return
		}
		kind, body, err = readPacket(rd)
		if err != nil {
			errorsC <- err
			return
		}
		if kind != 0x33 || !bytes.Contains(body, []byte(`"effect_list"`)) {
			errorsC <- fmt.Errorf("retained QoS1 discovery missing %x", kind)
			return
		}
		errorsC <- nil
	}()
	rd, err := connect(client, opt)
	if err != nil || rd == nil {
		t.Fatalf("connect %v", err)
	}
	b, _ := discoveryJSON(opt)
	if err := publish(client, discoveryTopic(opt), b, true, 1, 42); err != nil {
		t.Fatal(err)
	}
	if err := <-errorsC; err != nil {
		t.Fatal(err)
	}
}
