package monitor

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"time"

	"example.com/tuya-beacon-mesh-p2-controller/internal/bluez"
	"example.com/tuya-beacon-mesh-p2-controller/internal/config"
	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
	"example.com/tuya-beacon-mesh-p2-controller/internal/state"
)

// Packet: Linux HCI Monitor 6-byte header [opcode LE16][index LE16][len LE16]
// followed by an HCI event packet. Monitor event opcode 3 is HCI_MON_EVENT_PKT.
func observedFromMonitorPacket(packet []byte, adapterIndex int, src, dst uint16, k1, k2 []byte) []p2.ObservedCommand {
	if len(packet) < 6 || binary.LittleEndian.Uint16(packet[0:2]) != 3 ||
		int(binary.LittleEndian.Uint16(packet[2:4])) != adapterIndex ||
		int(binary.LittleEndian.Uint16(packet[4:6])) != len(packet)-6 {
		return nil
	}
	advs := bluez.AdvertisementsFromHCIEvent(packet[6:])
	var out []p2.ObservedCommand
	for _, adv := range advs {
		for _, frame := range p2.P2FramesFromAdvertisement(adv.Data) {
			if obs, ok := p2.DecodeObservedCommand(frame, k1, k2, src, dst, adv.RSSI); ok {
				out = append(out, obs)
			}
		}
	}
	return out
}

// The receiver process is privileged only for its HCI monitor socket. The
// controller, decoder, file locking and state persistence all run as the user.
func Run(ctx context.Context, cfg config.Config, statePath, adapter string, src, dst uint16, k1, k2 []byte, fp string, autoScan bool) error {
	index, err := bluez.ParseAdapterIndex(adapter)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if _, err := exec.LookPath("sudo"); err != nil {
		return fmt.Errorf("sudo is required for the small HCI reader: %w", err)
	}

	childCtx, stopChild := context.WithCancel(ctx)
	defer stopChild()
	cmd := exec.CommandContext(childCtx, "sudo", "-n", exe, "__hci-monitor", strconv.Itoa(index))
	cmd.Stdin = nil
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start privileged HCI reader: %w", err)
	}
	defer func() { stopChild(); _ = cmd.Wait() }()

	var ready [4]byte
	if _, err := io.ReadFull(stdout, ready[:]); err != nil {
		return fmt.Errorf("HCI reader failed to start (run sudo -v first): %w", err)
	}
	if string(ready[:]) != bluez.MonitorReady {
		return errors.New("invalid HCI reader greeting")
	}

	var stopScan func()
	if autoScan {
		stopScan, err = bluez.StartBLEScan(ctx, adapter)
		if err != nil {
			return fmt.Errorf("start BLE scan: %w", err)
		}
		defer stopScan()
	}
	fmt.Printf("Monitoring P2 commands to lamp over BLE (adapter=%s src=%04X dst=%04X)\n", adapter, src, dst)
	fmt.Printf("state=%s; scan=%t; Ctrl+C to stop\n", statePath, autoScan)

	// Read the privileged helper on a separate goroutine so SIGINT/SIGTERM
	// always stops this process, even if sudo failed to close the child's
	// stdout pipe promptly after cancellation.
	type received struct {
		packet []byte
		err    error
	}
	incoming := make(chan received, 8)
	go func() {
		for {
			var hdr [4]byte
			_, err := io.ReadFull(stdout, hdr[:])
			if err != nil {
				select {
				case incoming <- received{err: err}:
				case <-ctx.Done():
				}
				return
			}
			n := binary.BigEndian.Uint32(hdr[:])
			if n < 6 || n > bluez.MaxMonitorPacket {
				select {
				case incoming <- received{err: fmt.Errorf("invalid HCI monitor packet length: %d", n)}:
				case <-ctx.Done():
				}
				return
			}
			packet := make([]byte, n)
			if _, err := io.ReadFull(stdout, packet); err != nil {
				select {
				case incoming <- received{err: err}:
				case <-ctx.Done():
				}
				return
			}
			select {
			case incoming <- received{packet: packet}:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Suppress radio retransmissions of the very same P2 frame. Distinct seq
	// values are still processed even when DP plaintext is identical.
	seen := make(map[uint16][16]byte)
	// No long-lived state lock: each verified packet gets a short atomic state
	// update, allowing other 'tuya-beacon-mesh-p2-controller on/off' processes to take their turn.
	for {
		var r received
		select {
		case <-ctx.Done():
			return nil
		case r = <-incoming:
		}
		if r.err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("HCI reader terminated: %w", r.err)
		}
		for _, observed := range observedFromMonitorPacket(r.packet, index, src, dst, k1, k2) {
			if previous, ok := seen[observed.Seq]; ok && previous == observed.Plain {
				continue
			}
			seen[observed.Seq] = observed.Plain
			changed, last, err := state.SyncObservedFull(statePath, cfg.Device.DeviceID, fp, observed, "phone")
			status := "repeat/old"
			if changed {
				status = "synced"
			}
			if err != nil {
				// Do not end monitoring due to old/out-of-order frames; they
				// can arrive later as BLE scanning sees packet duplicates.
				if errors.Is(err, state.ErrObservedOlder) {
					status = "old"
				} else {
					return fmt.Errorf("update seq state: %w", err)
				}
			}
			fmt.Printf("%s P2 -> LAMP seq=0x%04X %-30s DP%d data=%x RSSI=%d last=0x%04X %s\n",
				time.Now().Format("15:04:05"), observed.Seq, observed.Description,
				observed.ID, observed.Value, observed.RSSI, last, status)
		}
	}
}
