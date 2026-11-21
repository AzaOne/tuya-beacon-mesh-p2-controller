package provision

import (
	"context"
	"encoding/binary"
	"encoding/json"
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
)

// recoverKeyAuto tries separate (source,destination) groups. This allows a
// freshly paired phone/lamp pair to have different addresses than old captures.
// The candidate is accepted only when recoverKey verifies independent DPs and
// unique sequence numbers with BOTH cryptographic/CRC checks.
func recoverKeyAuto(frames [][]byte, k1 []byte, preferredSrc, preferredDst uint16, autoAddress bool) (learnedKey, error) {
	type pair struct{ src, dst uint16 }
	groups := map[pair][][]byte{}
	for _, f := range frames {
		if len(f) != 26 || f[0] != 0x0b || f[7] != 0x05 {
			continue
		}
		p := pair{binary.BigEndian.Uint16(f[1:3]), binary.BigEndian.Uint16(f[3:5])}
		if !autoAddress && (p.src != preferredSrc || p.dst != preferredDst) {
			continue
		}
		groups[p] = append(groups[p], f)
	}
	var best learnedKey
	var found, ambiguous bool
	for p, candidates := range groups {
		result, err := recoverKey(candidates, k1, p.src, p.dst)
		if err != nil {
			continue
		}
		if !found || result.VerifiedFrames > best.VerifiedFrames ||
			(result.VerifiedFrames == best.VerifiedFrames && result.DistinctDPs > best.DistinctDPs) {
			found, best, ambiguous = true, result, false
		} else if result.VerifiedFrames == best.VerifiedFrames && result.DistinctDPs == best.DistinctDPs &&
			(result.BeaconKey != best.BeaconKey || result.Source != best.Source || result.Destination != best.Destination) {
			ambiguous = true
		}
	}
	if !found {
		return learnedKey{}, errors.New("no cryptographically verified ON/OFF pair yet")
	}
	if ambiguous {
		return learnedKey{}, errors.New("multiple matching devices with equal confidence; set -learn-auto=false -phone-src/-dst")
	}
	return best, nil
}

// learnLive receives *on-air* BLE advertisements with the existing privileged
// HCI monitor helper, WITHOUT requiring beacon_key. It cannot see the phone's
// private HCI stream; only frames received over radio by the PC's adapter.
func LearnLive(ctx context.Context, configPath, output, adapter string,
	src, dst uint16, autoAddress, autoScan bool, timeout time.Duration) error {

	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var cfg config.Config
	if err = json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	k1, err := config.ParseKey(cfg.Device.LocalKey)
	if err != nil {
		return fmt.Errorf("local_key: %w", err)
	}
	index, err := bluez.ParseAdapterIndex(adapter)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if _, err := exec.LookPath("sudo"); err != nil {
		return fmt.Errorf("sudo required for HCI monitor: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sudo", "-n", exe, "__hci-monitor", strconv.Itoa(index))
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	var hello [4]byte
	if _, err := io.ReadFull(stdout, hello[:]); err != nil {
		return fmt.Errorf("start HCI helper (run sudo -v): %w", err)
	}
	if string(hello[:]) != bluez.MonitorReady {
		return errors.New("unexpected HCI helper greeting")
	}
	if autoScan {
		stopScan, err := bluez.StartBLEScan(ctx, adapter)
		if err != nil {
			return err
		}
		defer stopScan()
	}
	fmt.Printf("Listening for Smart Life BLE advertisements on %s (timeout %s). Toggle ON and OFF on your phone.\n", adapter, timeout)
	fmt.Println("The phone must be within radio range of the PC; root on the PC cannot access phone-side HCI packets.")

	type result struct {
		packet []byte
		err    error
	}
	input := make(chan result, 8)
	go func() {
		for {
			var hdr [4]byte
			if _, err := io.ReadFull(stdout, hdr[:]); err != nil {
				select {
				case input <- result{err: err}:
				case <-ctx.Done():
				}
				return
			}
			n := binary.BigEndian.Uint32(hdr[:])
			if n < 6 || n > bluez.MaxMonitorPacket {
				select {
				case input <- result{err: fmt.Errorf("invalid HCI monitor length %d", n)}:
				case <-ctx.Done():
				}
				return
			}
			packet := make([]byte, n)
			if _, err := io.ReadFull(stdout, packet); err != nil {
				select {
				case input <- result{err: err}:
				case <-ctx.Done():
				}
				return
			}
			select {
			case input <- result{packet: packet}:
			case <-ctx.Done():
				return
			}
		}
	}()

	frames := make([][]byte, 0, 128)
	seen := make(map[string]bool)
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("live learning ended without an independently verified ON/OFF key: %w (frames=%d)", ctx.Err(), len(frames))
		case item := <-input:
			if item.err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return fmt.Errorf("HCI reader exited: %w", item.err)
			}
			p := item.packet
			if len(p) < 6 || binary.LittleEndian.Uint16(p[0:2]) != 3 || int(binary.LittleEndian.Uint16(p[2:4])) != index || int(binary.LittleEndian.Uint16(p[4:6])) != len(p)-6 {
				continue
			}
			for _, ad := range bluez.AdvertisementsFromHCIEvent(p[6:]) {
				for _, f := range p2.P2FramesFromAdvertisement(ad.Data) {
					if len(f) != 26 || f[0] != 0x0b || f[7] != 0x05 {
						continue
					}
					s := string(f)
					if seen[s] {
						continue
					}
					seen[s] = true
					if len(frames) < 4096 {
						frames = append(frames, f)
					}
					if len(frames)%4 == 0 {
						fmt.Printf("Observed %d distinct P2 frames; checking key candidates...\n", len(frames))
					}
					learned, err := recoverKeyAuto(frames, k1, src, dst, autoAddress)
					if err != nil {
						continue
					}
					if output != "" {
						if err := saveLearnResult(output, learned); err != nil {
							return err
						}
						fmt.Printf("Verified %d P2 frames and %d distinct DP payloads; recovered beacon_key saved (0600) to %s\n", learned.VerifiedFrames, learned.DistinctDPs, output)
					} else {
						fmt.Printf("Verified %d P2 frames and %d distinct DP payloads. Pass -learn-out <file> to save recovered beacon_key.\n", learned.VerifiedFrames, learned.DistinctDPs)
					}
					fmt.Printf("Detected src=%s dst=%s; update -phone-src/-dst if they changed.\n", learned.Source, learned.Destination)
					return nil
				}
			}
		}
	}
}
