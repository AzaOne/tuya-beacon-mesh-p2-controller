// Package version reports the Git revision embedded by build.sh.
package version

import "runtime/debug"

var description string

func String() string {
	if description != "" {
		return description
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				return info.Main.Version + " (" + setting.Value + ")"
			}
		}
	}
	return "dev"
}
