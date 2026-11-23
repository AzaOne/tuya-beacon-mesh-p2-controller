//go:build !linux

package ipc

import (
	"errors"
	"net"
)

func checkPeer(*net.UnixConn) error { return errors.New("local controller IPC requires Linux") }
