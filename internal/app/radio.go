package app

import (
	"errors"
	"fmt"
	"time"

	"example.com/tuya-beacon-mesh-p2-controller/internal/bluez"
	"example.com/tuya-beacon-mesh-p2-controller/internal/config"
	"example.com/tuya-beacon-mesh-p2-controller/internal/controller"
	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
)

type RadioSettings struct {
	Adapter, Backend, Src, Dst, PhoneSrc, Flags string
	Frames, KelvinMin, KelvinMax                int
	Duration, Gap, AdvInterval                  time.Duration
	InvertTemp, DryRun, MultiDP, ClearAll       bool
}

func RadioDefaults(c config.ControllerSettings) RadioSettings {
	duration, _ := time.ParseDuration(c.Duration)
	gap, _ := time.ParseDuration(c.Gap)
	interval, _ := time.ParseDuration(c.AdvInterval)
	return RadioSettings{
		Adapter: c.Adapter, Backend: c.Transport,
		Src: c.Src, Dst: c.Dst, PhoneSrc: c.PhoneSrc, Flags: c.Flags,
		Frames: c.Frames, KelvinMin: c.KelvinMin, KelvinMax: c.KelvinMax,
		Duration: duration, Gap: gap, AdvInterval: interval,
		InvertTemp: c.InvertTemp, DryRun: c.DryRun, MultiDP: c.MultiDP, ClearAll: c.ClearAll,
	}
}

func ValidateRadio(v RadioSettings) (controller.Options, error) {
	if v.Frames < 1 || v.Frames > 4 {
		return controller.Options{}, errors.New("frames must be 1..4")
	}
	if v.Duration < 100*time.Millisecond || v.Duration > 1600*time.Millisecond {
		return controller.Options{}, errors.New("duration must be 100ms..1600ms")
	}
	if v.AdvInterval < 20*time.Millisecond || v.AdvInterval > 1000*time.Millisecond || v.AdvInterval%(625*time.Microsecond) != 0 {
		return controller.Options{}, errors.New("adv-interval must be 20ms..1000ms in steps of 625us")
	}
	if v.Gap < 0 || v.Gap > 5*time.Second {
		return controller.Options{}, errors.New("gap must be 0..5s")
	}
	if v.KelvinMin < 1000 || v.KelvinMax > 12000 || v.KelvinMin >= v.KelvinMax {
		return controller.Options{}, errors.New("invalid kelvin min/max")
	}
	src, err := p2.ParseU16(v.Src)
	if err != nil {
		return controller.Options{}, fmt.Errorf("src: %w", err)
	}
	dst, err := p2.ParseU16(v.Dst)
	if err != nil {
		return controller.Options{}, fmt.Errorf("dst: %w", err)
	}
	flagByte, err := p2.ParseHexByte(v.Flags)
	if err != nil {
		return controller.Options{}, err
	}
	phoneSrc, err := p2.ParseU16(v.PhoneSrc)
	if err != nil {
		return controller.Options{}, fmt.Errorf("phone-src: %w", err)
	}
	if v.Backend != "mgmt" && v.Backend != "mgmt-legacy" && v.Backend != "raw-hci" && v.Backend != "btmgmt" {
		return controller.Options{}, errors.New("transport must be mgmt|mgmt-legacy|raw-hci|btmgmt")
	}
	if flagByte != 1 && flagByte != 2 && flagByte != 6 {
		return controller.Options{}, errors.New("flags must be 01|02|06")
	}

	return controller.Options{
		Src: src, Dst: dst, PhoneSrc: phoneSrc, FlagByte: flagByte,
		Frames: v.Frames, KelvinMin: v.KelvinMin, KelvinMax: v.KelvinMax,
		InvertTemp: v.InvertTemp, MultiDP: v.MultiDP, DryRun: v.DryRun,
	}, nil
}

func (v RadioSettings) Transport() bluez.Transport {
	return bluez.Transport{
		Adapter: v.Adapter, Backend: v.Backend,
		Duration: v.Duration, Gap: v.Gap, AdvInterval: v.AdvInterval, ClearAll: v.ClearAll,
	}
}
