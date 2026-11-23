#!/usr/bin/env bash
set -euo pipefail

usage() {
    echo "Usage: ./install.sh [--user] [--migrate | --migrate-only]"
    echo "Install the CLI and raw-hci user service."
    echo "  --migrate       Move legacy config and state into XDG directories during installation."
    echo "  --migrate-only  Move the data and exit."
}

case "${1:-}" in
    -h|--help) usage; exit 0 ;;
esac
if [[ ${1:-} == --user ]]; then
    shift
fi
if [[ $# -gt 1 || ( $# == 1 && $1 != --migrate && $1 != --migrate-only ) ]]; then
    usage >&2
    exit 2
fi
if (( EUID == 0 )); then
    echo "Run as the service user. The installer invokes sudo for setcap." >&2
    exit 1
fi

cd -- "$(dirname -- "$0")"
case "$(uname -sm)" in
    'Linux x86_64') arch=amd64 ;;
    'Linux aarch64'|'Linux arm64') arch=arm64 ;;
    *) echo "Supported platforms: Linux amd64 and arm64" >&2; exit 1 ;;
esac

name=tuya-beacon-mesh-p2-controller
bindir="$HOME/.local/bin"
config_home=${XDG_CONFIG_HOME:-}
state_home=${XDG_STATE_HOME:-}
[[ $config_home == /* ]] || config_home="$HOME/.config"
[[ $state_home == /* ]] || state_home="$HOME/.local/state"
unitdir="$config_home/systemd/user"
configdir="$config_home/$name"
statedir="$state_home/$name"

# Publish each complete file without overwriting an existing XDG copy.
publish_file() (
    local source=$1 target=$2 stagefile
    [[ -e $source ]] || return 0
    [[ -f $source && ! -L $source ]] || { echo "Expected a regular file: $source" >&2; return 1; }
    [[ ! $source -ef $target ]] || return 0
    stagefile=$(mktemp -- "${target%/*}/.migrate.XXXXXXXX")
    trap 'rm -f -- "$stagefile"' EXIT
    if [[ -r $source ]]; then
        cat -- "$source" > "$stagefile"
    else
        sudo cat -- "$source" > "$stagefile"
    fi
    if ! ln -T -- "$stagefile" "$target" 2>/dev/null; then
        if [[ -L $target ]] || ! cmp -s -- "$stagefile" "$target"; then
            echo "Cannot migrate $source to $target. Resolve the destination conflict before starting the service." >&2
            return 1
        fi
    fi
)

migrate_data() {
    local snapshot i
    local -a sources=() targets=()
    install -d -m 0700 "$configdir" "$statedir"
    for snapshot in "$PWD"/state/state-*.json "$PWD"/state-*.json; do
        [[ -f $snapshot ]] || continue
        sources+=("$snapshot")
        targets+=("$statedir/${snapshot##*/}")
    done
    if [[ -f config.json ]]; then
        sources+=("$PWD/config.json")
        targets+=("$configdir/config.json")
    fi
    # Keep every original until all snapshots and configs have been published.
    for i in "${!sources[@]}"; do
        publish_file "${sources[i]}" "${targets[i]}"
    done
    for i in "${!sources[@]}"; do
        [[ -e ${sources[i]} && ! ${sources[i]} -ef ${targets[i]} ]] || continue
        rm -- "${sources[i]}"
        echo "Migrated ${sources[i]} to ${targets[i]}"
    done
}

if [[ ${1:-} == --migrate-only ]]; then
    if systemctl --user is-active --quiet "$name.service"; then
        echo "Stop $name.service before migrating its files." >&2
        exit 1
    fi
    migrate_data
    exit 0
fi
for bin in "$name" "$name-server"; do
    if [[ ! -f "build/$bin-linux-$arch" ]]; then
        echo "Missing build/$bin-linux-$arch. Run ./build.sh first." >&2
        exit 1
    fi
done

mkdir -p -- "$bindir" "$unitdir"
stage=$(mktemp -d -- "$bindir/.tuya-install.XXXXXXXX")
trap 'rm -rf -- "$stage"' EXIT
install -m 0755 "build/$name-linux-$arch" "$stage/$name"
install -m 0700 "build/$name-server-linux-$arch" "$stage/$name-server"
sudo setcap cap_net_raw=ep "$stage/$name-server"
if systemctl --user is-active --quiet "$name.service"; then
    systemctl --user stop "$name.service"
fi
if [[ ${1:-} == --migrate ]]; then
    migrate_data
fi
mv -fT -- "$stage/$name" "$bindir/$name"
mv -fT -- "$stage/$name-server" "$bindir/$name-server"

sed -e 's|@BINDIR@|%h/.local/bin|g' \
    "systemd/user/$name.service" > "$stage/$name.service"
install -m 0644 "$stage/$name.service" "$unitdir/$name.service"
systemctl --user daemon-reload
if [[ -f "$configdir/config.json" ]]; then
    systemctl --user enable "$name.service"
    systemctl --user restart "$name.service"
else
    echo "Installed. Configure $configdir/config.json with raw-hci, then run:"
    echo "  systemctl --user enable --now $name.service"
fi
