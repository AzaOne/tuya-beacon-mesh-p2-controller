//go:build linux

package bluez

import (
	"bytes"
	"testing"
	"time"
)

func TestExtendedAdvParamsSelectRequestedInterval(t *testing.T) {
	// MGMT 0x0054: instance, flags, duration, timeout, min/max intervals, TX power.
	want := []byte{2, 0x01, 0x40, 0, 0, 0, 0, 0, 0, 0x20, 0, 0, 0, 0x20, 0, 0, 0, 0}
	if got := extendedAdvParams(2, 20*time.Millisecond); !bytes.Equal(got, want) {
		t.Fatalf("MGMT parameters = %x, want %x", got, want)
	}
}
