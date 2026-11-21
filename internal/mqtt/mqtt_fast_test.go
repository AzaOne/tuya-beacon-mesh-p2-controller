package mqtt

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"example.com/tuya-beacon-mesh-p2-controller/internal/state"
)

func TestMQTTHAJsonDiscoveryAndCommands(t *testing.T) {
	opt := Options{DeviceID: "lamp", Prefix: "tuya/lamp", KelvinMin: 2700, KelvinMax: 6500, StatePath: filepath.Join(t.TempDir(), "state.json"), Fingerprint: "fp"}
	b, err := discoveryJSON(opt)
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err = json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]any{"schema": "json", "command_topic": "tuya/lamp/ha/set", "state_topic": "tuya/lamp/ha/state", "brightness_scale": float64(100), "optimistic": false} {
		if d[k] != v {
			t.Errorf("%s=%v want %v", k, d[k], v)
		}
	}
	if len(d["effect_list"].([]any)) != 28 {
		t.Fatal("28 scenes missing")
	}
	tests := []struct {
		body string
		want []string
	}{
		{`{"state":"OFF"}`, []string{"off"}},
		{`{"state":"ON"}`, []string{"on"}},
		{`{"state":"ON","effect":"Red & blue gradient","brightness":75}`, []string{"scene", "13", "--brightness", "75"}},
		{`{"state":"ON","color":{"r":255,"g":0,"b":0},"brightness":67}`, []string{"hsv", "0", "100", "67"}},
		{`{"state":"ON","color_temp":4000,"brightness":50}`, []string{"white", "4000", "50"}},
		{`{"state":"ON","brightness":0}`, []string{"off"}},
	}
	for _, tt := range tests {
		got, err := haCommand(opt, tt.body)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%q got=%v err=%v want=%v", tt.body, got, err, tt.want)
		}
	}
	for _, body := range []string{`{`, `{"brightness":120}`, `{"color":{"r":256,"g":0,"b":1}}`, `{"effect":"unknown"}`, `{"color_temp":1000}`, `{}`} {
		if _, err = haCommand(opt, body); err == nil {
			t.Errorf("accepted invalid %s", body)
		}
	}
}

func TestMQTTFastQueueHAOffPriority(t *testing.T) {
	q := newMQTTCommandQueue(100*time.Millisecond, 450*time.Millisecond, 350*time.Millisecond)
	now := time.Unix(123, 0)
	topic := "test/lamp/ha/set"
	for n := 1; n <= 135; n++ {
		v := `{"state":"ON","brightness":` + strconv.Itoa(n%100+1) + `}`
		q.offer(topic, v, now.Add(time.Duration(n)*time.Millisecond))
	}
	job, ok := q.nextReady(now.Add(time.Second))
	if !ok {
		t.Fatal("no ready job")
	}
	if !strings.Contains(job.Payload, `"brightness":36`) {
		t.Fatalf("last payload was not selected %s", job.Payload)
	}
	q.complete(job, nil, now.Add(time.Second))
	q.offer(topic, `{"state":"ON","brightness":55}`, now.Add(time.Second))
	q.offer(topic, `{"state":"OFF"}`, now.Add(time.Second+20*time.Millisecond))
	off, ok := q.nextReady(now.Add(time.Second + 20*time.Millisecond))
	if !ok || off.Payload != `{"state":"OFF"}` {
		t.Fatalf("OFF was not prioritized: %+v %v", off, ok)
	}
}

func TestMQTTHAState(t *testing.T) {
	opt := Options{Prefix: "t", KelvinMin: 2700, KelvinMax: 6500}
	if _, ok := haLightState(opt, state.Snapshot{}); ok {
		t.Fatal("invented state")
	}
	power := true
	mode := byte(1)
	hsv := [3]int{0, 100, 55}
	b, ok := haLightState(opt, state.Snapshot{Light: &state.LightState{Power: &power, Mode: &mode, HSV: &hsv}})
	if !ok {
		t.Fatal("no state")
	}
	var data map[string]any
	if e := json.Unmarshal(b, &data); e != nil {
		t.Fatal(e)
	}
	if data["state"] != "ON" || data["brightness"] != float64(55) || data["color_mode"] != "rgb" {
		t.Fatalf("bad state %v", data)
	}
}
