//go:build !linux

package bluez

import (
	"context"
	"errors"
	"time"

	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
)

func sendAllMGMT(context.Context, string, []p2.WireFrame, time.Duration, time.Duration, time.Duration) error {
	return errors.New("native BlueZ MGMT requires Linux")
}

func runMGMTSendHelper([]string) error { return errors.New("native BlueZ MGMT requires Linux") }
