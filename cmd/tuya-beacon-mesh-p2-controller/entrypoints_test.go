package main

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Exercise actual flag parsing, XDG defaults and signal handling.
func TestEntrypoints(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-o", dir+string(os.PathSeparator), "./tuya-beacon-mesh-p2-controller", "./tuya-beacon-mesh-p2-controller-server")
	build.Dir = ".."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	cli, server := filepath.Join(dir, "tuya-beacon-mesh-p2-controller"), filepath.Join(dir, "tuya-beacon-mesh-p2-controller-server")
	workDir := t.TempDir()
	configHome := t.TempDir()
	appName := "tuya-beacon-mesh-p2-controller"
	cfg := filepath.Join(configHome, appName, "config.json")
	if err := os.Mkdir(filepath.Dir(cfg), 0700); err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	check := func(bin string, want string, success bool, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Dir = workDir
		out, err := cmd.CombinedOutput()
		if (err == nil) != success || !strings.Contains(string(out), want) {
			t.Fatalf("%s %v: %v\n%s", filepath.Base(bin), args, err, out)
		}
	}
	t.Setenv("TUYA_BEACON_MESH_P2_CONTROLLER_CONFIG", "")
	t.Setenv("TUYA_P2_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	check(server, "open "+cfg+":", false)
	write(filepath.Join(dir, "tuya-lamp-re-verified.json"), "{}")
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "legacy"))
	write(cfg, "{")
	if runtime.GOOS == "linux" {
		for _, bin := range []string{cli, server} {
			check(bin, "invalid HCI index", false, "__hci-monitor", "invalid")
			check(bin, "bad dwell ms", false, "__mgmt-send", "0", "0", "0", "1", "32")
			check(bin, "invalid RAW HCI helper parameters", false, "__raw-hci-send", "0", "0", "0", "1", "32")
		}
	}
	check(server, "settings", false)
	write(cfg, `{"mqtt":{"enabled":false}}`)
	check(server, "device.local_key", false)
	for _, bin := range []string{cli, server} {
		check(bin, "Usage:", true, "-h")
		check(bin, "--config", true, "--help")
		check(bin, "use --help", false, "-help")
		check(bin, "use -h", false, "--h")
		check(bin, "use --version", false, "-version")
		check(bin, "use --config", false, "-config", cfg)
	}
	check(cli, "27  RGB white jump\n", true, "scene", "list")
	check(cli, "00000000640a40", true, "scene", "list", "--verbose")
	write(cfg, `{"device":{"device_id":"lamp","local_key":"abcdefghijklmnop","beacon_key":"pqrstuvwxyz12345"},"mqtt":{"broker":"tcp://127.0.0.1:0"}}`)
	check(cli, "last=0x008E", true, "seq", "show")
	check(cli, "seq=0x008F", true, "--dry-run", "on")
	check(cli, "last=0x008E", true, "seq", "show")
	// Local config and state take precedence together, for both entrypoints.
	localCfg := filepath.Join(workDir, "config.json")
	write(localCfg, `{"controller":{"dry_run":true},"mqtt":{"enabled":false},"device":{"device_id":"portable","local_key":"abcdefghijklmnop","beacon_key":"pqrstuvwxyz12345"}}`)
	check(cli, "state-lamp.json", true, "seq", "show")
	if err := os.Mkdir(filepath.Join(workDir, "state"), 0700); err != nil {
		t.Fatal(err)
	}
	// Run a separate server with MQTT disabled and a dry-run transmitter.
	localServer := exec.CommandContext(ctx, server)
	localServer.Dir = workDir
	log, err := localServer.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := localServer.Start(); err != nil {
		t.Fatal(err)
	}
	defer localServer.Process.Kill()
	if line, err := bufio.NewReader(log).ReadString('\n'); err != nil || !strings.Contains(line, "Listening for local CLI commands") {
		t.Fatalf("local server: %q, %v", line, err)
	}
	check(server, "address already in use", false)
	check(cli, "Sent via local server", true, "on")
	check(cli, "unknown", false, "unknown-command")
	if err := localServer.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := localServer.Wait(); err != nil {
		t.Fatal(err)
	}
	check(cli, "last=0x009F", true, "seq", "sync", "0x9f")
	if _, err := os.Stat(filepath.Join(workDir, "state", "state-portable.json")); err != nil {
		t.Fatal(err)
	}
	check(cli, "last=0x008E", true, "--config", cfg, "--state", filepath.Join(os.Getenv("XDG_STATE_HOME"), appName, "state-lamp.json"), "seq", "show")
	if err := os.Remove(localCfg); err != nil {
		t.Fatal(err)
	}
	check(cli, "state-lamp.json", true, "seq", "show")
	check(cli, "last=0x00A0", true, "seq", "sync", "0xa0")
	check(cli, `"last_reserved": 160`, true, "state", "show")
	invalid := filepath.Join(dir, "invalid.json")
	write(invalid, "{")
	check(server, "state file malformed", false, "--state", invalid)
	write(invalid, `{"version":1,"device":"other"}`)
	check(server, "different key/device", false, "--state", invalid)
	for _, entry := range []struct {
		bin  string
		args []string
	}{{server, nil}, {cli, []string{"mqtt"}}} {
		cmd := exec.CommandContext(ctx, entry.bin, entry.args...)
		cmd.Dir = workDir
		stderr, err := cmd.StderrPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		reader := bufio.NewReader(stderr)
		if entry.bin == server {
			if _, err := reader.ReadString('\n'); err != nil {
				t.Fatal(err)
			}
		}
		line, err := reader.ReadString('\n')
		if err != nil || !strings.Contains(line, "MQTT disconnected") {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatalf("broker retry: %q, %v", line, err)
		}
		check(cli, "last=0x00A0", true, "seq", "show")
		if err = cmd.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
		if err = cmd.Wait(); err != nil {
			t.Fatalf("SIGTERM: %v", err)
		}
	}
}
