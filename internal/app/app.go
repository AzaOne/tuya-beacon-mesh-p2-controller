package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"example.com/tuya-beacon-mesh-p2-controller/internal/bluez"
	"example.com/tuya-beacon-mesh-p2-controller/internal/config"
	"example.com/tuya-beacon-mesh-p2-controller/internal/controller"
	"example.com/tuya-beacon-mesh-p2-controller/internal/mqtt"
	"example.com/tuya-beacon-mesh-p2-controller/internal/state"
)

type Device struct {
	configPath string
	Config     config.Config
	Lamp       controller.Controller
	Transport  bluez.Transport
}

func OpenDevice(configPath, statePath string, o controller.Options, transport bluez.Transport) (Device, error) {
	c, k1, k2, err := config.LoadDevice(configPath)
	if err != nil {
		return Device{}, fmt.Errorf("config %q: %w", configPath, err)
	}
	return newDevice(c, configPath, statePath, o, transport, k1, k2)
}

func newDevice(c config.Config, configPath, statePath string, o controller.Options, transport bluez.Transport, k1, k2 []byte) (Device, error) {
	fp := state.Fingerprint(c.Device.DeviceID, o.Src, o.Dst, k1, k2)
	if statePath == "" {
		statePath = state.DefaultStatePath(c.Device.DeviceID)
	}

	lamp := controller.Controller{
		Options: o, Sender: transport, DeviceID: c.Device.DeviceID,
		StatePath: statePath, Fingerprint: fp, LocalKey: k1, BeaconKey: k2,
	}
	return Device{configPath: configPath, Config: c, Lamp: lamp, Transport: transport}, nil
}

func BridgeDefaults(c config.Config) mqtt.Options {
	debounce, _ := time.ParseDuration(c.MQTT.Debounce)
	maxWait, _ := time.ParseDuration(c.MQTT.MaxWait)
	dedup, _ := time.ParseDuration(c.MQTT.DedupWindow)
	dwell, _ := time.ParseDuration(c.MQTT.TxDuration)
	return mqtt.Options{
		Address: c.MQTT.Broker, User: c.MQTT.Username, ClientID: c.MQTT.ClientID,
		Discovery: c.MQTT.HADiscoveryEnabled, DiscoveryPrefix: c.MQTT.HADiscoveryPrefix,
		Debounce: debounce, MaxWait: maxWait, DedupWindow: dedup, TxDuration: dwell,
	}
}

func (d Device) StartBridge(ctx context.Context, opt mqtt.Options, passwordEnv string) error {
	c, lamp := d.Config, d.Lamp
	if !c.MQTT.Enabled {
		return errors.New("MQTT disabled by config mqtt.enabled=false")
	}
	if opt.Prefix == "" {
		opt.Prefix = mqtt.DevicePrefix(c.MQTT.TopicPrefix, lamp.DeviceID)
	}
	opt.Password = c.MQTT.Password
	if passwordEnv != "" {
		opt.Password = os.Getenv(passwordEnv)
	}
	if opt.Debounce < 0 || opt.Debounce > 5*time.Second {
		return errors.New("--mqtt-debounce must be 0..5s")
	}
	if opt.MaxWait < opt.Debounce || opt.MaxWait > 10*time.Second {
		return errors.New("--mqtt-max-wait must be >= debounce and <= 10s")
	}
	if opt.TxDuration < 100*time.Millisecond || opt.TxDuration > 1600*time.Millisecond {
		return errors.New("--mqtt-tx-duration must be 100ms..1600ms")
	}
	if opt.DedupWindow < 0 || opt.DedupWindow > time.Minute {
		return errors.New("--mqtt-dedup-window must be 0..1m")
	}

	opt.DeviceName = c.Device.Name
	opt.LightName = c.MQTT.LightName
	opt.DeviceModel = c.Device.Model
	opt.Firmware = c.Device.Firmware
	opt.ProductID = c.Device.ProductID
	opt.Hardware = c.Device.Hardware
	opt.StatePath = lamp.StatePath
	opt.DeviceID = lamp.DeviceID
	opt.Fingerprint = lamp.Fingerprint
	opt.Adapter = d.Transport.Adapter
	opt.Backend = d.Transport.Backend
	opt.KelvinMin = lamp.Options.KelvinMin
	opt.KelvinMax = lamp.Options.KelvinMax
	opt.InvertTemp = lamp.Options.InvertTemp
	d.Transport.Duration = opt.TxDuration

	if _, err := lamp.Snapshot(); err != nil {
		return err
	}
	return mqtt.Run(ctx, opt, d.executeBridgeCommand)
}

// Bridge flags are fixed for this session. Read device credentials and the
// configured transmission policy for each command, as the standalone CLI does.
func (d Device) executeBridgeCommand(ctx context.Context, args []string) error {
	current, err := OpenDevice(d.configPath, d.Lamp.StatePath, d.Lamp.Options, d.Transport)
	if err != nil {
		return err
	}
	current.Lamp.Options.DryRun = current.Config.Controller.DryRun || (d.Lamp.Options.DryRun && !d.Config.Controller.DryRun)
	current.Transport.ClearAll = current.Config.Controller.ClearAll
	current.Lamp.Sender = current.Transport
	return ExecuteCommand(ctx, current.Lamp, args)
}

func RunServer(ctx context.Context, configPath, statePath string) error {
	c, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if !c.MQTT.Enabled {
		return errors.New("no enabled services: mqtt.enabled=false")
	}
	k1, k2, err := c.DeviceKeys()
	if err != nil {
		return err
	}
	radio := RadioDefaults(c.Controller)
	o, err := ValidateRadio(radio)
	if err != nil {
		return err
	}
	if statePath == "" {
		statePath = c.Controller.State
	}
	device, err := newDevice(c, configPath, statePath, o, radio.Transport(), k1, k2)
	if err != nil {
		return err
	}
	return device.StartBridge(ctx, BridgeDefaults(c), c.MQTT.PasswordEnv)
}
