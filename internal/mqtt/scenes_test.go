package mqtt

import (
	"bytes"
	"strings"
	"testing"

	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
)

func TestSceneMonitorDescriptionsAndMQTT(t *testing.T) {
	d := p2.DescribeDP(73, 0, []byte{0x08, 0, 2, 0, 25, 50, 0x40, 0x80})
	for _, want := range []string{"scene 8", "Warm light breath", "speed=25", "brightness=50"} {
		if !strings.Contains(d, want) {
			t.Fatalf("monitor description missing %s: %s", want, d)
		}
	}
	for _, payload := range []string{"scene 8", "scene red-breathing", "scene 13 --speed 50 --brightness 80"} {
		if _, err := allowedCommand(payload); err != nil {
			t.Errorf("mqtt rejected %q: %v", payload, err)
		}
	}
	for _, payload := range []string{"scene list", "scene current", "scene 45", "scene 8 --speed 300"} {
		if _, err := allowedCommand(payload); err == nil {
			t.Errorf("mqtt accepted invalid %q", payload)
		}
	}
	var list bytes.Buffer
	p2.PrintSceneList(&list)
	if strings.Count(list.String(), "captured") != 29 {
		t.Fatal("scene list should show 28 captured presets")
	}
}
