package p2

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
)

// scenePreset is a DP73 strip_scene template observed for product wbl39qpt.
// All 28 RAW templates were captured from Smart Life. They represent observed
// settings, not necessarily factory defaults.
type scenePreset struct {
	ID       byte
	Name     string
	Raw      string
	Captured bool
}

var ScenePresets = [...]scenePreset{
	{0, "Good Night", "00000000640a40", true},
	{1, "Reading", "01000000646408", true},
	{2, "Work", "02000000646401", true},
	{3, "Leisure", "03000000643c04", true},
	{4, "Changes in temperature gradient", "040002004b644001", true},
	{5, "Changes in temperature jump", "0500010032644001", true},
	{6, "The white light breath", "060002003c640180", true},
	{7, "The white light jump", "0700010032640180", true},
	{8, "Warm light breath", "080002003c644080", true},
	{9, "Warm light jump", "0900010032644080", true},
	{10, "Full color gradient", "0a0002014b64010204102008", true},
	{11, "Blue & green gradient", "0b0002014e640204", true},
	{12, "Red & green gradient", "0c0002014b640104", true},
	{13, "Red & blue gradient", "0d00020150640102", true},
	{14, "Red & blue & green breath", "0e0002013c64018002800480", true},
	{15, "Red breathing", "0f0002013c640180", true},
	{16, "Green breathing", "100002013c640480", true},
	{17, "Blue breathing", "110002013c640280", true},
	{18, "Purple breathing", "120002013c642080", true},
	{19, "Yellow breathing", "130002013c641080", true},
	{20, "Full-color jump", "140001013264010204102008", true},
	{21, "Red & green & blue jump", "150001013264010204", true},
	{22, "Yellow & green jump", "1600010132640410", true},
	{23, "Red & green jump", "1700010132640104", true},
	{24, "Red flashing", "1800010132640180", true},
	{25, "Blue flashing", "1900010132640280", true},
	{26, "Green flashing", "1a00010132640480", true},
	{27, "RGB white jump", "1b00010132644080", true},
}

func GetScene(id byte) (scenePreset, bool) {
	if int(id) >= len(ScenePresets) {
		return scenePreset{}, false
	}
	return ScenePresets[id], true
}

func normalSceneName(s string) string {
	// Accept case-insensitive names and common kebab/underscore/space forms.
	// Ampersands are conjunctions, not significant for lookup.
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	fields := strings.Fields(b.String())
	out := fields[:0]
	for _, f := range fields {
		if f != "and" {
			out = append(out, f)
		}
	}
	return strings.Join(out, "")
}

func FindScene(input string) (scenePreset, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return scenePreset{}, errors.New("missing scene ID or name")
	}
	// Decimal identifiers, including 08/09; optional 0x for hexadecimal.
	var id uint64
	var err error
	if strings.HasPrefix(strings.ToLower(input), "0x") {
		id, err = strconv.ParseUint(input[2:], 16, 8)
	} else {
		id, err = strconv.ParseUint(input, 10, 8)
	}
	if err == nil {
		if p, ok := GetScene(byte(id)); ok {
			return p, nil
		}
		return scenePreset{}, fmt.Errorf("scene ID %d out of range (0..27)", id)
	}
	want := normalSceneName(input)
	for _, p := range ScenePresets {
		if normalSceneName(p.Name) == want {
			return p, nil
		}
	}
	return scenePreset{}, fmt.Errorf("unknown scene %q; run 'tuya-beacon-mesh-p2-controller scene list'", input)
}

func validSceneRaw(id byte, raw []byte) bool {
	return len(raw) >= 7 && len(raw) <= 14 && raw[0] == id
}

func PrintSceneList(w io.Writer, verbose bool) {
	if !verbose {
		fmt.Fprintln(w, "ID  NAME")
		for _, p := range ScenePresets {
			fmt.Fprintf(w, "%2d  %s\n", p.ID, p.Name)
		}
		return
	}
	fmt.Fprintln(w, "ID   NAME                               SPEED BRIGHT STATUS      RAW")
	for _, p := range ScenePresets {
		status := "captured"
		if !p.Captured {
			status = "inferred"
		}
		b, _ := hex.DecodeString(p.Raw)
		fmt.Fprintf(w, "%-4d %-34s %3d   %3d    %-9s   %s\n", p.ID, p.Name, b[4], b[5], status, p.Raw)
	}
	fmt.Fprintln(w, "\nSpeeds/brightness are captured values, not guaranteed factory defaults.")
}

// sceneRequest accepts a numeric ID or a quoted (or unquoted) multiword name,
// followed by optional --speed / --brightness parameters.
type SceneRequest struct {
	Preset     scenePreset
	Speed      *int
	Brightness *int
}

func ParseSceneRequest(args []string) (SceneRequest, error) {
	var req SceneRequest
	var nameParts []string
	for i := 0; i < len(args); i++ {
		s := args[i]
		field := ""
		val := ""
		switch {
		case strings.HasPrefix(s, "--speed="):
			field = "speed"
			val = strings.TrimPrefix(s, "--speed=")
		case strings.HasPrefix(s, "--brightness="):
			field = "brightness"
			val = strings.TrimPrefix(s, "--brightness=")
		case s == "--speed" || s == "--brightness":
			field = strings.TrimPrefix(s, "--")
			if i+1 >= len(args) {
				return req, fmt.Errorf("%s requires a value 1..100", s)
			}
			i++
			val = args[i]
		case strings.HasPrefix(s, "-"):
			return req, fmt.Errorf("unknown scene option %q", s)
		default:
			nameParts = append(nameParts, s)
			continue
		}
		n, err := ParseInt(val, 1, 100)
		if err != nil {
			return req, fmt.Errorf("--%s: %w", field, err)
		}
		if field == "speed" {
			if req.Speed != nil {
				return req, errors.New("duplicate --speed")
			}
			req.Speed = &n
		} else {
			if req.Brightness != nil {
				return req, errors.New("duplicate --brightness")
			}
			req.Brightness = &n
		}
	}
	var err error
	req.Preset, err = FindScene(strings.Join(nameParts, " "))
	return req, err
}

// sceneCommands deliberately uses the original bytes rather than attempting
// to synthesize undocumented animation flags or palette data.
// The monitor may supply a newer, *actually captured* RAW for the same scene.
// Returns inferred=true only for any future explicitly unverified template.
func SceneCommands(args []string, learned map[string]string) ([]DPCommand, SceneRequest, bool, error) {
	req, err := ParseSceneRequest(args)
	if err != nil {
		return nil, req, false, err
	}
	rawHex := req.Preset.Raw
	inferred := !req.Preset.Captured
	if saved := learned[strconv.Itoa(int(req.Preset.ID))]; saved != "" {
		if bytes, decErr := hex.DecodeString(saved); decErr == nil && validSceneRaw(req.Preset.ID, bytes) {
			rawHex = saved
			inferred = false
		}
	}
	raw, err := hex.DecodeString(rawHex)
	if err != nil || !validSceneRaw(req.Preset.ID, raw) {
		return nil, req, false, fmt.Errorf("invalid RAW template for scene %d", req.Preset.ID)
	}
	if req.Speed != nil {
		raw[4] = byte(*req.Speed)
	}
	if req.Brightness != nil {
		raw[5] = byte(*req.Brightness)
	}
	label := fmt.Sprintf("scene %d %s speed=%d brightness=%d", req.Preset.ID, req.Preset.Name, raw[4], raw[5])
	c, err := makeDP(label, 73, 0, raw)
	if err != nil {
		return nil, req, false, err
	}
	return []DPCommand{c}, req, inferred, nil
}
