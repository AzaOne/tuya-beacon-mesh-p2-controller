package xdg

import (
	"path/filepath"
	"testing"
)

func TestBaseDirectories(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, tc := range []struct {
		env, fallback string
		dir           func() string
	}{
		{"XDG_CONFIG_HOME", ".config", ConfigDir},
		{"XDG_STATE_HOME", ".local/state", StateDir},
	} {
		for _, base := range []string{"", "relative", filepath.Join(home, "custom")} {
			t.Setenv(tc.env, base)
			want := filepath.Join(home, tc.fallback, app)
			if filepath.IsAbs(base) {
				want = filepath.Join(base, app)
			}
			if got := tc.dir(); got != want {
				t.Fatalf("%s=%q: got %q, want %q", tc.env, base, got, want)
			}
		}
	}
}
