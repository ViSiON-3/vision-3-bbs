package telnetserver

import (
	"net"
	"testing"
	"time"
)

func TestTelnetIPv6Listen(t *testing.T) {
	probe, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	for _, host := range []string{"::1", "[::1]"} {
		t.Run(host, func(t *testing.T) {
			srv, err := NewServer(Config{Host: host, Port: port, SessionHandler: func(*TelnetSessionAdapter) {}})
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- srv.ListenAndServe() }()
			defer srv.Close()
			deadline := time.Now().Add(5 * time.Second)
			for {
				select {
				case err := <-done:
					t.Fatalf("server exited before listening: %v", err)
				default:
				}
				srv.mu.Lock()
				ln := srv.listener
				srv.mu.Unlock()
				if ln != nil {
					if got := ln.Addr().(*net.TCPAddr).IP; !got.Equal(net.IPv6loopback) {
						t.Errorf("bound IP = %v", got)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("timed out waiting for listener")
				}
				time.Sleep(time.Millisecond)
			}
			if err := srv.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("server did not stop")
			}
		})
	}
}
