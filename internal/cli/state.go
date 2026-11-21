package cli

import (
	"encoding/json"
	"errors"
	"fmt"

	"example.com/tuya-beacon-mesh-p2-controller/internal/capture"
	"example.com/tuya-beacon-mesh-p2-controller/internal/controller"
	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
	"example.com/tuya-beacon-mesh-p2-controller/internal/state"
)

func stateCommand(action string, lamp controller.Controller, args []string) error {
	store, err := state.Open(lamp.StatePath, lamp.DeviceID, lamp.Fingerprint)
	if err != nil {
		return err
	}
	defer store.Close()

	if action == "state" {
		if len(args) != 2 || args[1] != "show" {
			return errors.New("usage: tuya-beacon-mesh-p2-controller state show")
		}
		data, err := json.MarshalIndent(store.State, "", "  ")
		if err != nil {
			return err
		}
		fmt.Printf("path=%s\n%s\n", lamp.StatePath, data)
		return nil
	}
	if action == "scene" && len(args) == 2 && args[1] == "current" {
		if store.State.Light == nil || store.State.Light.Scene == nil {
			fmt.Println("No observed/sent scene in state yet. Use 'scene <id>' or monitor Smart Life.")
			return nil
		}
		out, err := json.MarshalIndent(store.State.Light.Scene, "", "  ")
		if err != nil {
			return err
		}
		fmt.Printf("state=%s\n%s\n", lamp.StatePath, out)
		return nil
	}

	if len(args) < 2 {
		return errors.New("usage: seq show | seq sync <last-observed> | seq sync-log <file.cfa>")
	}
	switch args[1] {
	case "show":
		if len(args) != 2 {
			return errors.New("usage: seq show")
		}
		next := "exhausted"
		if store.State.LastReserved < 0xffff {
			next = fmt.Sprintf("0x%04X", store.State.LastReserved+1)
		}
		fmt.Printf("state=%s\nlast=0x%04X next=%s initialized=%t\n", lamp.StatePath, store.State.LastReserved, next, !store.FirstRun)
		return nil
	case "sync":
		if len(args) != 3 {
			return errors.New("usage: seq sync 0xNNNN")
		}
		v, err := p2.ParseU16(args[2])
		if err != nil {
			return err
		}
		changed, err := store.SyncTo(v)
		if err != nil {
			return err
		}
		fmt.Printf("seq synchronized: last=0x%04X updated=%t\n", store.State.LastReserved, changed)
		return nil
	case "sync-log":
		if len(args) != 3 {
			return errors.New("usage: seq sync-log <file.cfa|file.log>")
		}
		v, found, err := capture.LastVerifiedSeqFromCapture(args[2], lamp.Options.PhoneSrc, lamp.Options.Dst, lamp.LocalKey, lamp.BeaconKey)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("no P2 TX commands matching src=0x%04X dst=0x%04X in %s", lamp.Options.Src, lamp.Options.Dst, args[2])
		}
		changed, err := store.SyncTo(v)
		if err != nil {
			return err
		}
		fmt.Printf("seq synchronized from capture: last=0x%04X updated=%t\n", store.State.LastReserved, changed)
		return nil
	default:
		return errors.New("usage: seq show | seq sync <last-observed> | seq sync-log <file.cfa>")
	}
}
