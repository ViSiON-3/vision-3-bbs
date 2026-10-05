package config

import (
	"net"
	"testing"
)

func TestV3NetHubListenAddr(t *testing.T) {
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
			c := V3NetHubConfig{Host: tt.host, Port: 2323}
			if got := c.ListenAddr(); got != tt.want {
				t.Fatalf("ListenAddr = %q, want %q", got, tt.want)
			}
		})
	}
	c := V3NetHubConfig{}
	if got := c.ListenAddr(); got != ":8765" {
		t.Fatalf("default address = %q", got)
	}
}
func TestV3NetHubIPv6Listen(t *testing.T) {
	probe, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	for _, host := range []string{"::1", "[::1]"} {
		c := V3NetHubConfig{Host: host, Port: port}
		ln, err := net.Listen("tcp", c.ListenAddr())
		if err != nil {
			t.Fatalf("listen %q: %v", c.ListenAddr(), err)
		}
		ln.Close()
	}
}
