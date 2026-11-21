package mqtt

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"

	"example.com/tuya-beacon-mesh-p2-controller/internal/state"
)

func stateJSON(path, device, fp string) ([]byte, error) {
	st, err := state.Read(path, device, fp)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		LastSeq   uint32            `json:"last_seq"`
		Light     *state.LightState `json:"light,omitempty"`
		Confirmed bool              `json:"confirmed"`
	}{st.LastReserved, st.Light, false})
}

func hsvToRGB(hsv [3]int) string {
	h := math.Mod(float64(hsv[0]), 360)
	s := math.Max(0, math.Min(1, float64(hsv[1])/100))
	v := math.Max(0, math.Min(1, float64(hsv[2])/100))
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := v - c
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return fmt.Sprintf("%d,%d,%d", int(math.Round((r+m)*255)), int(math.Round((g+m)*255)), int(math.Round((b+m)*255)))
}

func stateTopics(opt Options, st state.Snapshot) map[string]string {
	p := opt.Prefix
	out := map[string]string{}
	if st.Light == nil {
		return out
	}
	v := st.Light
	if v.Power != nil {
		if *v.Power {
			out[p+"/power/state"] = "ON"
		} else {
			out[p+"/power/state"] = "OFF"
		}
	}
	if v.Mode != nil {
		switch *v.Mode {
		case 0:
			out[p+"/color_mode/state"] = "color_temp"
		case 1, 2, 3:
			out[p+"/color_mode/state"] = "rgb"
		}
	}
	if v.Mode != nil && *v.Mode == 2 && v.Scene != nil {
		out[p+"/brightness/state"] = strconv.Itoa(v.Scene.Brightness)
	} else if v.Mode != nil && *v.Mode == 1 && v.HSV != nil {
		out[p+"/brightness/state"] = strconv.Itoa((*v.HSV)[2])
	} else if v.BrightnessRaw != nil {
		out[p+"/brightness/state"] = strconv.Itoa(int(math.Round(float64(*v.BrightnessRaw) / 10)))
	}
	if v.Mode != nil && *v.Mode == 2 && v.Scene != nil {
		out[p+"/effect/speed/state"] = strconv.Itoa(v.Scene.Speed)
	} else {
		// Clear previous retained scene-speed when a non-scene mode is chosen.
		out[p+"/effect/speed/state"] = "unknown"
	}
	if v.HSV != nil {
		out[p+"/color/state"] = hsvToRGB(*v.HSV)
	}
	if v.TemperatureRaw != nil {
		value := int(*v.TemperatureRaw)
		if opt.InvertTemp {
			value = 1000 - value
		}
		kelvin := opt.KelvinMin + int(math.Round(float64(value)/1000*float64(opt.KelvinMax-opt.KelvinMin)))
		out[p+"/temperature/state"] = strconv.Itoa(kelvin)
	}
	if v.Mode != nil && *v.Mode != 2 {
		out[p+"/effect/state"] = ""
	} else if v.Scene != nil {
		out[p+"/effect/state"] = v.Scene.Name
	}
	return out
}

// MQTT JSON light state, published only when there is an observed/commanded
// power value. No wireless acknowledgement is implied.
func haLightState(opt Options, st state.Snapshot) ([]byte, bool) {
	if st.Light == nil || st.Light.Power == nil {
		return nil, false
	}
	v := st.Light
	state := "OFF"
	if *v.Power {
		state = "ON"
	}
	data := map[string]any{"state": state}
	if v.Mode != nil {
		switch *v.Mode {
		case 0:
			data["color_mode"] = "color_temp"
		case 1:
			data["color_mode"] = "rgb"
		}
	}
	if v.Mode != nil && *v.Mode == 2 && v.Scene != nil {
		data["brightness"] = v.Scene.Brightness
		data["effect"] = v.Scene.Name
	} else if v.Mode != nil && *v.Mode == 1 && v.HSV != nil {
		data["brightness"] = (*v.HSV)[2]
	} else if v.BrightnessRaw != nil {
		data["brightness"] = int(math.Round(float64(*v.BrightnessRaw) / 10))
	}
	if v.Mode != nil && *v.Mode == 1 && v.HSV != nil {
		rgb := strings.Split(hsvToRGB(*v.HSV), ",")
		r, _ := strconv.Atoi(rgb[0])
		g, _ := strconv.Atoi(rgb[1])
		b, _ := strconv.Atoi(rgb[2])
		data["color"] = map[string]int{"r": r, "g": g, "b": b}
	}
	if v.TemperatureRaw != nil && v.Mode != nil && *v.Mode == 0 {
		temp := int(*v.TemperatureRaw)
		if opt.InvertTemp {
			temp = 1000 - temp
		}
		data["color_temp"] = opt.KelvinMin + int(math.Round(float64(temp)/1000*float64(opt.KelvinMax-opt.KelvinMin)))
	}
	b, _ := json.Marshal(data)
	return b, true
}

// A state update is sent only when lighting properties change. In particular,
// reserving another seq does not re-publish every HA light attribute.
type snapshotCache struct {
	initialized    bool
	lightSignature string
	properties     map[string]string
}

func lightSignature(light *state.LightState) string {
	if light == nil {
		return "null"
	}
	copy := *light
	// These fields identify the origin/time of a command, not a change to the
	// light's visible state. Smart Life often repeats the same DP many times.
	copy.UpdatedAt = ""
	copy.LastSource = ""
	b, _ := json.Marshal(copy)
	return string(b)
}

func publishSnapshot(conn net.Conn, opt Options, cache *snapshotCache) error {
	snapshot, err := state.Read(opt.StatePath, opt.DeviceID, opt.Fingerprint)
	if err != nil {
		return err
	}
	signature := lightSignature(snapshot.Light)
	if cache.initialized && cache.lightSignature == signature {
		return nil
	}
	raw, err := json.Marshal(struct {
		LastSeq   uint32            `json:"last_seq"`
		Light     *state.LightState `json:"light,omitempty"`
		Confirmed bool              `json:"confirmed"`
	}{snapshot.LastReserved, snapshot.Light, false})
	if err != nil {
		return err
	}
	if err := publish(conn, opt.Prefix+"/state", raw, true, 0, 0); err != nil {
		return err
	}
	if cache.properties == nil {
		cache.properties = map[string]string{}
	}
	for topic, value := range stateTopics(opt, snapshot) {
		if previous, ok := cache.properties[topic]; ok && previous == value {
			continue
		}
		if err := publish(conn, topic, []byte(value), true, 0, 0); err != nil {
			return err
		}
		cache.properties[topic] = value
	}
	if ha, ok := haLightState(opt, snapshot); ok {
		if err := publish(conn, opt.Prefix+"/ha/state", ha, true, 0, 0); err != nil {
			return err
		}
	}
	cache.lightSignature = signature
	cache.initialized = true
	return nil
}
