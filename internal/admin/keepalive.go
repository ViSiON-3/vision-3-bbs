package admin

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Default SSH keepalive cadence for admin links. A silent peer is declared
// dead after interval+timeout, roughly 25 seconds, which is quick enough to
// notice a laptop lid closing or a NAT table expiring without adding
// meaningful traffic.
const (
	DefaultKeepAliveInterval = 15 * time.Second
	DefaultKeepAliveTimeout  = 10 * time.Second
)

// ErrKeepAliveTimeout reports that the peer stopped answering keepalives.
var ErrKeepAliveTimeout = errors.New("admin: keepalive timed out")

// globalRequester is the subset of ssh.Conn needed to send a keepalive.
// Both *ssh.Client and *ssh.ServerConn satisfy it.
type globalRequester interface {
	SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error)
}

// keepAlive sends an OpenSSH-style keepalive global request every interval
// and calls onDead once if the peer fails to answer within timeout or the
// transport reports an error. It returns when stop is closed or after onDead.
//
// A rejected request ("false" reply) still proves the peer is alive; only a
// transport error or a missing reply counts as death. Because SendRequest on
// a half-dead TCP connection can block for the kernel's retransmit budget
// (minutes), the wait is bounded by a timer rather than trusting the call to
// return.
func keepAlive(stop <-chan struct{}, conn globalRequester, interval, timeout time.Duration, onDead func(error)) {
	if interval <= 0 {
		interval = DefaultKeepAliveInterval
	}
	if timeout <= 0 {
		timeout = DefaultKeepAliveTimeout
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		errc := make(chan error, 1)
		go func() {
			_, _, err := conn.SendRequest("keepalive@openssh.com", true, nil)
			errc <- err
		}()
		timer := time.NewTimer(timeout)
		select {
		case <-stop:
			timer.Stop()
			return
		case err := <-errc:
			timer.Stop()
			if err != nil {
				onDead(fmt.Errorf("admin: keepalive: %w", err))
				return
			}
		case <-timer.C:
			onDead(ErrKeepAliveTimeout)
			return
		}
	}
}

// KeepAlive runs keepAlive in the background against conn and returns a stop
// function. It is exported for the daemon side, which needs to detect a
// vanished console so its admin session goroutines do not linger for the
// kernel's TCP timeout. onDead should close the connection.
func KeepAlive(conn globalRequester, interval, timeout time.Duration, onDead func(error)) (stop func()) {
	stopCh := make(chan struct{})
	go keepAlive(stopCh, conn, interval, timeout, onDead)
	var once sync.Once
	return func() { once.Do(func() { close(stopCh) }) }
}
