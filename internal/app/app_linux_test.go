package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"example.com/tuya-beacon-mesh-p2-controller/internal/config"
)

func TestBridgeTransmissionUsesDeviceConfigAndSharedState(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "lamp.json")
	statePath := filepath.Join(dir, "state.json")
	writeConfig := func(key string, dry bool) {
		t.Helper()
		c := config.Defaults()
		c.Device.DeviceID = "lamp"
		c.Device.LocalKey = key
		c.Device.BeaconKey = "pqrstuvwxyz12345"
		c.Controller.DryRun = dry
		data, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cfg, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeConfig("abcdefghijklmnop", false)
	// Capture exactly the bytes and options destined for the privileged helper.
	t.Setenv("PATH", dir)
	t.Setenv("P2_TEST_FRAMES", filepath.Join(dir, "frames"))
	script := "#!/bin/sh\n[ \"$3\" = __mgmt-send ] && [ \"$5\" = 250 ] && [ \"$7\" = 1 ] || exit 1\n/bin/cat > \"$P2_TEST_FRAMES\"\n"
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	radio := RadioDefaults(config.Defaults().Controller)
	opts, err := ValidateRadio(radio)
	if err != nil {
		t.Fatal(err)
	}
	device, err := OpenDevice(cfg, statePath, opts, radio.Transport())
	if err != nil {
		t.Fatal(err)
	}
	device.Transport.Duration = 250 * time.Millisecond
	execOn := func() error { return device.executeBridgeCommand(context.Background(), []string{"on"}) }
	if err := execOn(); err != nil {
		t.Fatal(err)
	}
	frames, err := os.ReadFile(os.Getenv("P2_TEST_FRAMES"))
	if err != nil || len(frames) != 31 {
		t.Fatalf("advertisement: %x, %v", frames, err)
	}
	st, err := device.Lamp.Snapshot()
	if err != nil || st.LastReserved != 0x8f || st.Light == nil || st.Light.Power == nil || !*st.Light.Power {
		t.Fatalf("sent state %+v, %v", st, err)
	}
	writeConfig("abcdefghijklmnop", true)
	if err := execOn(); err != nil {
		t.Fatal(err)
	}
	st, err = device.Lamp.Snapshot()
	if err != nil || st.LastReserved != 0x8f {
		t.Fatalf("dry run reserved a seq: %+v, %v", st, err)
	}
	writeConfig("differentkey1234", false)
	if err := execOn(); err == nil {
		t.Fatal("accepted state from a different pairing")
	}
}
