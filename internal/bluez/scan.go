package bluez

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const MonitorReady = "P2M1"

const MaxMonitorPacket = 65535

type bleAdvertisement struct {
	Data []byte
	RSSI int8
}

// Payload of an HCI event packet (without H4 packet-type prefix).
// Supports the legacy advertising report (LE subevent 0x02) and extended
// advertising report (0x0D). Invalid or truncated events are dropped.
func AdvertisementsFromHCIEvent(packet []byte) []bleAdvertisement {
	if len(packet) < 4 || packet[0] != 0x3e || int(packet[1]) != len(packet)-2 {
		return nil
	}
	subevent, nReports := packet[2], int(packet[3])
	i := 4
	var out []bleAdvertisement
	for r := 0; r < nReports; r++ {
		switch subevent {
		case 0x02: // LE Advertising Report
			if len(packet)-i < 9 {
				return out
			}
			// [event_type:1][addr_type:1][addr:6][data_len:1]
			n := int(packet[i+8])
			i += 9
			if n > len(packet)-i-1 {
				return out
			}
			out = append(out, bleAdvertisement{Data: append([]byte(nil), packet[i:i+n]...), RSSI: int8(packet[i+n])})
			i += n + 1
		case 0x0d: // LE Extended Advertising Report
			// [event_type:2][addr_type:1][addr:6][primary_phy:1]
			// [secondary_phy:1][sid:1][tx_power:1][rssi:1]
			// [interval:2][direct_addr_type:1][direct_addr:6][data_len:1]
			if len(packet)-i < 24 {
				return out
			}
			n := int(packet[i+23])
			rssi := int8(packet[i+13])
			i += 24
			if n > len(packet)-i {
				return out
			}
			out = append(out, bleAdvertisement{Data: append([]byte(nil), packet[i:i+n]...), RSSI: rssi})
			i += n
		default:
			return nil
		}
	}
	return out
}

func ParseAdapterIndex(adapter string) (int, error) {
	if !strings.HasPrefix(adapter, "hci") {
		return 0, fmt.Errorf("adapter %q: expected hciN", adapter)
	}
	n, err := strconv.Atoi(strings.TrimPrefix(adapter, "hci"))
	if err != nil || n < 0 || n > 65534 {
		return 0, fmt.Errorf("adapter %q: expected hci0..hci65534", adapter)
	}
	return n, nil
}

func StartBLEScan(ctx context.Context, adapter string) (func(), error) {
	if _, err := exec.LookPath("btmgmt"); err != nil {
		return nil, fmt.Errorf("btmgmt missing (install bluez-utils): %w", err)
	}
	cmd := exec.CommandContext(ctx, "sudo", "-n", "btmgmt", "-i", adapter, "find", "-l")
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	ended := make(chan error, 1)
	go func() { ended <- cmd.Wait() }()
	select {
	case err := <-ended:
		// Several btmgmt releases immediately exit after issuing Start
		// Discovery. That's fine if it succeeded; explicitly stop later.
		if err != nil {
			return nil, fmt.Errorf("btmgmt find -l: %w", err)
		}
	case <-time.After(300 * time.Millisecond):
	}
	return func() {
		// Stop only the scan requested by our monitor, not advertising.
		cancel, stop := context.WithTimeout(context.Background(), 4*time.Second)
		defer stop()
		stopCmd := exec.CommandContext(cancel, "sudo", "-n", "btmgmt", "-i", adapter, "stop-find", "-l")
		stopCmd.Stdin, stopCmd.Stdout, stopCmd.Stderr = nil, io.Discard, io.Discard
		_ = stopCmd.Run()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}, nil
}
