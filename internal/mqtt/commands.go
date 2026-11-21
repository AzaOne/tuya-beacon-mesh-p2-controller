package mqtt

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
	"example.com/tuya-beacon-mesh-p2-controller/internal/state"
)

func allowedCommand(payload string) ([]string, error) {
	args := strings.Fields(strings.TrimSpace(payload))
	if len(args) == 0 {
		return nil, errors.New("empty command")
	}
	allowed := map[string]bool{"on": true, "off": true, "brightness": true, "brightness-raw": true, "temp": true, "temp-raw": true, "white": true, "rgb": true, "hsv": true, "mode": true, "scene": true}
	if !allowed[strings.ToLower(args[0])] {
		return nil, errors.New("MQTT supports standard lighting commands only")
	}
	if strings.EqualFold(args[0], "scene") && len(args) > 1 {
		if args[1] == "list" || args[1] == "current" {
			return nil, errors.New("scene list/current are not MQTT actions")
		}
		if _, err := p2.ParseSceneRequest(args[1:]); err != nil {
			return nil, err
		}
	}
	return args, nil
}

func commandForTopic(prefix, topic, payload string) ([]string, error) {
	if topic == prefix+"/set" {
		return allowedCommand(payload)
	}
	s := strings.TrimSpace(payload)
	switch topic {
	case prefix + "/power/set":
		switch strings.ToUpper(s) {
		case "ON", "TRUE", "1":
			return []string{"on"}, nil
		case "OFF", "FALSE", "0":
			return []string{"off"}, nil
		}
		return nil, errors.New("power must be ON/OFF")
	case prefix + "/brightness/set":
		v, err := p2.ParseInt(s, 1, 100)
		if err != nil {
			return nil, err
		}
		return []string{"brightness", strconv.Itoa(v)}, nil
	case prefix + "/color/set":
		// HA RGB Light emits comma-separated 8-bit RGB, not DP11/HSV.
		vals := strings.Split(s, ",")
		if len(vals) != 3 {
			return nil, errors.New("RGB needs R,G,B")
		}
		cmd := []string{"rgb"}
		for _, x := range vals {
			v, err := p2.ParseInt(strings.TrimSpace(x), 0, 255)
			if err != nil {
				return nil, err
			}
			cmd = append(cmd, strconv.Itoa(v))
		}
		return cmd, nil
	case prefix + "/temperature/set":
		n, err := strconv.Atoi(s)
		if err != nil {
			return nil, err
		}
		return []string{"temp", strconv.Itoa(n)}, nil
	case prefix + "/effect/speed/set":
		value, err := p2.ParseInt(s, 1, 100)
		if err != nil {
			return nil, err
		}
		// Resolving the selected scene needs the persisted state. This marker
		// is replaced with an actual scene command by the MQTT worker.
		return []string{"__effect-speed", strconv.Itoa(value)}, nil
	case prefix + "/effect/set":
		preset, err := p2.FindScene(s)
		if err != nil {
			return nil, err
		}
		return []string{"scene", strconv.Itoa(int(preset.ID))}, nil
	default:
		return nil, errors.New("unknown MQTT command topic")
	}
}

// haCommand decodes a single Home Assistant JSON light command into one
// local command. RGB+brightness, temperature+brightness and scene+brightness
// become one logical action, not separate MQTT tasks.
func haCommand(opt Options, payload string) ([]string, error) {
	var c struct {
		State      string `json:"state"`
		Brightness *int   `json:"brightness"`
		Color      *struct {
			R int `json:"r"`
			G int `json:"g"`
			B int `json:"b"`
		} `json:"color"`
		ColorTemp *int    `json:"color_temp"`
		Effect    *string `json:"effect"`
	}
	if err := json.Unmarshal([]byte(payload), &c); err != nil {
		return nil, fmt.Errorf("invalid HA JSON command: %w", err)
	}
	if c.State != "" && c.State != "ON" && c.State != "OFF" {
		return nil, errors.New("HA JSON state must be ON/OFF")
	}
	if c.State == "OFF" {
		return []string{"off"}, nil
	}
	if c.Brightness != nil && (*c.Brightness < 0 || *c.Brightness > 100) {
		return nil, errors.New("HA brightness must be 0..100")
	}
	if c.Brightness != nil && *c.Brightness == 0 {
		return []string{"off"}, nil
	}
	if c.Effect != nil && *c.Effect != "" {
		preset, err := p2.FindScene(*c.Effect)
		if err != nil {
			return nil, err
		}
		args := []string{"scene", strconv.Itoa(int(preset.ID))}
		if c.Brightness != nil {
			args = append(args, "--brightness", strconv.Itoa(*c.Brightness))
		}
		return args, nil
	}
	if c.Color != nil {
		if c.Color.R < 0 || c.Color.R > 255 || c.Color.G < 0 || c.Color.G > 255 || c.Color.B < 0 || c.Color.B > 255 {
			return nil, errors.New("HA RGB components must be 0..255")
		}
		h, sat, val := p2.RgbToHSV(uint8(c.Color.R), uint8(c.Color.G), uint8(c.Color.B))
		if c.Brightness != nil {
			val = *c.Brightness
		}
		return []string{"hsv", strconv.Itoa(h), strconv.Itoa(sat), strconv.Itoa(val)}, nil
	}
	if c.ColorTemp != nil {
		if *c.ColorTemp < opt.KelvinMin || *c.ColorTemp > opt.KelvinMax {
			return nil, fmt.Errorf("HA temperature outside %d..%d K", opt.KelvinMin, opt.KelvinMax)
		}
		if c.Brightness != nil {
			return []string{"white", strconv.Itoa(*c.ColorTemp), strconv.Itoa(*c.Brightness)}, nil
		}
		return []string{"temp", strconv.Itoa(*c.ColorTemp)}, nil
	}
	if c.Brightness != nil {
		return brightnessCommand(opt, strconv.Itoa(*c.Brightness))
	}
	if c.State == "ON" {
		return []string{"on"}, nil
	}
	return nil, errors.New("HA JSON command did not contain a light change")
}

func validateHACommand(opt Options, payload string) error {
	_, err := haCommand(opt, payload)
	return err
}

// Preserve current effect ID and brightness while changing only DP73 byte 4.
// A scene must actually be selected; otherwise the command is ambiguous.
func effectSpeedCommand(opt Options, payload string) ([]string, error) {
	speed, err := p2.ParseInt(strings.TrimSpace(payload), 1, 100)
	if err != nil {
		return nil, fmt.Errorf("effect speed: %w", err)
	}
	st, err := state.Read(opt.StatePath, opt.DeviceID, opt.Fingerprint)
	if err != nil {
		return nil, err
	}
	if st.Light == nil || st.Light.Mode == nil || *st.Light.Mode != 2 || st.Light.Scene == nil {
		return nil, errors.New("effect speed: select a scene first")
	}
	scene := st.Light.Scene
	if _, ok := p2.GetScene(byte(scene.ID)); !ok || scene.ID < 0 || scene.ID > 27 {
		return nil, errors.New("effect speed: invalid active scene")
	}
	return []string{"scene", strconv.Itoa(scene.ID), "--speed", strconv.Itoa(speed), "--brightness", strconv.Itoa(scene.Brightness)}, nil
}

// Brightness means a different DP in white, colour and scene modes.
// Reuse the mode and values last seen over radio instead of blindly issuing DP3.
func brightnessCommand(opt Options, payload string) ([]string, error) {
	n, err := p2.ParseInt(strings.TrimSpace(payload), 1, 100)
	if err != nil {
		return nil, err
	}
	st, err := state.Read(opt.StatePath, opt.DeviceID, opt.Fingerprint)
	if err != nil {
		return nil, err
	}
	if st.Light != nil && st.Light.Mode != nil {
		switch *st.Light.Mode {
		case 2:
			if st.Light.Scene != nil {
				sc := st.Light.Scene
				return []string{"scene", strconv.Itoa(sc.ID), "--speed", strconv.Itoa(sc.Speed), "--brightness", strconv.Itoa(n)}, nil
			}
		case 1:
			if st.Light.HSV != nil {
				hsv := st.Light.HSV
				return []string{"hsv", strconv.Itoa((*hsv)[0]), strconv.Itoa((*hsv)[1]), strconv.Itoa(n)}, nil
			}
		}
	}
	return []string{"brightness", strconv.Itoa(n)}, nil
}
