package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
	"example.com/tuya-beacon-mesh-p2-controller/internal/state"
)

type Options struct {
	Src, Dst, PhoneSrc           uint16
	FlagByte                     byte
	Frames, KelvinMin, KelvinMax int
	DryRun, MultiDP, InvertTemp  bool
}

type Sender interface {
	Send(context.Context, []p2.WireFrame) error
}

type Controller struct {
	Sender                           Sender
	Options                          Options
	DeviceID, StatePath, Fingerprint string
	LocalKey, BeaconKey              []byte
	// Prepared runs after sequence reservation and before transmission.
	Prepared func(Result)
}

type Result struct {
	Frames   []p2.WireFrame
	Warnings []string
}

func (lamp Controller) Snapshot() (state.Snapshot, error) {
	return state.Read(lamp.StatePath, lamp.DeviceID, lamp.Fingerprint)
}

func (lamp Controller) Execute(ctx context.Context, args []string) (Result, error) {
	var result Result
	o := lamp.Options
	if len(args) == 0 {
		return result, errors.New("missing command")
	}
	action := strings.ToLower(args[0])
	if !o.DryRun {
		lock, err := state.OpenTxLock(ctx, lamp.StatePath)
		if err != nil {
			return result, fmt.Errorf("acquire transmit lock: %w", err)
		}
		defer state.CloseTxLock(lock)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	store, err := state.Open(lamp.StatePath, lamp.DeviceID, lamp.Fingerprint)
	if err != nil {
		return result, err
	}
	defer store.Close()
	var commands []p2.DPCommand
	if action == "scene" && len(args) > 1 {
		var request p2.SceneRequest
		var inferred bool
		commands, request, inferred, err = p2.SceneCommands(args[1:], store.State.SceneTemplates)
		if err == nil && inferred {
			result.Warnings = append(result.Warnings, fmt.Sprintf("WARNING: scene %d (%s) uses INFERRED, not captured, DP73 RAW: %s", request.Preset.ID, request.Preset.Name, request.Preset.Raw))
		}
	} else {
		commands, err = p2.CommandForArgs(action, args[1:], o.KelvinMin, o.KelvinMax, o.InvertTemp)
	}
	if err != nil {
		return result, err
	}
	if o.MultiDP {
		commands, err = p2.PackCommands(commands)
		if err != nil {
			return result, err
		}
	}
	n := len(commands) * o.Frames
	var seqs []uint16
	if o.DryRun {
		if store.State.LastReserved+uint32(n) > 0xffff {
			return result, errors.New("seq would wrap")
		}
		for i := 0; i < n; i++ {
			seqs = append(seqs, uint16(store.State.LastReserved+uint32(i)+1))
		}
	} else {
		if store.FirstRun {
			result.Warnings = append(result.Warnings, "WARNING: first run seeded from old capture (0x008E). If Smart Life was used since then, run 'seq sync' to the last observed phone seq before sending.")
		}
		seqs, err = store.Reserve(n)
		if err != nil {
			return result, err
		}
	}
	frames := make([]p2.WireFrame, 0, n)
	for i, cmd := range commands {
		for j := 0; j < o.Frames; j++ {
			fr, err := p2.BuildFrame(o.Src, o.Dst, seqs[i*o.Frames+j], cmd, lamp.LocalKey, lamp.BeaconKey, o.FlagByte)
			if err != nil {
				return result, err
			}
			frames = append(frames, fr)
		}
	}

	result.Frames = frames
	store.Close()
	if lamp.Prepared != nil {
		lamp.Prepared(result)
		result.Warnings = nil
	}
	if o.DryRun {
		return result, nil
	}
	if err := lamp.Sender.Send(ctx, frames); err != nil {
		return result, err
	}
	if err := state.SaveSentState(lamp.StatePath, lamp.DeviceID, lamp.Fingerprint, commands, uint32(seqs[len(seqs)-1])); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("warning: state light tracking: %v", err))
	}
	return result, nil
}
