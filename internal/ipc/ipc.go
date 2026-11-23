// Package ipc carries lamp commands between the CLI and the local server.
package ipc

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Linux abstract sockets disappear when the server exits, including after a crash.
// Include both storage paths so different lamp configurations stay independent.
func Address(config, state string) (string, error) {
	config, err := filepath.Abs(config)
	if err != nil {
		return "", err
	}
	state, err = filepath.Abs(state)
	if err != nil {
		return "", err
	}
	key := sha256.Sum256([]byte(config + "\x00" + state))
	return fmt.Sprintf("@tuya-beacon-mesh-p2-%d-%x", os.Geteuid(), key[:16]), nil
}

type Reply struct {
	Output   string
	Warnings string
	Error    string
}

// Call returns false only when no server is listening. After connecting, a failed
// request must never be retried via Bluetooth: the lamp may already have acted.
func Call(ctx context.Context, addr string, args []string) (Reply, bool, error) {
	var reply Reply
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", addr)
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, os.ErrNotExist) {
		return reply, false, nil
	}
	if err != nil {
		return reply, true, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if err := checkPeer(conn.(*net.UnixConn)); err != nil {
		return reply, true, err
	}
	if err := json.NewEncoder(conn).Encode(args); err != nil {
		return reply, true, err
	}
	err = json.NewDecoder(conn).Decode(&reply)
	return reply, true, err
}

func Serve(ctx context.Context, listener *net.UnixListener, run func(context.Context, []string) Reply) error {
	stop := context.AfterFunc(ctx, func() { listener.Close() })
	defer stop()
	defer listener.Close()
	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		// Serialize requests. The controller's transmit lock also coordinates MQTT.
		func() {
			defer conn.Close()
			if checkPeer(conn) != nil {
				return
			}
			conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			var args []string
			var reply Reply
			if err := json.NewDecoder(io.LimitReader(conn, 65536)).Decode(&args); err != nil {
				reply.Error = err.Error()
			} else {
				reply = run(ctx, args)
			}
			conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
			json.NewEncoder(conn).Encode(reply)
		}()
	}
}
