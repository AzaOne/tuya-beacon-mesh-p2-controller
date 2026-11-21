package mqtt

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionExecutesCommandAndPublishesResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	conn, broker := net.Pipe()
	defer broker.Close()
	_ = broker.SetDeadline(time.Now().Add(4 * time.Second))
	opt := Options{Prefix: "test/lamp", DeviceID: "lamp", Fingerprint: "fp", StatePath: filepath.Join(t.TempDir(), "state.json")}
	done := make(chan error, 1)
	go func() {
		done <- serveSession(ctx, conn, "test", opt, func(_ context.Context, args []string) error {
			if strings.Join(args, " ") != "on" {
				return fmt.Errorf("unexpected command %q", args)
			}
			return nil
		})
	}()
	reader := bufio.NewReader(broker)
	kind, _, err := readPacket(reader)
	if err != nil || kind != 0x10 {
		t.Fatalf("CONNECT %x, %v", kind, err)
	}
	if err := writePacket(broker, 0x20, []byte{0, 0}); err != nil {
		t.Fatal(err)
	}
	kind, _, err = readPacket(reader)
	if err != nil || kind != 0x82 {
		t.Fatalf("SUBSCRIBE %x, %v", kind, err)
	}
	published := make(chan error, 1)
	go func() { published <- publish(broker, opt.Prefix+"/power/set", []byte("ON"), false, 0, 0) }()
	for {
		_, packet, err := readPacket(reader)
		if err != nil {
			t.Fatal(err)
		}
		body, ok := bytes.CutPrefix(packet, encodeString(opt.Prefix+"/result"))
		if !ok {
			continue
		}
		var result struct {
			OK             bool
			Topic, Command string
		}
		if err := json.Unmarshal(body, &result); err != nil || !result.OK || result.Topic != "test/lamp/power/set" || result.Command != "ON" {
			t.Fatalf("result %s, %v", body, err)
		}
		break
	}
	if err := <-published; err != nil {
		t.Fatal(err)
	}
	cancel()
	for kind != 0xe0 {
		kind, _, err = readPacket(reader)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
