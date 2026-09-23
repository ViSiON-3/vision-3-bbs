package menu

import (
	"net"
	"testing"

	"github.com/gliderlabs/ssh"
)

// remoteAddrSession is an ssh.Session that only answers RemoteAddr. Every
// other method panics on the nil embedded interface, which is what we want:
// doorUserIP must touch nothing else.
type remoteAddrSession struct {
	ssh.Session
	addr net.Addr
}

func (s remoteAddrSession) RemoteAddr() net.Addr { return s.addr }

func TestDoorUserIP(t *testing.T) {
	tcp := func(addr string) net.Addr {
		a, err := net.ResolveTCPAddr("tcp", addr)
		if err != nil {
			t.Fatalf("ResolveTCPAddr(%q): %v", addr, err)
		}
		return a
	}

	tests := []struct {
		name string
		sess ssh.Session
		want string
	}{
		{"nil session", nil, ""},
		{"nil remote addr", remoteAddrSession{}, ""},
		{"ipv4 with port", remoteAddrSession{addr: tcp("203.0.113.7:51234")}, "203.0.113.7"},
		{"ipv6 with port", remoteAddrSession{addr: tcp("[2001:db8::1]:22")}, "2001:db8::1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := doorUserIP(tc.sess); got != tc.want {
				t.Errorf("doorUserIP() = %q, want %q", got, tc.want)
			}
		})
	}
}
