package util

import (
	"net"
	"testing"
)

func TestListenAddress(t *testing.T) {
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
			got := ListenAddress(tt.host, 2323)
			if got != tt.want {
				t.Fatalf("ListenAddress = %q, want %q", got, tt.want)
			}
			if _, _, err := net.SplitHostPort(got); err != nil {
				t.Fatal(err)
			}
		})
	}
}
