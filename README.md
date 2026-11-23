# Tuya Beacon Mesh P2 Controller

Control a Tuya Beacon Mesh P2 lamp from Linux over Bluetooth Low Energy. The CLI sends encrypted advertisements directly to the lamp. The MQTT server exposes the same controls to Home Assistant. Once the lamp keys are configured, both work locally.

## Features

- Control power, brightness, white temperature, RGB and HSV colors, and 28 built-in scenes with adjustable speed and brightness.
- Use Home Assistant MQTT Discovery for the light, effect speed control, and bridge diagnostics.
- Monitor commands from Smart Life, synchronize sequence numbers, and recover the beacon key from live traffic or an Android BTSnoop capture.

## Quick start

You need a Linux amd64 or arm64 PC within Bluetooth range of the paired lamp, an adapter that supports LE advertising, and the lamp’s Smart Life account. The user installer uses systemd, `sudo`, and `setcap`. For key recovery, install `qrencode` and BlueZ’s `btmgmt`. MQTT is optional for CLI control.

### Build and install

With Go 1.23+ and a C compiler installed, run as your login user:

```sh
git clone https://github.com/AzaOne/tuya-beacon-mesh-p2-controller.git
cd tuya-beacon-mesh-p2-controller
./build.sh
./install.sh
```

The installer places the CLI and server in `~/.local/bin`. Include that directory in your `PATH`.

### Configure and switch on the lamp

From the same directory, create a private config for your user:

```sh
configdir="${XDG_CONFIG_HOME:-$HOME/.config}/tuya-beacon-mesh-p2-controller"
install -d -m 0700 "$configdir"
test -e "$configdir/config.json" || install -m 0600 config.example.json "$configdir/config.json"
```

Open the lamp in Smart Life, then open its settings and **Device Information**. Copy the lamp’s **Virtual ID** into `device.device_id` in `$configdir/config.json`. Set `controller.adapter` if your Bluetooth adapter differs from `hci0`. The example selects `raw-hci`.

1. Run `tuya-beacon-mesh-p2-controller login`. Enter the **User Code** from **Smart Life → Me → Settings → Account and Security**, scan the terminal QR code with Smart Life, and confirm. The CLI saves the lamp’s local key.
2. Run `sudo -v`, then `tuya-beacon-mesh-p2-controller learn live` as your login user. Switch the lamp off and on in Smart Life near the adapter. The CLI saves the recovered beacon key. Check the reported mesh addresses against `controller.phone_src`, `controller.src`, and `controller.dst`. If you already have both keys, enter them directly in the config and check the addresses.
3. Run `tuya-beacon-mesh-p2-controller on` and check that the lamp turns on. For Home Assistant, configure `mqtt.broker` and credentials, then start the [user service](#user-service).

## Configuration

Use `controller.transport` to select `raw-hci`, `mgmt` (the application default), `mgmt-legacy`, or `btmgmt`. [Full configuration example](config.example.json)

When the working directory contains both `config.json` and a `state/` directory, both binaries use those local files. Otherwise they use `$XDG_CONFIG_HOME/tuya-beacon-mesh-p2-controller/config.json` and `$XDG_STATE_HOME/tuya-beacon-mesh-p2-controller/state-<device-id>.json`. The base directories default to `~/.config` and `~/.local/state`. Override the config with `TUYA_BEACON_MESH_P2_CONTROLLER_CONFIG` or `-config`, and the state file with `controller.state` or `-state`. [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir/latest/)

**Upgrading:** Stop any previously installed system service or manually started controller before migrating. Run `./install.sh --migrate` to install and move existing configs and snapshots from the project directory, preserving sequence numbers. The installer stops its own service first. A plain `./install.sh` skips migration. For a snapshot created under `sudo`, it may prompt for permission to read the old file. The migrated copy belongs to the login user with mode `0600`. Conflicting destination files require manual reconciliation. To move only the data, stop the user service too and run `./install.sh --migrate-only`. If you have an explicit relative `controller.state`, set it to the absolute path of your existing snapshot before restarting.

### Other key recovery options

Use `learn capture.cfa` for an Android BTSnoop capture. Both learning modes back up the config when the beacon key changes. Use `-learn-out beacon-key.json` for an additional private export or `-learn-save=false` to skip the config update. Repeat recovery after pairing the lamp again. For the local key, Tuya OpenAPI users can use `-cloud-save cloud local-key` with `TUYA_CLIENT_ID` and `TUYA_CLIENT_SECRET`.

## Usage

### CLI

Place global flags before the command. A dry run prints the frames without transmitting or reserving sequence numbers.

```sh
tuya-beacon-mesh-p2-controller -dry-run on
tuya-beacon-mesh-p2-controller scene list
sudo -v
tuya-beacon-mesh-p2-controller on
tuya-beacon-mesh-p2-controller white 4000 80
tuya-beacon-mesh-p2-controller rgb 100 232 111
tuya-beacon-mesh-p2-controller scene 8 --speed 50 --brightness 80
tuya-beacon-mesh-p2-controller state show
```

Use `monitor` to observe phone commands and advance the local sequence, or `seq sync-log capture.cfa` to synchronize from a recording. Run `tuya-beacon-mesh-p2-controller -h` for all commands, including raw datapoints and sequence management. [Protocol and frame format](SPEC.md)

### MQTT and Home Assistant

Set `mqtt.enabled` to `true`, configure `mqtt.broker` and credentials, then run `tuya-beacon-mesh-p2-controller-server`. The CLI also supports `tuya-beacon-mesh-p2-controller mqtt`. Run one bridge per MQTT client ID. For existing automations, retain the original `mqtt.topic_prefix` and client ID.

With `mqtt.ha_discovery_enabled` enabled, Home Assistant discovers the lamp and bridge automatically. Device topics use `<mqtt.topic_prefix>/<device_id>` as their prefix.

| Topic suffix | Example payload or purpose |
| --- | --- |
| `/set` | `white 4000 80` |
| `/ha/set` | `{"state":"ON","color":{"r":100,"g":232,"b":111}}` |
| `/effect/speed/set` | `50` (1–100%) |
| `/ha/state` | Last known lamp state |

Bridge availability is published at `<mqtt.topic_prefix>/bridge/state`. The lamp protocol is one-way. Published lamp state reflects commands sent or observed locally.

## Running with systemd

In my setup, the MQTT broker and BLEDOM controller run on the home server. Its CSR USB adapter (`0a12:0001`) maintains a BLE connection to the BLEDOM strip and cannot start the Tuya controller’s connectable advertisements while that connection is active. The server also has poor Bluetooth range to the lamp.

I run the Tuya controller on my desktop PC with the user service below. Its Realtek adapter (`2b89:8761`) can send the advertisements via `raw-hci` while my Bluetooth headphones are connected. The controller connects to the same broker over the LAN, so Home Assistant can control the lamp while the PC is awake.

The service user needs read access to `config.json` and write access to the state directory. If you use `mqtt.password_env`, supply that variable through an `EnvironmentFile=` in the service configuration.

### User service

Run `./install.sh` as your login user after downloading or building the binaries. It installs the user unit, uses `sudo setcap` to grant the server binary `CAP_NET_RAW`, and reads the XDG config. With an existing config it enables and restarts the service. Run the same installer after rebuilding to update the binaries and reapply the capability. [User service template](systemd/user/tuya-beacon-mesh-p2-controller.service)

After creating your config on a fresh install:

```sh
systemctl --user enable --now tuya-beacon-mesh-p2-controller.service
journalctl --user -u tuya-beacon-mesh-p2-controller.service -f
```

The user service starts at login. To start it at boot and keep it running after logout, use `loginctl enable-linger "$(id -un)"`.

Check the journal for `MQTT connected` with `transport=raw-hci`. Send a command from Home Assistant and look for `Raw HCI: active frame=`, then check the lamp itself.

## Versioning

Releases use `MAJOR.MINOR.PATCH` tags (for example, `1.0.0`). Bump the major version for incompatible CLI, configuration, or MQTT changes, minor for compatible features, and patch for fixes. Tag the tested release commit before pushing. Both binaries and the MQTT bridge report the tag, commit distance, and abbreviated hash for tagged history, or the abbreviated commit hash for untagged history.

## Acknowledgments

Thanks to [oLucasAguilar](https://github.com/oLucasAguilar), whose work taught me that the lamp protocol uses `beacon_key` as a 16-byte XOR mask applied after XXTEA encryption. I also used the project as a reference for the 20 ms BLE advertising interval and device discovery during Smart Life login. [tuya-beacon-linux](https://github.com/oLucasAguilar/tuya-beacon-linux)

## License

Copyright (c) 2026 AzaOne. Licensed under the [MIT License](./LICENSE).
