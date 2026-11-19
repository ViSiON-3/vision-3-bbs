package telnetserver

import (
	"net"
	"testing"
)

// The goroutines a telnet connection starts name its session adapter, which
// the BBS maps to the caller's node, once the adapter exists.
func TestTelnetConnOwnerIsItsSession(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	tc := NewTelnetConn(a)
	if got := tc.sessionOwner(); got != any(tc) {
		t.Errorf("owner before the adapter = %v, want the connection", got)
	}
	adapter := NewTelnetSessionAdapter(tc)
	defer adapter.Close() // closes tc.winCh, so the window forwarder returns
	if got := tc.sessionOwner(); got != any(adapter) {
		t.Errorf("owner = %v, want the session adapter", got)
	}
}
