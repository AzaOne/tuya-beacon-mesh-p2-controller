package capture

import (
	"bytes"
	"os"
	"testing"

	"example.com/tuya-beacon-mesh-p2-controller/internal/config"
	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
)

// Optional private regression: supply real keys and a real capture locally.
// Neither the private config nor the capture is embedded in the repository.
func TestPrivateCaptureRegression(t *testing.T) {
	path := os.Getenv("P2_PRIVATE_CAPTURE")
	cfgPath := os.Getenv("P2_PRIVATE_CONFIG")
	if path == "" || cfgPath == "" {
		t.Skip("set P2_PRIVATE_CAPTURE and P2_PRIVATE_CONFIG")
	}
	c, k1, k2, err := config.LoadDevice(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = c
	frames, err := CaptureTXFrames(path)
	if err != nil {
		t.Fatal(err)
	}
	valid := 0
	for _, raw := range frames {
		obs, ok := p2.DecodeObservedCommand(raw, k1, k2, 0x7a59, 3, 0)
		if !ok {
			continue
		}
		cmd := p2.DPCommand{Label: "private regression", Plain: obs.Plain}
		built, err := p2.BuildFrame(obs.Source, obs.Target, obs.Seq, cmd, k1, k2, 1)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(built.Frame[:], raw) {
			t.Fatalf("byte mismatch at seq 0x%04x", obs.Seq)
		}
		valid++
	}
	if valid < 50 {
		t.Fatalf("too few verified P2 frames: %d", valid)
	}
	t.Logf("regression passed for %d real P2 frames", valid)
}
