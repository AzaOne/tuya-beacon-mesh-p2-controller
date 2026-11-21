package p2

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestP2KnownFixture(t *testing.T) {
	// Fixture from tuya-lamp-re TestPythonVector: synthetic keys, not real secrets.
	k1 := []byte("0123456789abcdef")
	k2 := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	dp := MustDP("test", 4, 1, []byte{1})
	got, err := BuildFrame(0x7df9, 0x0005, 0x1234, dp, k1, k2, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := "0b7df9000512340539a5829f259fde6dfde2aeebcdcffe2d7b45"
	if hex.EncodeToString(got.Frame[:]) != want {
		t.Fatalf("got %x want %s", got.Frame, want)
	}
	if !bytes.Equal(got.Adv[:5], []byte{2, 1, 1, 0x1b, 3}) {
		t.Fatalf("bad ADV %x", got.Adv)
	}
}

func TestCapturedPlainDPLayouts(t *testing.T) {
	examples := []struct {
		name string
		c    DPCommand
		want string
	}{
		{"on", BoolDP(true), "011101"},
		{"off", BoolDP(false), "011100"},
		{"white mode", ModeDP(0), "024100"},
		{"colour mode", ModeDP(1), "024101"},
		{"scene mode", ModeDP(2), "024102"},
		{"brightness", ValueDP("", 3, 1000), "0324000003e8"},
		{"temperature", ValueDP("", 4, 1000), "0424000003e8"},
		{"green", HsvDP(120, 100, 100), "0b0400786464"},
		{"blue", HsvDP(240, 100, 100), "0b0400f06464"},
	}
	for _, tt := range examples {
		t.Run(tt.name, func(t *testing.T) {
			p, _ := hex.DecodeString(tt.want)
			if !bytes.Equal(tt.c.Plain[:len(p)], p) {
				t.Errorf("got %x want prefix %x", tt.c.Plain, p)
			}
			if !bytes.Equal(tt.c.Plain[len(p):], make([]byte, 16-len(p))) {
				t.Errorf("not zero padded: %x", tt.c.Plain)
			}
		})
	}
}

func TestRGB(t *testing.T) {
	ex := []struct {
		r, g, b uint8
		h, s, v int
	}{
		{255, 0, 0, 0, 100, 100}, {0, 255, 0, 120, 100, 100}, {0, 0, 255, 240, 100, 100},
		{100, 232, 111, 125, 57, 91}, {255, 255, 255, 0, 0, 100}, {0, 0, 0, 0, 0, 0},
	}
	for _, tt := range ex {
		h, s, v := RgbToHSV(tt.r, tt.g, tt.b)
		if h != tt.h || s != tt.s || v != tt.v {
			t.Errorf("rgb %d,%d,%d => %d,%d,%d", tt.r, tt.g, tt.b, h, s, v)
		}
	}
}

func TestCommands(t *testing.T) {
	cs, err := CommandForArgs("temp", []string{"4000"}, 2700, 6500, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 || cs[0].Plain[0] != 2 || cs[1].Plain[0] != 4 || !bytes.Equal(cs[1].Plain[2:6], []byte{0, 0, 1, 86}) {
		t.Fatalf("temp: %+v", cs)
	}
	cs, err = CommandForArgs("rgb", []string{"0", "255", "0"}, 2700, 6500, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 || !bytes.Equal(cs[1].Plain[:6], []byte{0x0b, 0x04, 0x00, 0x78, 0x64, 0x64}) {
		t.Fatalf("rgb %x", cs[1].Plain)
	}
	cs, err = CommandForArgs("white", []string{"5000", "80"}, 2700, 6500, false)
	if err != nil || len(cs) != 3 || cs[2].Plain[0] != 3 {
		t.Fatalf("white %v %+v", err, cs)
	}
	for _, tc := range []struct {
		act  string
		args []string
	}{
		{"on", []string{"unexpected"}}, {"rgb", []string{"999", "2", "3"}},
		{"temp", []string{"14000"}}, {"raw", []string{"0111"}},
		{"dp", []string{"raw", "11", "00112233445566778899aabbccddeeff"}},
	} {
		if _, err := CommandForArgs(tc.act, tc.args, 2700, 6500, false); err == nil {
			t.Errorf("should reject %s %v", tc.act, tc.args)
		}
	}
}
