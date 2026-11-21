//go:build !linux

package bluez

import (
	"context"
	"errors"
	"time"

	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
)

func runRawHCISendHelper([]string) error { return errors.New("Raw HCI requires Linux") }

func sendAllRawHCI(context.Context, string, []p2.WireFrame, time.Duration, time.Duration, time.Duration) error {
	return errors.New("Raw HCI requires Linux")
}
