package sshserver

import (
	"testing"

	"github.com/gliderlabs/ssh"
)

func TestNewServerWiresPubkeyAndSubsystems(t *testing.T) {
	// Use the existing writeTestHostKey helper (defined in server_test.go).
	keyPath := writeTestHostKey(t)

	called := false
	srv, err := NewServer(Config{
		HostKeyPath:    keyPath,
		Host:           "127.0.0.1",
		Port:           0,
		SessionHandler: func(ssh.Session) {},
		PublicKeyHandler: func(ssh.Context, ssh.PublicKey) bool {
			called = true
			return true
		},
		SubsystemHandlers: map[string]func(ssh.Session){
			"wfc-admin": func(ssh.Session) {},
		},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if srv.inner.PublicKeyHandler == nil {
		t.Fatal("PublicKeyHandler not wired")
	}
	if _, ok := srv.inner.SubsystemHandlers["wfc-admin"]; !ok {
		t.Fatal("wfc-admin subsystem not wired")
	}
	_ = called
}
