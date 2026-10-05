package sshserver

import (
	"net"
	"testing"
)

func TestNewServerListenAddress(t *testing.T) {
	key := writeTestHostKey(t)
	for _, tt := range []struct{ host, want string }{
		{"127.0.0.1", "127.0.0.1:2323"},
		{"bbs.example.com", "bbs.example.com:2323"},
		{"", ":2323"},
		{"::", "[::]:2323"},
		{"::1", "[::1]:2323"},
		{"2001:db8::10", "[2001:db8::10]:2323"},
		{"[::1]", "[::1]:2323"},
		{"fe80::1%eth0", "[fe80::1%eth0]:2323"},
		{"[fe80::1%eth0]", "[fe80::1%eth0]:2323"},
	} {
		t.Run(tt.host, func(t *testing.T) {
			srv, err := NewServer(Config{HostKeyPath: key, Host: tt.host, Port: 2323})
			if err != nil {
				t.Fatal(err)
			}
			if srv.inner.Addr != tt.want {
				t.Fatalf("Addr = %q, want %q", srv.inner.Addr, tt.want)
			}
		})
	}
}
func TestSSHIPv6Listen(t *testing.T) {
	probe, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	probe.Close()
	for _, host := range []string{"::1", "[::1]"} {
		srv, err := NewServer(Config{HostKeyPath: writeTestHostKey(t), Host: host})
		if err != nil {
			t.Fatal(err)
		}
		ln, err := net.Listen("tcp", srv.inner.Addr)
		if err != nil {
			t.Fatalf("listen %q: %v", srv.inner.Addr, err)
		}
		ln.Close()
	}
}
