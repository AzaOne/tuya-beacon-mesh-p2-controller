package cli

import "example.com/tuya-beacon-mesh-p2-controller/internal/version"

var usageText = `tuya-beacon-mesh-p2-controller ` + version.String() + ` — Tuya Beacon Mesh P2 CLI / BlueZ advertising

Usage: tuya-beacon-mesh-p2-controller [options] <command> [args]

Lighting:
  tuya-beacon-mesh-p2-controller on | off
  tuya-beacon-mesh-p2-controller brightness 50 | brightness-raw 500
  tuya-beacon-mesh-p2-controller temp 4000 | temp-raw 500
  tuya-beacon-mesh-p2-controller white 4000 80
  tuya-beacon-mesh-p2-controller rgb 100 232 111 | hsv 120 100 100
  tuya-beacon-mesh-p2-controller mode white|colour|scene|music   (music not verified)
  tuya-beacon-mesh-p2-controller scene list                        list all 28 DP73 presets
  tuya-beacon-mesh-p2-controller scene 8                           select by ID
  tuya-beacon-mesh-p2-controller scene "Red & blue gradient"       select by name
  tuya-beacon-mesh-p2-controller scene 8 --speed 50 --brightness 80
  tuya-beacon-mesh-p2-controller scene current                     last observed/sent scene
  tuya-beacon-mesh-p2-controller scene                            legacy: mode scene only
  tuya-beacon-mesh-p2-controller raw 0241010b0400786464      multiple complete TLVs allowed
  tuya-beacon-mesh-p2-controller dp bool|enum|value|raw ID VALUE  (experimental)

Monitoring, recovery, automation:
  tuya-beacon-mesh-p2-controller monitor                    intercept phone BLE packets and sync seq
  tuya-beacon-mesh-p2-controller seq show                   print current seq and state path
  tuya-beacon-mesh-p2-controller state show                 print last optimistic lamp state
  tuya-beacon-mesh-p2-controller seq sync 0x0092            advance the seq counter (never rewind)
  tuya-beacon-mesh-p2-controller seq sync-log capture.cfa   verify crypto before syncing Android HCI log
  tuya-beacon-mesh-p2-controller learn capture.cfa          recover beacon_key from Android BTSnoop
  tuya-beacon-mesh-p2-controller learn live                 recover beacon_key from PC over-air BLE
  tuya-beacon-mesh-p2-controller login                      obtain local_key by Smart Life QR (no developer account)
  tuya-beacon-mesh-p2-controller cloud local-key            alternative: fetch using Tuya OpenAPI
  tuya-beacon-mesh-p2-controller -learn-out key.json learn capture.cfa   save recovered key (0600)
  tuya-beacon-mesh-p2-controller mqtt                       MQTT bridge with optional Home Assistant discovery

Options BEFORE command:
  -config FILE         JSON containing device.local_key + beacon_key
  -state FILE          state JSON; default $XDG_STATE_HOME/tuya-beacon-mesh-p2-controller/state-<device-id>.json
  -adapter hci0        Bluetooth adapter
  -src 0x7a59          local logical transmitter source, experimental alternatives
  -phone-src 0x7a59    source of Smart Life commands, for monitor/sync/learn
  -dst 0x0003          target lamp logical address
  -transport mgmt      mgmt (default) | mgmt-legacy | raw-hci | btmgmt
  -multi-dp=true       combine compatible DP-TLVs into one 16B block
  -frames 1            new consecutive seq values per block (1..4)
  -duration 500ms      dwell time per advertising frame (300ms..1600ms)
  -gap 120ms           delay between distinct advertised frames
  -flags 01            BLE AD Flags: 01|02|06
  -kelvin-min 2700     approximate warmth calibration endpoint
  -kelvin-max 6500     approximate coolness endpoint
  -invert-temp         reverse Kelvin -> DP4 mapping
  -scan=true           start active scan when monitoring
  -clear-all           only for legacy btmgmt fallback (dangerous)
  -dry-run             print frames without transmitting/reserving seq
  -learn-save=true     update device.beacon_key in config after verification
  -learn-out FILE      create new 0600 beacon-key JSON after learn
  -learn-auto=true     discover source/destination from verified frames
  -learn-timeout 120s  timeout for live learn
  -qr-user-code CODE   Smart Life User Code (or prompted interactively)
  -qr-app smartlife    smartlife|tuyaSmart QR format
  -qr-timeout 4m       wait for QR confirmation (30s..10m)
  -qr-save=true        save local_key to config after success (0600, backup)
  -cloud-region eu     eu=Central Europe, we=Western Europe
  -cloud-device-id ID  override ID read from config
  -cloud-save          securely update config, backing up old key
  Cloud OpenAPI credentials only: TUYA_CLIENT_ID and TUYA_CLIENT_SECRET
  QR login needs optional system tool qrencode (Arch: pacman -S qrencode)
  -mqtt-address URL         broker (config mqtt.broker), tcp://host:1883
  -mqtt-prefix TOPIC        override full device topic prefix (JSON mqtt.topic_prefix is root)
  -mqtt-user USER           optional MQTT username
  -mqtt-client-id ID        optional MQTT client ID
  -mqtt-discovery=true      publish HA MQTT light discovery
  -mqtt-discovery-prefix    default homeassistant
  -mqtt-password-env ENV    override JSON mqtt.password via environment variable
  -mqtt-debounce 300ms      coalesce fast HA slider updates (JSON mqtt.debounce)
  -mqtt-max-wait 1200ms     max interval for intermediate updates (JSON mqtt.max_wait)
  -mqtt-dedup-window 2s     ignore recent duplicate commands (JSON mqtt.dedup_window)
  All runtime options may be set in the JSON controller/learn/qr/cloud/mqtt objects;
  explicit CLI options take precedence. See config.example.json.

Examples:
  tuya-beacon-mesh-p2-controller -dry-run rgb 100 232 111
  tuya-beacon-mesh-p2-controller -multi-dp=false -transport btmgmt on
  tuya-beacon-mesh-p2-controller -transport raw-hci scene 8
  tuya-beacon-mesh-p2-controller login
  tuya-beacon-mesh-p2-controller -qr-user-code ABCDEF login
  tuya-beacon-mesh-p2-controller -adapter hci1 monitor
  tuya-beacon-mesh-p2-controller -mqtt-prefix room/lamp mqtt

All radio commands are optimistic: neither MGMT nor BLE Advertising provides
an acknowledgment that the lamp applied them. Keep secrets and state private.
See PROTOKOL.md for the 26-byte frame format and both-key XXTEA/XOR scheme.
`
