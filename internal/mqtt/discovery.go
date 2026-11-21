package mqtt

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
	"example.com/tuya-beacon-mesh-p2-controller/internal/state"
)

// These identities are stable across restarts. Each HA light/number references
// a real controller device, rather than attempting to supply a display string
// in the via_device field.
const softwareVersion = "9.3.6"

func controllerID(opt Options) string {
	id := strings.TrimSpace(opt.ClientID)
	if id == "" {
		id = "tuya-p2-" + state.CleanDeviceID(opt.DeviceID)
	}
	return "tuya_p2_controller_" + state.CleanDeviceID(id)
}

// Like Zigbee2MQTT, the bridge has its own root-level state, info and devices
// subtrees. The lamp's existing MQTT command topics remain unchanged.
func rootPrefix(opt Options) string {
	prefix := strings.Trim(strings.TrimSpace(opt.Prefix), "/")
	suffix := "/" + state.CleanDeviceID(opt.DeviceID)
	if strings.HasSuffix(prefix, suffix) {
		prefix = strings.TrimSuffix(prefix, suffix)
	}
	if prefix == "" {
		return "tuya-beacon-mesh-p2-controller"
	}
	return prefix
}

func bridgeStateTopic(opt Options) string { return rootPrefix(opt) + "/bridge/state" }

func bridgeInfoTopic(opt Options) string { return rootPrefix(opt) + "/bridge/info" }

func bridgeDevicesTopic(opt Options) string { return rootPrefix(opt) + "/bridge/devices" }

func bridgeStateJSON(state string) []byte {
	b, _ := json.Marshal(map[string]string{"state": state})
	return b
}

func bridgeInfoJSON(opt Options) ([]byte, error) {
	return json.Marshal(map[string]any{
		"version": softwareVersion, "adapter": opt.Adapter,
		"transport": opt.Backend, "client_id": opt.ClientID,
		"device_count": 1, "protocol": "Tuya Beacon Mesh P2",
	})
}

func bridgeDevicesJSON(opt Options) ([]byte, error) {
	product := strings.TrimSpace(opt.ProductID)
	if product == "" {
		product = strings.TrimSpace(opt.Hardware)
	}
	if product == "" && opt.DeviceID == "test-lamp" {
		product = "wbl39qpt"
	}
	return json.Marshal([]map[string]any{{
		"id": opt.DeviceID, "friendly_name": fmt.Sprintf("Smart bulb (%s)", opt.DeviceID),
		"manufacturer": "Tuya", "product_id": product, "type": "Beacon P2 light",
	}})
}

// Retain the old discovery topic/unique_id to update the existing HA entity in
// place. The bridge's LWT lives at bridge/state, just like Zigbee2MQTT.
func controllerDiscoveryTopic(opt Options) string {
	return strings.Trim(opt.DiscoveryPrefix, "/") + "/binary_sensor/" + controllerID(opt) + "_connection/config"
}

func bridgeMetadata(opt Options) map[string]any {
	return map[string]any{
		"identifiers": []string{controllerID(opt)},
		"name":        "tuya-beacon-mesh-p2-controller Bridge", "manufacturer": "AzaOne",
		"model": "Bridge", "sw_version": softwareVersion,
	}
}

func controllerDiscoveryJSON(opt Options) ([]byte, error) {
	id := controllerID(opt)
	return json.Marshal(map[string]any{
		"name": "Bridge state", "unique_id": id + "_connection",
		"object_id": id + "_connection", "device_class": "connectivity",
		"entity_category": "diagnostic", "icon": "mdi:lan-connect",
		"state_topic":    bridgeStateTopic(opt),
		"value_template": "{{ value_json.state }}",
		"payload_on":     "online", "payload_off": "offline",
		"device": bridgeMetadata(opt),
		"origin": map[string]any{"name": "tuya-beacon-mesh-p2-controller", "sw": softwareVersion},
	})
}

// Only expose real bridge properties; unlike Zigbee2MQTT there is no true
// linkquality, paired-device query, OTA status or coordinator controls here.
func bridgeDiagnosticEntries(opt Options) ([]discoveryEntry, error) {
	type metric struct{ field, name, icon string }
	metrics := []metric{
		{"version", "Version", "mdi:information-outline"},
		{"adapter", "Adapter", "mdi:bluetooth"},
		{"transport", "Transport", "mdi:transit-connection-variant"},
	}
	out := make([]discoveryEntry, 0, len(metrics))
	for _, m := range metrics {
		id := controllerID(opt) + "_" + m.field
		topic := strings.Trim(opt.DiscoveryPrefix, "/") + "/sensor/" + id + "/config"
		payload := map[string]any{
			"name": m.name, "unique_id": id, "object_id": id,
			"entity_category": "diagnostic", "icon": m.icon,
			"state_topic":           bridgeInfoTopic(opt),
			"value_template":        "{{ value_json." + m.field + " }}",
			"availability_topic":    bridgeStateTopic(opt),
			"availability_template": "{{ value_json.state }}",
			"payload_available":     "online", "payload_not_available": "offline",
			"device": bridgeMetadata(opt),
			"origin": map[string]any{"name": "tuya-beacon-mesh-p2-controller", "sw": softwareVersion},
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		out = append(out, discoveryEntry{Topic: topic, Payload: raw})
	}
	return out, nil
}

type discoveryEntry struct {
	Topic   string
	Payload []byte
}

func discoveryEntries(opt Options) ([]discoveryEntry, error) {
	entries := []struct {
		topic string
		fn    func(Options) ([]byte, error)
	}{
		{controllerDiscoveryTopic(opt), controllerDiscoveryJSON},
		{discoveryTopic(opt), discoveryJSON},
		{speedDiscoveryTopic(opt), speedDiscoveryJSON},
	}
	results := make([]discoveryEntry, 0, len(entries)+3)
	for _, entry := range entries {
		payload, err := entry.fn(opt)
		if err != nil {
			return nil, err
		}
		results = append(results, discoveryEntry{Topic: entry.topic, Payload: payload})
	}
	diagnostics, err := bridgeDiagnosticEntries(opt)
	if err != nil {
		return nil, err
	}
	return append(results, diagnostics...), nil
}

func publishDiscovery(conn net.Conn, opt Options) error {
	entries, err := discoveryEntries(opt)
	if err != nil {
		return err
	}
	for i, entry := range entries {
		payload := entry.Payload
		if !opt.Discovery {
			payload = nil
		}
		if err := publish(conn, entry.Topic, payload, true, 1, uint16(i+2)); err != nil {
			return err
		}
	}
	return nil
}

func publishBridgeSnapshot(conn net.Conn, opt Options) error {
	info, err := bridgeInfoJSON(opt)
	if err != nil {
		return err
	}
	devices, err := bridgeDevicesJSON(opt)
	if err != nil {
		return err
	}
	for _, entry := range []struct {
		topic   string
		payload []byte
	}{
		{bridgeInfoTopic(opt), info},
		{bridgeDevicesTopic(opt), devices},
		{bridgeStateTopic(opt), bridgeStateJSON("online")},
	} {
		if err := publish(conn, entry.topic, entry.payload, true, 0, 0); err != nil {
			return err
		}
	}
	return nil
}

func deviceMetadata(opt Options) map[string]any {
	// The displayed device name is intentionally separate from the MQTT light
	// entity's friendly name, which still comes from device.name in config.
	dev := map[string]any{
		"identifiers":  []string{"tuya_p2_" + state.CleanDeviceID(opt.DeviceID)},
		"name":         fmt.Sprintf("Smart bulb (%s)", opt.DeviceID),
		"manufacturer": "Tuya",
		"via_device":   controllerID(opt),
	}
	firmware := strings.TrimSpace(opt.Firmware)
	// Product ID is part of device identity, not a hardware revision, but is
	// intentionally displayed as Hardware in HA per the user's preference.
	// The old device.hardware key stays readable for existing configs.
	hardware := strings.TrimSpace(opt.ProductID)
	if hardware == "" {
		hardware = strings.TrimSpace(opt.Hardware)
	}
	// Backward-compatible metadata for the specific lamp previously identified
	// through the owner's Tuya Cloud API. Other products must configure these.
	if opt.DeviceID == "test-lamp" {
		if firmware == "" {
			firmware = "1.2"
		}
		if hardware == "" {
			hardware = "wbl39qpt"
		}
	}
	// Present the same descriptive model line as Zigbee2MQTT, while leaving
	// the Tuya Product ID in Hardware as requested. A custom device.model
	// overrides the human-friendly description, not the actual Product ID.
	model := strings.TrimSpace(opt.DeviceModel)
	if model == "" && opt.DeviceID == "test-lamp" {
		model = "Smart Bulb 2 (Tuya Beacon Mesh P2)"
	}
	if model != "" {
		if hardware != "" && !strings.Contains(model, "("+hardware+")") {
			model += " (" + hardware + ")"
		}
		dev["model"] = model
	}
	if firmware != "" {
		dev["sw_version"] = firmware
	}
	if hardware != "" {
		dev["hw_version"] = hardware
	}
	return dev
}

func discoveryTopic(opt Options) string {
	return strings.Trim(opt.DiscoveryPrefix, "/") + "/light/tuya_p2_" + state.CleanDeviceID(opt.DeviceID) + "/config"
}

// Give the main light a short, distinct entity label. Home Assistant combines
// it with the parent device context where needed; name:null would show the
// device name again in the device's Controls section.
func lightName(opt Options) string {
	if name := strings.TrimSpace(opt.LightName); name != "" {
		return name
	}
	return "Light"
}

func discoveryJSON(opt Options) ([]byte, error) {
	base := opt.Prefix
	id := "tuya_p2_" + state.CleanDeviceID(opt.DeviceID)
	effects := make([]string, 0, len(p2.ScenePresets))
	for _, p := range p2.ScenePresets {
		effects = append(effects, p.Name)
	}
	// A single Home Assistant JSON command replaces multiple independent
	// /power,/brightness,/rgb MQTT publications generated by basic schema.
	data := map[string]any{
		"name": lightName(opt), "unique_id": id, "object_id": id, "icon": "mdi:lightbulb",
		"schema": "json", "command_topic": base + "/ha/set", "state_topic": base + "/ha/state",
		"brightness": true, "brightness_scale": 100,
		"supported_color_modes": []string{"rgb", "color_temp"},
		"color_temp_kelvin":     true, "min_kelvin": opt.KelvinMin, "max_kelvin": opt.KelvinMax,
		"effect": true, "effect_list": effects,
		"transition": false, "flash": false,
		"availability_topic": bridgeStateTopic(opt), "availability_template": "{{ value_json.state }}", "payload_available": "online", "payload_not_available": "offline",
		"optimistic": false, // Display one toggle; /ha/state is locally observed/commanded, not BLE-acknowledged.
		"device":     deviceMetadata(opt),
		"origin":     map[string]any{"name": "tuya-beacon-mesh-p2-controller", "sw": softwareVersion},
	}
	return json.Marshal(data)
}

// HSV to RGB state follows DP11's 0..360, 0..100, 0..100 ranges.
// MQTT Number entity: Home Assistant does not have a native effect-speed
// property on light. A number on the same HA device is the interoperable way.
func speedDiscoveryTopic(opt Options) string {
	return strings.Trim(opt.DiscoveryPrefix, "/") + "/number/tuya_p2_" + state.CleanDeviceID(opt.DeviceID) + "_effect_speed/config"
}

func speedDiscoveryJSON(opt Options) ([]byte, error) {
	id := "tuya_p2_" + state.CleanDeviceID(opt.DeviceID)
	return json.Marshal(map[string]any{
		"name": "Effect speed", "unique_id": id + "_effect_speed", "object_id": id + "_effect_speed",
		"icon": "mdi:speedometer", "min": 1, "max": 100, "step": 1, "mode": "slider",
		"unit_of_measurement": "%", "payload_reset": "unknown",
		"command_topic": opt.Prefix + "/effect/speed/set", "state_topic": opt.Prefix + "/effect/speed/state",
		"availability_topic": bridgeStateTopic(opt), "availability_template": "{{ value_json.state }}", "payload_available": "online", "payload_not_available": "offline",
		"device": deviceMetadata(opt),
		"origin": map[string]any{"name": "tuya-beacon-mesh-p2-controller", "sw": softwareVersion},
	})
}
