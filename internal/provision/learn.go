package provision

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"example.com/tuya-beacon-mesh-p2-controller/internal/capture"
	"example.com/tuya-beacon-mesh-p2-controller/internal/config"
	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
)

type learnedKey struct {
	BeaconKey      string `json:"beacon_key"`
	Source         string `json:"source"`
	Destination    string `json:"destination"`
	VerifiedFrames int    `json:"verified_frames"`
	DistinctDPs    int    `json:"distinct_datapoints"`
	Note           string `json:"note"`
}

// recoverKey derives candidate XOR masks from known ON/OFF payloads and
// requires a single candidate to validate independent distinct commands.
func recoverKey(frames [][]byte, k1 []byte, src, dst uint16) (learnedKey, error) {
	var out learnedKey
	if len(k1) != 16 {
		return out, errors.New("local_key must be 16 bytes")
	}
	known := []p2.DPCommand{p2.BoolDP(true), p2.BoolDP(false)}
	count := map[[16]byte]int{}
	for _, f := range frames {
		if len(f) != 26 || f[0] != 0x0b || f[7] != 5 || binary.BigEndian.Uint16(f[1:3]) != src || binary.BigEndian.Uint16(f[3:5]) != dst {
			continue
		}
		for _, cmd := range known {
			if p2.Crc16hi(cmd.Plain[:]) != f[8] {
				continue
			}
			encrypted := p2.EncryptXXTEABE(cmd.Plain[:], k1)
			var candidate [16]byte
			for i := range candidate {
				candidate[i] = f[9+i] ^ encrypted[i]
			}
			count[candidate]++
		}
	}
	var winner [16]byte
	max := 0
	for key, n := range count {
		if n > max {
			winner = key
			max = n
		}
	}
	if max == 0 {
		return out, errors.New("no ON/OFF known plaintext candidate in capture")
	}
	distinct := map[string]bool{}
	seqs := map[uint16]bool{}
	verified := 0
	for _, f := range frames {
		obs, ok := p2.DecodeObservedCommand(f, k1, winner[:], src, dst, 0)
		if !ok {
			continue
		}
		verified++
		seqs[obs.Seq] = true
		for _, dp := range obs.DPs {
			distinct[fmt.Sprintf("%d/%d/%x", dp.ID, dp.Type, dp.Value)] = true
		}
	}
	// Two independent plaintexts and two seqs are the minimal evidence.
	// Require higher confidence than a single accidental CRC match.
	if verified < 2 || len(seqs) < 2 || len(distinct) < 2 {
		return out, fmt.Errorf("candidate insufficiently verified: frames=%d unique seq=%d unique dp=%d", verified, len(seqs), len(distinct))
	}
	out = learnedKey{BeaconKey: hex.EncodeToString(winner[:]), Source: fmt.Sprintf("0x%04x", src), Destination: fmt.Sprintf("0x%04x", dst), VerifiedFrames: verified, DistinctDPs: len(distinct), Note: "Known-plaintext inference, not a cryptographic proof of lamp acceptance. Do not publish this secret."}
	return out, nil
}

func saveLearnResult(path string, v learnedKey) error {
	if path == "" {
		return errors.New("empty output file")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	// Reject overwriting existing secrets/configurations without an explicit
	// deletion by the user.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = f.Write(b); err != nil {
		return err
	}
	return f.Sync()
}

func LearnFromCapture(cfgPath, capturePath, output string, src, dst uint16, autoAddress, save bool) error {
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	var c config.Config
	if err = json.Unmarshal(b, &c); err != nil {
		return err
	}
	k1, err := config.ParseKey(c.Device.LocalKey)
	if err != nil {
		return fmt.Errorf("local_key: %w", err)
	}
	frames, err := capture.CaptureTXFrames(capturePath)
	if err != nil {
		return err
	}
	result, err := recoverKeyAuto(frames, k1, src, dst, autoAddress)
	if err != nil {
		return err
	}
	return finishLearn(cfgPath, c, result, output, save)
}

func finishLearn(path string, cfg config.Config, result learnedKey, output string, save bool) error {
	fmt.Printf("Verified %d frames across %d distinct DPs (src=%s dst=%s).\n", result.VerifiedFrames, result.DistinctDPs, result.Source, result.Destination)
	if save {
		changed, err := config.UpdateBeaconKeyConfig(path, cfg.Device.DeviceID, cfg.Device.LocalKey, result.BeaconKey)
		if err != nil {
			return fmt.Errorf("save beacon_key in config: %w", err)
		}
		if changed {
			fmt.Printf("Saved device.beacon_key in %s\n", path)
		} else {
			fmt.Printf("device.beacon_key already matches in %s\n", path)
		}
	}
	if output != "" {
		if err := saveLearnResult(output, result); err != nil {
			return fmt.Errorf("export learned key: %w", err)
		}
		fmt.Printf("Exported learned key to %s\n", output)
	}
	return nil
}
