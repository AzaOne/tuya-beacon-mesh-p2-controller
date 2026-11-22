// Package xdg locates the controller's per-user files on Linux.
package xdg

import (
	"os"
	"os/user"
	"path/filepath"
)

const app = "tuya-beacon-mesh-p2-controller"

func directory(variable, fallback string) string {
	if base := os.Getenv(variable); filepath.IsAbs(base) {
		return filepath.Join(base, app)
	}
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		if account, err := user.Current(); err == nil {
			home = account.HomeDir
		}
	}
	return filepath.Join(home, fallback, app)
}

func ConfigDir() string { return directory("XDG_CONFIG_HOME", ".config") }
func StateDir() string  { return directory("XDG_STATE_HOME", ".local/state") }

// Local storage is selected as a pair, so a stray config or empty directory
// alone cannot redirect one half of an installed controller's data.
func LocalStorage() bool {
	cfg, err := os.Stat("config.json")
	if err != nil || !cfg.Mode().IsRegular() {
		return false
	}
	state, err := os.Stat("state")
	return err == nil && state.IsDir()
}
