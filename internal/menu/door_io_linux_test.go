//go:build linux

package menu

import (
	"syscall"
	"testing"
)

// Both ends of a SOCKET I/O door's pair must be close-on-exec, so a door
// started on another node never inherits this caller's session; the door's
// own end reaches it only as ExtraFiles fd 3.
func TestDoorSocketpairIsCloseOnExec(t *testing.T) {
	fds, err := doorSocketpair()
	if err != nil {
		t.Fatalf("doorSocketpair: %v", err)
	}
	for _, fd := range fds {
		defer func() { _ = syscall.Close(fd) }()
		flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_GETFD, 0)
		if errno != 0 {
			t.Fatalf("fcntl(%d, F_GETFD): %v", fd, errno)
		}
		if flags&syscall.FD_CLOEXEC == 0 {
			t.Errorf("fd %d is inheritable by every program the BBS starts", fd)
		}
	}
}
