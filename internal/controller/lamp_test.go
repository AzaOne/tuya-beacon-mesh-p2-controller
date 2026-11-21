package controller

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
	"example.com/tuya-beacon-mesh-p2-controller/internal/testutil"
)

type sendFunc func(context.Context, []p2.WireFrame) error

func (f sendFunc) Send(ctx context.Context, frames []p2.WireFrame) error { return f(ctx, frames) }

func (lamp Controller) executeWith(ctx context.Context, args []string, send sendFunc) (Result, error) {
	lamp.Sender = send
	return lamp.Execute(ctx, args)
}

func testLamp(path string) Controller {
	k1, k2 := testutil.SampleKeys()
	return Controller{Options: Options{Frames: 1, Src: 1, Dst: 2, FlagByte: 1}, DeviceID: "test", Fingerprint: "test-fp", StatePath: path, LocalKey: k1, BeaconKey: k2}
}

// A child owns the TX lock until its stdin is closed. This synchronizes the
// test with an actual transmission in another OS process without Bluetooth.
func TestConcurrentSenders(t *testing.T) {
	if path := os.Getenv("P2_TEST_CHILD_STATE"); path != "" {
		lamp := testLamp(path)
		_, err := lamp.executeWith(context.Background(), []string{"on"}, func(ctx context.Context, frames []p2.WireFrame) error {
			fmt.Fprintln(os.Stdout, frames[0].Seq)
			_, err := io.Copy(io.Discard, os.Stdin)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	lamp := testLamp(filepath.Join(t.TempDir(), "nested", "state.json"))
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestConcurrentSenders$")
	cmd.Env = append(os.Environ(), "P2_TEST_CHILD_STATE="+lamp.StatePath)
	cmd.Stderr = os.Stderr
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	release, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer release.Close()
	reader := bufio.NewReader(output)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(line) != "143" {
		t.Fatalf("first transmitted seq %q", line)
	}
	// A contending sender must wait without holding the state lock.
	waiting, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	_, err = lamp.executeWith(waiting, []string{"off"}, func(context.Context, []p2.WireFrame) error {
		return errors.New("concurrent send reached transport")
	})
	stop()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting for TX lock: %v", err)
	}
	st, err := lamp.Snapshot()
	if err != nil || st.LastReserved != 143 {
		t.Fatalf("snapshot during send: %+v, %v", st, err)
	}
	sent := make(chan error, 1)
	go func() {
		_, err := lamp.executeWith(ctx, []string{"off"}, func(_ context.Context, frames []p2.WireFrame) error {
			if frames[0].Seq != 144 {
				return fmt.Errorf("second transmitted seq %d", frames[0].Seq)
			}
			return nil
		})
		sent <- err
	}()
	if err := release.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	st, err = lamp.Snapshot()
	if err != nil || st.LastReserved != 144 || st.Light == nil || st.Light.Power == nil || *st.Light.Power {
		t.Fatalf("final snapshot %+v, %v", st, err)
	}
}

func TestTransmissionFailureConsumesReservation(t *testing.T) {
	lamp := testLamp(filepath.Join(t.TempDir(), "state.json"))
	failure := errors.New("transmission failed")
	send := func(context.Context, []p2.WireFrame) error { return failure }
	first, err := lamp.executeWith(context.Background(), []string{"on"}, send)
	if !errors.Is(err, failure) {
		t.Fatalf("send error %v", err)
	}
	st, err := lamp.Snapshot()
	if err != nil || st.LastReserved != uint32(first.Frames[0].Seq) || st.Light != nil {
		t.Fatalf("state after send error %+v, %v", st, err)
	}
	next, err := lamp.executeWith(context.Background(), []string{"on"}, func(context.Context, []p2.WireFrame) error { return nil })
	if err != nil || next.Frames[0].Seq != first.Frames[0].Seq+1 {
		t.Fatalf("next transmission %+v, %v", next, err)
	}
}
