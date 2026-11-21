package bluez

import (
	"context"
	"fmt"
	"time"

	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
)

type Transport struct {
	Adapter, Backend           string
	Duration, Gap, AdvInterval time.Duration
	ClearAll                   bool
}

func (t Transport) Send(ctx context.Context, frames []p2.WireFrame) error {
	switch t.Backend {
	case "mgmt":
		return sendAllMGMT(ctx, t.Adapter, frames, t.Duration, t.Gap, t.AdvInterval)
	case "mgmt-legacy":
		return sendAllMGMT(ctx, t.Adapter, frames, t.Duration, t.Gap, 0)
	case "raw-hci":
		return sendAllRawHCI(ctx, t.Adapter, frames, t.Duration, t.Gap, t.AdvInterval)
	case "btmgmt":
		tx := transmitter{Adapter: t.Adapter, Duration: t.Duration, Gap: t.Gap, Instance: 1, ClearAll: t.ClearAll}
		return tx.sendAll(ctx, frames)
	default:
		return fmt.Errorf("unknown BlueZ transport %q", t.Backend)
	}
}
