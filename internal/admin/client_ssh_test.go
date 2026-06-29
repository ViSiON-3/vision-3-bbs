package admin

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"testing"
	"time"

	gliderssh "github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// genEd25519Signer returns a new ephemeral ed25519 signer.
func genEd25519Signer(t *testing.T) gossh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}
	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	return signer
}

func TestSSHChannelClient_SnapshotOverSSH(t *testing.T) {
	// Set up the admin server with a known system name.
	srv := NewServer(ServerConfig{
		Reg:        &fakeRegistry{},
		SystemName: "SSHTestNode",
		StartedAt:  time.Now(),
		MaxEvents:  8,
		CallsToday: func() int { return -1 },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Run(ctx)

	// Generate ephemeral host and client keys.
	hostSigner := genEd25519Signer(t)
	clientSigner := genEd25519Signer(t)

	// Stand up an in-memory gliderlabs/ssh server that runs ServeRPC over the
	// wfc-admin subsystem.
	gliderSrv := &gliderssh.Server{
		HostSigners: []gliderssh.Signer{hostSigner},
		// Allow any client public key (the test uses the ephemeral clientSigner).
		PublicKeyHandler: func(_ gliderssh.Context, _ gliderssh.PublicKey) bool { return true },
		SubsystemHandlers: map[string]gliderssh.SubsystemHandler{
			"wfc-admin": func(s gliderssh.Session) {
				_ = ServeRPC(ctx, s, srv, nil)
			},
		},
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = gliderSrv.Serve(ln) }()
	t.Cleanup(func() { _ = gliderSrv.Close() })

	// Dial with DialSSH using Insecure:true so no known_hosts file is needed.
	addr := ln.Addr().String()
	cfg := SSHDialConfig{
		Addr:     addr,
		User:     "sysop",
		Signer:   clientSigner,
		Insecure: true,
	}
	client, err := DialSSH(cfg)
	if err != nil {
		t.Fatalf("DialSSH: %v", err)
	}
	defer client.Close()

	// Verify the AdminClient interface is satisfied.
	var _ AdminClient = client

	// Snapshot must return the correct system name.
	snapCtx, snapCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer snapCancel()
	snap, err := client.Snapshot(snapCtx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.SystemName != "SSHTestNode" {
		t.Errorf("SystemName = %q, want %q", snap.SystemName, "SSHTestNode")
	}
}

func TestSSHDialConfig_InsecureIgnoresKnownHosts(t *testing.T) {
	// Just verify DialSSH handles Insecure without needing a KnownHostsPath.
	// This test reuses the same server setup but checks the config path.
	srv := NewServer(ServerConfig{
		Reg:        &fakeRegistry{},
		SystemName: "InsecureTest",
		StartedAt:  time.Now(),
		MaxEvents:  4,
		CallsToday: func() int { return -1 },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Run(ctx)

	hostSigner := genEd25519Signer(t)
	clientSigner := genEd25519Signer(t)

	gliderSrv := &gliderssh.Server{
		HostSigners:      []gliderssh.Signer{hostSigner},
		PublicKeyHandler: func(_ gliderssh.Context, _ gliderssh.PublicKey) bool { return true },
		SubsystemHandlers: map[string]gliderssh.SubsystemHandler{
			"wfc-admin": func(s gliderssh.Session) {
				_ = ServeRPC(ctx, s, srv, nil)
			},
		},
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = gliderSrv.Serve(ln) }()
	t.Cleanup(func() { _ = gliderSrv.Close() })

	client, err := DialSSH(SSHDialConfig{
		Addr:     ln.Addr().String(),
		User:     "sysop",
		Signer:   clientSigner,
		Insecure: true,
		// KnownHostsPath intentionally empty — must be ignored when Insecure:true
	})
	if err != nil {
		t.Fatalf("DialSSH (insecure, no known_hosts): %v", err)
	}
	defer client.Close()

	snapCtx, snapCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer snapCancel()
	snap, err := client.Snapshot(snapCtx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.SystemName != "InsecureTest" {
		t.Errorf("SystemName = %q, want %q", snap.SystemName, "InsecureTest")
	}
}

// startSSHTestServer sets up a gliderlabs SSH server with the given host signer
// over the wfc-admin subsystem and returns the listener address.
func startSSHTestServer(t *testing.T, ctx context.Context, srv *Server, hostSigner gossh.Signer) string {
	t.Helper()
	gliderSrv := &gliderssh.Server{
		HostSigners:      []gliderssh.Signer{hostSigner},
		PublicKeyHandler: func(_ gliderssh.Context, _ gliderssh.PublicKey) bool { return true },
		SubsystemHandlers: map[string]gliderssh.SubsystemHandler{
			"wfc-admin": func(s gliderssh.Session) {
				_ = ServeRPC(ctx, s, srv, nil)
			},
		},
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = gliderSrv.Serve(ln) }()
	t.Cleanup(func() { _ = gliderSrv.Close() })
	return ln.Addr().String()
}

func TestSSHChannelClient_SecurePath(t *testing.T) {
	srv := NewServer(ServerConfig{
		Reg:        &fakeRegistry{},
		SystemName: "SecureNode",
		StartedAt:  time.Now(),
		MaxEvents:  8,
		CallsToday: func() int { return -1 },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Run(ctx)

	hostSigner := genEd25519Signer(t)
	clientSigner := genEd25519Signer(t)
	addr := startSSHTestServer(t, ctx, srv, hostSigner)

	// Write a proper known_hosts file for the server's host key.
	hostPubKey := hostSigner.PublicKey()
	knownHostsLine := knownhosts.Line([]string{addr}, hostPubKey)
	tmpFile, err := os.CreateTemp(t.TempDir(), "known_hosts")
	if err != nil {
		t.Fatalf("create temp known_hosts: %v", err)
	}
	if _, err := tmpFile.WriteString(knownHostsLine + "\n"); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
	tmpFile.Close()

	// Positive case: secure dial with a valid known_hosts entry must succeed.
	t.Run("valid_known_hosts", func(t *testing.T) {
		client, err := DialSSH(SSHDialConfig{
			Addr:           addr,
			User:           "sysop",
			Signer:         clientSigner,
			KnownHostsPath: tmpFile.Name(),
			Insecure:       false,
		})
		if err != nil {
			t.Fatalf("DialSSH (secure): %v", err)
		}
		defer client.Close()

		var _ AdminClient = client

		snapCtx, snapCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer snapCancel()
		snap, err := client.Snapshot(snapCtx)
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		if snap.SystemName != "SecureNode" {
			t.Errorf("SystemName = %q, want %q", snap.SystemName, "SecureNode")
		}
	})

	// Negative case: empty known_hosts must cause host key rejection.
	t.Run("empty_known_hosts_rejected", func(t *testing.T) {
		emptyFile, err := os.CreateTemp(t.TempDir(), "known_hosts_empty")
		if err != nil {
			t.Fatalf("create empty known_hosts: %v", err)
		}
		emptyFile.Close()

		_, err = DialSSH(SSHDialConfig{
			Addr:           addr,
			User:           "sysop",
			Signer:         clientSigner,
			KnownHostsPath: emptyFile.Name(),
			Insecure:       false,
		})
		if err == nil {
			t.Fatal("DialSSH with empty known_hosts: expected error, got nil")
		}
	})
}
