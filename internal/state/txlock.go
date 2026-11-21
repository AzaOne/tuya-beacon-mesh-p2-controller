package state

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Serializes transmitters independently from the brief seq/state lock.
// A transmitter obtains TX lock before reserving its seq numbers, then holds
// TX lock until all advertising has finished, preventing out-of-order sends.
func OpenTxLock(ctx context.Context, statePath string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(statePath), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(statePath+".tx.lock", os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}

}

func CloseTxLock(f *os.File) {
	if f != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}
}
