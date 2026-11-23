package p2

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	frameSize        = 26
	delta     uint32 = 0x9e3779b9
)

type DPCommand struct {
	Label string
	Plain [16]byte
}

type WireFrame struct {
	Frame [frameSize]byte
	Adv   [31]byte
	Seq   uint16
	Label string
}

// P2 data-point is [id][type<<4 | length][value...] with zero padding to 16 bytes.
// Not the generic Tuya WiFi data-point encoding.
func makeDP(label string, id, typ byte, value []byte) (DPCommand, error) {
	var out DPCommand
	if len(value) > 14 {
		return out, fmt.Errorf("DP value too long: %d bytes (maximum 14)", len(value))
	}
	if typ > 15 {
		return out, errors.New("DP type must be 0..15")
	}
	out.Label = label
	out.Plain[0], out.Plain[1] = id, (typ<<4)|byte(len(value))
	copy(out.Plain[2:], value)
	return out, nil
}

func MustDP(label string, id, typ byte, val []byte) DPCommand {
	out, err := makeDP(label, id, typ, val)
	if err != nil {
		panic(err)
	}
	return out
}

func BoolDP(on bool) DPCommand {
	b := byte(0)
	label := "off"
	if on {
		b = 1
		label = "on"
	}
	return MustDP(label, 1, 1, []byte{b})
}

func ModeDP(mode byte) DPCommand {
	labels := []string{"white", "colour", "scene", "music"}
	label := "mode"
	if int(mode) < len(labels) {
		label = "mode " + labels[mode]
	}
	return MustDP(label, 2, 4, []byte{mode})
}

func ValueDP(label string, id byte, val uint32) DPCommand {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], val)
	return MustDP(label, id, 2, b[:])
}

func HsvDP(h int, s, v int) DPCommand {
	var b [4]byte
	binary.BigEndian.PutUint16(b[:2], uint16(h))
	b[2], b[3] = byte(s), byte(v)
	return MustDP(fmt.Sprintf("hsv %d %d %d", h, s, v), 11, 0, b[:])
}

func RgbToHSV(r, g, b uint8) (h, s, v int) {
	rf, gf, bf := float64(r)/255, float64(g)/255, float64(b)/255
	maxv := math.Max(rf, math.Max(gf, bf))
	minv := math.Min(rf, math.Min(gf, bf))
	d := maxv - minv
	hue := 0.0
	if d != 0 {
		switch maxv {
		case rf:
			hue = math.Mod((gf-bf)/d, 6)
		case gf:
			hue = (bf-rf)/d + 2
		default:
			hue = (rf-gf)/d + 4
		}
		hue *= 60
		if hue < 0 {
			hue += 360
		}
	}
	sat := 0.0
	if maxv != 0 {
		sat = d / maxv
	}
	return int(math.Round(hue)) % 360, int(math.Round(sat * 100)), int(math.Round(maxv * 100))
}

func ParseInt(s string, min, max int) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < min || n > max {
		return 0, fmt.Errorf("%q must be integer in range %d..%d", s, min, max)
	}
	return n, nil
}

func ParseHexByte(s string) (byte, error) {
	v, err := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(s), "0x"), 16, 8)
	if err != nil {
		return 0, fmt.Errorf("invalid hex byte %q", s)
	}
	return byte(v), nil
}

func ParseU16(s string) (uint16, error) {
	n, err := strconv.ParseUint(s, 0, 16)
	return uint16(n), err
}

func parseU32(s string) (uint32, error) {
	n, err := strconv.ParseUint(s, 0, 32)
	return uint32(n), err
}

func validateExactArgs(action string, args []string, n int) error {
	if len(args) != n {
		return fmt.Errorf("%s requires %d arguments (got %d)", action, n, len(args))
	}
	return nil
}

func CommandForArgs(action string, args []string, kelvinMin, kelvinMax int, invertTemp bool) ([]DPCommand, error) {
	switch strings.ToLower(action) {
	case "on", "off":
		if err := validateExactArgs(action, args, 0); err != nil {
			return nil, err
		}
		return []DPCommand{BoolDP(action == "on")}, nil
	case "brightness", "bright":
		if err := validateExactArgs(action, args, 1); err != nil {
			return nil, err
		}
		p, err := ParseInt(args[0], 1, 100)
		if err != nil {
			return nil, err
		}
		value := uint32(p * 10)
		return []DPCommand{ValueDP(fmt.Sprintf("brightness %d%%", p), 3, value)}, nil
	case "brightness-raw", "bright-raw":
		if err := validateExactArgs(action, args, 1); err != nil {
			return nil, err
		}
		n, err := ParseInt(args[0], 10, 1000)
		if err != nil {
			return nil, err
		}
		return []DPCommand{ValueDP(fmt.Sprintf("brightness-raw %d", n), 3, uint32(n))}, nil
	case "white":
		if err := validateExactArgs(action, args, 2); err != nil {
			return nil, err
		}
		if kelvinMax <= kelvinMin {
			return nil, errors.New("invalid kelvin limits")
		}
		temp, err := ParseInt(args[0], kelvinMin, kelvinMax)
		if err != nil {
			return nil, err
		}
		pct, err := ParseInt(args[1], 1, 100)
		if err != nil {
			return nil, err
		}
		v := int(math.Round(float64(temp-kelvinMin) / float64(kelvinMax-kelvinMin) * 1000))
		if invertTemp {
			v = 1000 - v
		}
		return []DPCommand{ModeDP(0), ValueDP(fmt.Sprintf("temperature %dK => DP4=%d", temp, v), 4, uint32(v)), ValueDP(fmt.Sprintf("brightness %d%%", pct), 3, uint32(pct*10))}, nil
	case "temp":
		if err := validateExactArgs(action, args, 1); err != nil {
			return nil, err
		}
		if kelvinMax <= kelvinMin {
			return nil, errors.New("kelvin-max must be greater than kelvin-min")
		}
		n, err := ParseInt(args[0], kelvinMin, kelvinMax)
		if err != nil {
			return nil, err
		}
		value := int(math.Round(float64(n-kelvinMin) / float64(kelvinMax-kelvinMin) * 1000))
		if invertTemp {
			value = 1000 - value
		}
		return []DPCommand{ModeDP(0), ValueDP(fmt.Sprintf("temperature %dK => DP4=%d", n, value), 4, uint32(value))}, nil
	case "temp-raw":
		if err := validateExactArgs(action, args, 1); err != nil {
			return nil, err
		}
		n, err := ParseInt(args[0], 0, 1000)
		if err != nil {
			return nil, err
		}
		return []DPCommand{ModeDP(0), ValueDP(fmt.Sprintf("temp-raw %d", n), 4, uint32(n))}, nil
	case "rgb":
		if err := validateExactArgs(action, args, 3); err != nil {
			return nil, err
		}
		nums := [3]int{}
		for i := range nums {
			n, err := ParseInt(args[i], 0, 255)
			if err != nil {
				return nil, err
			}
			nums[i] = n
		}
		h, s, v := RgbToHSV(uint8(nums[0]), uint8(nums[1]), uint8(nums[2]))
		c := HsvDP(h, s, v)
		c.Label = fmt.Sprintf("rgb %d %d %d => %s", nums[0], nums[1], nums[2], c.Label)
		return []DPCommand{ModeDP(1), c}, nil
	case "hsv":
		if err := validateExactArgs(action, args, 3); err != nil {
			return nil, err
		}
		h, err := ParseInt(args[0], 0, 359)
		if err != nil {
			return nil, err
		}
		s, err := ParseInt(args[1], 0, 100)
		if err != nil {
			return nil, err
		}
		v, err := ParseInt(args[2], 0, 100)
		if err != nil {
			return nil, err
		}
		return []DPCommand{ModeDP(1), HsvDP(h, s, v)}, nil
	case "mode":
		if err := validateExactArgs(action, args, 1); err != nil {
			return nil, err
		}
		modes := map[string]byte{"white": 0, "colour": 1, "color": 1, "scene": 2, "music": 3}
		mode, ok := modes[strings.ToLower(args[0])]
		if !ok {
			return nil, errors.New("mode must be white|colour|scene|music (music is experimental)")
		}
		return []DPCommand{ModeDP(mode)}, nil
	case "scene":
		if err := validateExactArgs(action, args, 0); err != nil {
			return nil, err
		}
		return []DPCommand{ModeDP(2)}, nil
	case "raw":
		if err := validateExactArgs(action, args, 1); err != nil {
			return nil, err
		}
		b, err := hex.DecodeString(strings.TrimPrefix(strings.ToLower(args[0]), "0x"))
		if err != nil {
			return nil, fmt.Errorf("raw payload: %w", err)
		}
		if len(b) < 2 || len(b) > 16 {
			return nil, errors.New("raw DP-TLV must be 2..16 bytes")
		}
		// A raw CLI TLV must be complete; zero padding is added only AFTER validation.
		for offset := 0; offset < len(b); {
			if offset+2 > len(b) {
				return nil, errors.New("truncated raw TLV")
			}
			if b[offset] == 0 {
				return nil, errors.New("DP ID zero is reserved as padding")
			}
			length := int(b[offset+1] & 0x0f)
			if offset+2+length > len(b) {
				return nil, errors.New("raw TLV length mismatch")
			}
			offset += 2 + length
		}
		var c DPCommand
		c.Label = "raw " + hex.EncodeToString(b)
		copy(c.Plain[:], b)
		return []DPCommand{c}, nil
	case "dp":
		if len(args) < 3 {
			return nil, errors.New("usage: dp bool|enum|value|raw <id> <value>")
		}
		if err := validateExactArgs(action, args, 3); err != nil {
			return nil, err
		}
		id64, err := strconv.ParseUint(args[1], 0, 8)
		if err != nil {
			return nil, fmt.Errorf("invalid DP id: %w", err)
		}
		id := byte(id64)
		switch args[0] {
		case "bool":
			val := strings.ToLower(args[2])
			var b byte
			if val == "1" || val == "on" || val == "true" {
				b = 1
			} else if val == "0" || val == "off" || val == "false" {
				b = 0
			} else {
				return nil, errors.New("dp bool accepts on|off")
			}
			return []DPCommand{MustDP(fmt.Sprintf("dp bool %d=%d", id, b), id, 1, []byte{b})}, nil
		case "enum":
			v, err := ParseInt(args[2], 0, 255)
			if err != nil {
				return nil, err
			}
			return []DPCommand{MustDP(fmt.Sprintf("dp enum %d=%d", id, v), id, 4, []byte{byte(v)})}, nil
		case "value":
			v, err := parseU32(args[2])
			if err != nil {
				return nil, err
			}
			return []DPCommand{ValueDP(fmt.Sprintf("dp value %d=%d", id, v), id, v)}, nil
		case "raw":
			b, err := hex.DecodeString(strings.TrimPrefix(strings.ToLower(args[2]), "0x"))
			if err != nil {
				return nil, err
			}
			c, err := makeDP(fmt.Sprintf("dp raw %d=%x", id, b), id, 0, b)
			if err != nil {
				return nil, err
			}
			return []DPCommand{c}, nil
		default:
			return nil, errors.New("dp type must be bool|enum|value|raw")
		}
	default:
		return nil, fmt.Errorf("unknown command %q", action)
	}
}

// ParsedDP is one P2 TLV inside a 16-byte encrypted plaintext.
type ParsedDP struct {
	ID    byte
	Type  byte
	Value []byte
}

// parseDPBlock accepts concatenated TLVs and zero padding. A zero byte at a TLV
// boundary marks padding; DP id 0 is not supported in this family.
func ParseDPBlock(block []byte) ([]ParsedDP, error) {
	if len(block) != 16 {
		return nil, fmt.Errorf("P2 plaintext must be 16 bytes, got %d", len(block))
	}
	var dps []ParsedDP
	for pos := 0; pos < len(block); {
		if block[pos] == 0 {
			for _, b := range block[pos:] {
				if b != 0 {
					return nil, errors.New("nonzero trailing DP padding")
				}
			}
			break
		}
		if pos+2 > len(block) {
			return nil, errors.New("truncated DP header")
		}
		ln := int(block[pos+1] & 0x0f)
		if pos+2+ln > len(block) {
			return nil, errors.New("DP value crosses 16-byte boundary")
		}
		dps = append(dps, ParsedDP{ID: block[pos], Type: block[pos+1] >> 4, Value: append([]byte(nil), block[pos+2:pos+2+ln]...)})
		pos += 2 + ln
	}
	if len(dps) == 0 {
		return nil, errors.New("no P2 DP entries")
	}
	return dps, nil
}

// packCommands combines adjacent valid TLVs into a single 16-byte block and
// splits at a TLV boundary if the next element would overflow. This is an
// optional P2 optimization; no DP is truncated or changed.
func PackCommands(in []DPCommand) ([]DPCommand, error) {
	var out []DPCommand
	var cur DPCommand
	pos := 0
	flush := func() {
		if pos > 0 {
			out = append(out, cur)
			cur = DPCommand{}
			pos = 0
		}
	}
	for _, cmd := range in {
		dps, err := ParseDPBlock(cmd.Plain[:])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", cmd.Label, err)
		}
		for _, dp := range dps {
			n := 2 + len(dp.Value)
			if n > 16 {
				return nil, errors.New("DP TLV exceeds block")
			}
			if pos+n > 16 {
				flush()
			}
			cur.Plain[pos] = dp.ID
			cur.Plain[pos+1] = (dp.Type << 4) | byte(len(dp.Value))
			copy(cur.Plain[pos+2:pos+n], dp.Value)
			pos += n
			if cur.Label == "" {
				cur.Label = cmd.Label
			} else {
				cur.Label += " + " + cmd.Label
			}
		}
	}
	flush()
	return out, nil
}

func Crc16hi(data []byte) byte {
	var s uint16
	for _, b := range data {
		s ^= uint16(b) << 8
		for j := 0; j < 8; j++ {
			if s&0x8000 != 0 {
				s ^= 0x8380
			}
			s <<= 1
		}
	}
	return byte(s >> 8)
}

func mx(sum, y, z uint32, k [4]uint32, p int, e uint32) uint32 {
	return (((z >> 5) ^ (y << 2)) + ((y >> 3) ^ (z << 4))) ^ ((sum ^ y) + (k[(p&3)^int(e)] ^ z))
}

func EncryptXXTEABE(plain, key []byte) [16]byte {
	var v, k [4]uint32
	for i := 0; i < 4; i++ {
		v[i] = binary.BigEndian.Uint32(plain[4*i : 4*i+4])
		k[i] = binary.BigEndian.Uint32(key[4*i : 4*i+4])
	}
	var sum uint32
	z := v[3]
	for round := 0; round < 19; round++ {
		sum += delta
		e := (sum >> 2) & 3
		for p := 0; p < 3; p++ {
			y := v[p+1]
			v[p] += mx(sum, y, z, k, p, e)
			z = v[p]
		}
		y := v[0]
		v[3] += mx(sum, y, z, k, 3, e)
		z = v[3]
	}
	var out [16]byte
	for i := 0; i < 4; i++ {
		binary.BigEndian.PutUint32(out[4*i:4*i+4], v[i])
	}
	return out
}

func BuildFrame(src, dst, seq uint16, cmd DPCommand, k1, k2 []byte, flagByte byte) (WireFrame, error) {
	var out WireFrame
	if len(k1) != 16 || len(k2) != 16 {
		return out, errors.New("both keys must be 16 bytes")
	}
	if flagByte != 1 && flagByte != 2 && flagByte != 6 {
		return out, errors.New("--flags must be 01, 02 or 06")
	}
	f := &out.Frame
	f[0] = 0x0b
	binary.BigEndian.PutUint16(f[1:3], src)
	binary.BigEndian.PutUint16(f[3:5], dst)
	binary.BigEndian.PutUint16(f[5:7], seq)
	f[7] = 0x05
	f[8] = Crc16hi(cmd.Plain[:])
	ciphertext := EncryptXXTEABE(cmd.Plain[:], k1)
	copy(f[9:25], ciphertext[:])
	crcBuffer := make([]byte, 0, 25)
	crcBuffer = append(crcBuffer, f[0]&0xfc)
	crcBuffer = append(crcBuffer, f[1:25]...)
	f[25] = Crc16hi(crcBuffer)
	for i := 0; i < 16; i++ {
		f[9+i] ^= k2[i]
	}
	copy(out.Adv[:5], []byte{0x02, 0x01, flagByte, 0x1b, 0x03})
	copy(out.Adv[5:], f[:])
	out.Seq, out.Label = seq, cmd.Label
	return out, nil
}
