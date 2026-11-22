package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"example.com/tuya-beacon-mesh-p2-controller/internal/app"
	"example.com/tuya-beacon-mesh-p2-controller/internal/config"
	"example.com/tuya-beacon-mesh-p2-controller/internal/monitor"
	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
	"example.com/tuya-beacon-mesh-p2-controller/internal/provision"
)

func Run(ctx context.Context, argv []string) error {
	settingsPath := config.PathFromArgs(argv)
	defaults, err := config.Read(settingsPath)
	if err != nil {
		return err
	}

	learnTimeoutDefault, _ := time.ParseDuration(defaults.Learn.Timeout)
	qrTimeoutDefault, _ := time.ParseDuration(defaults.QR.Timeout)
	radio := app.RadioDefaults(defaults.Controller)
	bridge := app.BridgeDefaults(defaults)
	fs := flag.NewFlagSet("tuya-beacon-mesh-p2-controller", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfgPath := fs.String("config", settingsPath, "config file")
	statePath := fs.String("state", defaults.Controller.State, "seq state file")
	fs.StringVar(&radio.Adapter, "adapter", radio.Adapter, "BlueZ adapter")
	fs.StringVar(&radio.Src, "src", radio.Src, "mesh source address")
	fs.StringVar(&radio.Dst, "dst", radio.Dst, "mesh destination address")
	fs.IntVar(&radio.Frames, "frames", radio.Frames, "frames per command")
	fs.DurationVar(&radio.Duration, "duration", radio.Duration, "advertising window")
	fs.DurationVar(&radio.Gap, "gap", radio.Gap, "gap between frames")

	fs.DurationVar(&radio.AdvInterval, "adv-interval", radio.AdvInterval, "BLE advertising event interval; 20ms matches tuya-beacon-linux")
	fs.StringVar(&radio.Flags, "flags", radio.Flags, "advertising flags")
	fs.IntVar(&radio.KelvinMin, "kelvin-min", radio.KelvinMin, "minimum lamp Kelvin")
	fs.IntVar(&radio.KelvinMax, "kelvin-max", radio.KelvinMax, "maximum lamp Kelvin")
	fs.BoolVar(&radio.InvertTemp, "invert-temp", radio.InvertTemp, "reverse temp-to-DP mapping")
	fs.BoolVar(&radio.ClearAll, "clear-all", radio.ClearAll, "clear every advertising instance: opt-in only")
	fs.StringVar(&radio.Backend, "transport", radio.Backend, "mgmt (fast) | mgmt-legacy | raw-hci | btmgmt")
	fs.BoolVar(&radio.MultiDP, "multi-dp", radio.MultiDP, "pack several datapoints in 16B block")
	fs.StringVar(&radio.PhoneSrc, "phone-src", radio.PhoneSrc, "phone mesh source for monitor")
	learnSave := fs.Bool("learn-save", defaults.Learn.Save, "save verified beacon_key to config")
	learnOut := fs.String("learn-out", defaults.Learn.Out, "save recovered key in new 0600 JSON file")
	learnAuto := fs.Bool("learn-auto", defaults.Learn.Auto, "auto detect src/dst on verified P2 frames")
	learnTimeout := fs.Duration("learn-timeout", learnTimeoutDefault, "listen duration for learn live")
	cloudRegion := fs.String("cloud-region", defaults.Cloud.Region, "Tuya cloud region eu or we")
	cloudDeviceID := fs.String("cloud-device-id", defaults.Cloud.DeviceID, "override device ID for Tuya API")
	cloudSave := fs.Bool("cloud-save", defaults.Cloud.Save, "update local_key in config and backup existing file")
	qrUserCode := fs.String("qr-user-code", defaults.QR.UserCode, "Smart Life user code")
	qrApp := fs.String("qr-app", defaults.QR.App, "smartlife|tuyaSmart")
	qrTimeout := fs.Duration("qr-timeout", qrTimeoutDefault, "QR login deadline")
	qrSave := fs.Bool("qr-save", defaults.QR.Save, "save key and backup config after QR login")
	fs.StringVar(&bridge.Address, "mqtt-address", bridge.Address, "MQTT broker host:port")
	fs.StringVar(&bridge.Prefix, "mqtt-prefix", bridge.Prefix, "MQTT topic prefix (default tuya-beacon-mesh-p2-controller/<device-id>)")
	fs.StringVar(&bridge.User, "mqtt-user", bridge.User, "MQTT username")
	mqttPasswordEnv := fs.String("mqtt-password-env", defaults.MQTT.PasswordEnv, "environment variable containing MQTT password")
	fs.StringVar(&bridge.ClientID, "mqtt-client-id", bridge.ClientID, "MQTT client ID")
	fs.BoolVar(&bridge.Discovery, "mqtt-discovery", bridge.Discovery, "publish Home Assistant MQTT discovery")
	fs.StringVar(&bridge.DiscoveryPrefix, "mqtt-discovery-prefix", bridge.DiscoveryPrefix, "Home Assistant discovery prefix")

	fs.DurationVar(&bridge.Debounce, "mqtt-debounce", bridge.Debounce, "coalesce MQTT slider updates (default 100ms)")
	fs.DurationVar(&bridge.MaxWait, "mqtt-max-wait", bridge.MaxWait, "maximum wait for intermediate slider value (default 450ms)")
	fs.DurationVar(&bridge.DedupWindow, "mqtt-dedup-window", bridge.DedupWindow, "suppress identical recent MQTT commands")

	fs.DurationVar(&bridge.TxDuration, "mqtt-tx-duration", bridge.TxDuration, "BLE advertising dwell per MQTT command (default 250ms)")
	fs.BoolVar(&radio.DryRun, "dry-run", radio.DryRun, "print frames without sending")
	scan := fs.Bool("scan", defaults.Controller.Scan, "start BLE scan while monitoring")
	fs.Usage = func() { fmt.Fprint(fs.Output(), usageText) }
	if len(argv) == 0 {
		fs.Usage()
		return nil
	}
	if err := fs.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	args := fs.Args()
	if len(args) == 0 {
		fs.Usage()
		return errors.New("missing command")
	}
	// Listing built-in presets requires neither secrets, state nor Bluetooth.
	action := strings.ToLower(args[0])
	if action == "scene" && len(args) == 2 && args[1] == "list" {
		p2.PrintSceneList(os.Stdout)
		return nil
	}
	options, err := app.ValidateRadio(radio)
	if err != nil {
		return err
	}
	if action == "login" {
		if len(args) != 1 {
			return errors.New("usage: tuya-beacon-mesh-p2-controller [-qr-user-code CODE] [-qr-save=true] login")
		}
		if *qrTimeout < 30*time.Second || *qrTimeout > 10*time.Minute {
			return errors.New("-qr-timeout must be 30s..10m")
		}
		return provision.RunQRLocalKey(ctx, *cfgPath, *cloudDeviceID, *qrUserCode, *qrApp, *qrTimeout, *qrSave)
	}
	if action == "cloud" {
		if len(args) != 2 || args[1] != "local-key" {
			return errors.New("usage: tuya-beacon-mesh-p2-controller [-cloud-region eu|we] [-cloud-save] cloud local-key")
		}
		return provision.RunCloudLocalKey(ctx, *cfgPath, *cloudDeviceID, *cloudRegion, *cloudSave)
	}
	if action == "learn" {
		if len(args) != 2 {
			return errors.New("usage: tuya-beacon-mesh-p2-controller -learn-out beacon-key.json learn live|capture.cfa")
		}
		if args[1] == "live" {
			if *learnTimeout < 5*time.Second || *learnTimeout > 30*time.Minute {
				return errors.New("-learn-timeout must be 5s..30m")
			}
			return provision.LearnLive(ctx, *cfgPath, *learnOut, radio.Adapter, options.PhoneSrc, options.Dst, *learnAuto, *scan, *learnSave, *learnTimeout)
		}
		return provision.LearnFromCapture(*cfgPath, args[1], *learnOut, options.PhoneSrc, options.Dst, *learnAuto, *learnSave)
	}

	device, err := app.OpenDevice(*cfgPath, *statePath, options, radio.Transport())
	if err != nil {
		return err
	}
	lamp := device.Lamp
	// Monitor never holds the state lock for the life of the process.
	// Otherwise other CLI instances would block behind it forever.
	if action == "monitor" {
		if len(args) != 1 {
			return errors.New("usage: tuya-beacon-mesh-p2-controller [options] monitor")
		}
		if radio.DryRun {
			return errors.New("-dry-run does not apply to monitor")
		}
		if _, err := lamp.Snapshot(); err != nil {
			return err
		}
		return monitor.Run(ctx, device.Config, lamp.StatePath, radio.Adapter, options.PhoneSrc, options.Dst, lamp.LocalKey, lamp.BeaconKey, lamp.Fingerprint, *scan)
	}

	if action == "mqtt" {
		if len(args) != 1 {
			return errors.New("usage: tuya-beacon-mesh-p2-controller mqtt")
		}
		return device.StartBridge(ctx, bridge, *mqttPasswordEnv)
	}
	if action == "seq" || action == "state" || (action == "scene" && len(args) == 2 && args[1] == "current") {
		return stateCommand(action, lamp, args)
	}
	return app.ExecuteCommand(ctx, lamp, args)
}
