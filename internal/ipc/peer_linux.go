package ipc

import (
	"errors"
	"net"
	"os"
	"syscall"
)

// Abstract Unix sockets have no filesystem permissions. Authenticate the peer
// in both directions before accepting commands or disclosing arguments.
func checkPeer(conn *net.UnixConn) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var cred *syscall.Ucred
	var sockErr error
	err = raw.Control(func(fd uintptr) {
		cred, sockErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	})
	if err != nil {
		return err
	}
	if sockErr != nil {
		return sockErr
	}
	if cred.Uid != uint32(os.Geteuid()) {
		return errors.New("local controller socket belongs to a different user")
	}
	return nil
}
