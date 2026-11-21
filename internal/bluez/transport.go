package bluez

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"example.com/tuya-beacon-mesh-p2-controller/internal/p2"
)

type transmitter struct {
	Adapter  string
	Duration time.Duration
	Gap      time.Duration
	Instance int
	ClearAll bool
}

func mgmtCommand(ctx context.Context, adapter string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "sudo", append([]string{"-n", "btmgmt", "-i", adapter}, args...)...)
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	_ = w.Close()
	defer r.Close()
	cmd.Stdin = r
	result, err := cmd.CombinedOutput()
	return string(result), err
}

func (tx transmitter) remove() error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	args := []string{"rm-adv", strconv.Itoa(tx.Instance)}
	if tx.ClearAll {
		args = []string{"clr-adv"}
	}
	_, err := mgmtCommand(ctx, tx.Adapter, args...)
	return err
}

func (tx transmitter) sendOne(ctx context.Context, frame p2.WireFrame) error {
	// -c and -d follow the previously working Tuya P2 sender using btmgmt.
	// The caller clears OUR instance before and after the batch.

	cmdCtx, cancel := context.WithTimeout(ctx, 14*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "sudo", "-n", "btmgmt", "-i", tx.Adapter,
		"add-adv", "-c", "-d", hex.EncodeToString(frame.Adv[:]), strconv.Itoa(tx.Instance))
	r, w, err := os.Pipe()
	if err != nil {
		return err
	}
	_ = w.Close()
	defer r.Close()
	cmd.Stdin = r
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return err
	}
	timer := time.NewTimer(tx.Duration)
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
	timer.Stop()
	_ = tx.remove()
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil {
		return fmt.Errorf("btmgmt add-adv failed: %w: %s", waitErr, strings.TrimSpace(output.String()))
	}
	if !strings.Contains(strings.ToLower(output.String()), "added") {
		return fmt.Errorf("btmgmt did not confirm advertising (check sudo/permissions): %s", strings.TrimSpace(output.String()))
	}
	return nil
}

func (tx transmitter) sendAll(ctx context.Context, frames []p2.WireFrame) error {
	if _, err := exec.LookPath("sudo"); err != nil {
		return fmt.Errorf("sudo missing: %w", err)
	}
	if _, err := exec.LookPath("btmgmt"); err != nil {
		return fmt.Errorf("btmgmt missing: install bluez-utils: %w", err)
	}
	// An instance can be left behind after an interrupted older process.
	// By default ONLY our instance (1) is touched, not unrelated adverts.
	_ = tx.remove()
	defer tx.remove()
	for i, fr := range frames {
		fmt.Printf("[%d/%d] seq=0x%04X %s\n", i+1, len(frames), fr.Seq, fr.Label)
		if err := tx.sendOne(ctx, fr); err != nil {
			return fmt.Errorf("failed on seq 0x%04x: %w (reserved seqs are never reused)", fr.Seq, err)
		}
		if i+1 < len(frames) && tx.Gap > 0 {
			t := time.NewTimer(tx.Gap)
			select {
			case <-t.C:
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			}
		}
	}
	return nil
}
