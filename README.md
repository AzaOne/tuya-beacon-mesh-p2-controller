# Tuya Beacon Mesh P2 Controller

Control a Tuya Beacon Mesh P2 lamp from Linux over Bluetooth Low Energy. The CLI sends encrypted advertisements directly to the lamp. The MQTT server exposes the same controls to Home Assistant. Once the lamp keys are configured, both work locally.

## Features

- Control power, brightness, white temperature, RGB and HSV colors, and 28 built-in scenes with adjustable speed and brightness.
- Use Home Assistant MQTT Discovery for the light, effect speed control, and bridge diagnostics.
- Monitor commands from Smart Life, synchronize sequence numbers, and recover the beacon key from live traffic or an Android BTSnoop capture.

## Getting Started

You need Linux, a Bluetooth adapter that supports LE advertising, and a paired P2 lamp. Building from source requires Go 1.23+ and a C compiler for the race tests. The server also needs an MQTT broker.

### Build and install

Run from the repository root on the target Linux machine. The build script checks formatting, runs vet and race tests, and produces CLI and server binaries for both amd64 and arm64.

```sh
./build.sh
./install.sh
```

Releases use `MAJOR.MINOR.PATCH` tags (for example, `1.0.0`). Bump the major version for incompatible CLI, configuration, or MQTT changes, minor for compatible features, and patch for fixes. Tag the tested release commit before pushing. Untagged history builds with the abbreviated commit hash. Both binaries accept `--version` to print the Git build identifier (tag, commit distance, and abbreviated hash for tagged history). MQTT bridge metadata uses the same identifier.

The installer puts both binaries in `~/.local/bin`. Include that directory in your `PATH` to run the commands below.

### Configure the lamp

For a new installation, copy the example into the XDG configuration directory:

```sh
configdir="${XDG_CONFIG_HOME:-$HOME/.config}/tuya-beacon-mesh-p2-controller"
install -d -m 0700 "$configdir"
test -e "$configdir/config.json" || install -m 0600 config.example.json "$configdir/config.json"
```

Set `device.device_id`, `device.local_key`, and `device.beacon_key` for your lamp. Check the mesh addresses in `controller.src`, `phone_src`, and `dst`. The example selects `raw-hci` for the user service. Set `mqtt.broker` if your broker runs on another host (default `tcp://localhost:1883`) and select your Bluetooth adapter in `controller.adapter`. The other supported transports are `mgmt` (default), `mgmt-legacy`, and `btmgmt`. [Full configuration example](config.example.json)

When the working directory contains both `config.json` and a `state/` directory, both binaries use those local files. Otherwise they use `$XDG_CONFIG_HOME/tuya-beacon-mesh-p2-controller/config.json` and `$XDG_STATE_HOME/tuya-beacon-mesh-p2-controller/state-<device-id>.json`. The base directories default to `~/.config` and `~/.local/state`. Override the config with `TUYA_BEACON_MESH_P2_CONTROLLER_CONFIG` or `-config`, and the state file with `controller.state` or `-state`. [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir/latest/)

**Upgrading:** Stop any previously installed system service or manually started controller before migrating. Run `./install.sh --migrate` to install and move existing configs and snapshots from the project directory, preserving sequence numbers. The installer stops its own service first. A plain `./install.sh` skips migration. For a snapshot created under `sudo`, it may prompt for permission to read the old file. The migrated copy belongs to the login user with mode `0600`. Conflicting destination files require manual reconciliation. To move only the data, stop the user service too and run `./install.sh --migrate-only`. If you have an explicit relative `controller.state`, set it to the absolute path of your existing snapshot before restarting.

### Obtain the keys

If you already have both keys, continue to Usage. Otherwise, set `device.device_id` in the selected config and run these commands as your login user. Both commands update that config by default. Run `sudo -v` separately to authorize the Bluetooth helper.

```sh
tuya-beacon-mesh-p2-controller login
sudo -v
tuya-beacon-mesh-p2-controller learn live
```

1. For `login`, get your User Code from **Smart Life → Me → Settings → Account and Security**. Scan the displayed QR code with the app and confirm. The CLI saves the local key to the config. Install `qrencode` to display the QR in the terminal. Tuya OpenAPI credentials can also be used with `-cloud-save cloud local-key` and the environment variables `TUYA_CLIENT_ID` and `TUYA_CLIENT_SECRET`.
2. During `learn live`, switch the lamp off and on in Smart Life with the phone near the adapter. Live scanning uses BlueZ's `btmgmt`. Alternatively, use `learn capture.cfa` with an Android Bluetooth capture.
3. Both learning modes save the verified beacon key to `device.beacon_key` in the selected config and back up the previous config when the key changes. Use `-learn-out beacon-key.json` for an additional private export, or `-learn-save=false` to skip the config update. Check the reported source and destination against your config. Repeat key recovery if you pair the lamp again.

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

Run `./install.sh` as your login user after building. It installs the user unit, uses `sudo setcap` to grant the server binary `CAP_NET_RAW`, and reads the XDG config. With an existing config it enables and restarts the service. Run the same installer after rebuilding to update the binaries and reapply the capability. [User service template](systemd/user/tuya-beacon-mesh-p2-controller.service)

After creating your config on a fresh install:

```sh
systemctl --user enable --now tuya-beacon-mesh-p2-controller.service
journalctl --user -u tuya-beacon-mesh-p2-controller.service -f
```

The user service starts at login. To start it at boot and keep it running after logout, use `loginctl enable-linger "$(id -un)"`.

Check the journal for `MQTT connected` with `transport=raw-hci`. Send a command from Home Assistant and look for `Raw HCI: active frame=`, then check the lamp itself.

## Acknowledgments

Thanks to [oLucasAguilar](https://github.com/oLucasAguilar), whose work taught me that the lamp protocol uses `beacon_key` as a 16-byte XOR mask applied after XXTEA encryption. I also used the project as a reference for the 20 ms BLE advertising interval and device discovery during Smart Life login. [tuya-beacon-linux](https://github.com/oLucasAguilar/tuya-beacon-linux)

## License

Copyright (c) 2026 AzaOne. Licensed under the [MIT License](./LICENSE).
