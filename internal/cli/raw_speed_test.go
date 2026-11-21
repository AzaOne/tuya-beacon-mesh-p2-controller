package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"example.com/tuya-beacon-mesh-p2-controller/internal/config"
)

func TestRawHCIConfigAndCLI(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"controller":{"transport":"raw-hci","adv_interval":"20ms","frames":1}}`), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := config.Read(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if c.Controller.Transport != "raw-hci" || c.Controller.AdvInterval != "20ms" {
		t.Fatalf("bad config %+v", c.Controller)
	}
	// Don't connect to radio; verify the raw HCI backend is accepted and
	// the selected command can be encoded in dry-run mode.
	if err := Run(context.Background(), []string{"-config", cfgPath, "scene", "list"}); err != nil {
		t.Fatal(err)
	}
}
