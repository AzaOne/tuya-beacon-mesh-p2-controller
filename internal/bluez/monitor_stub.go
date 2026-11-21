//go:build !linux

package bluez

import (
	"errors"
)

func runHCIMonitorHelper(_ string) error { return errors.New("HCI monitor requires Linux/BlueZ") }
