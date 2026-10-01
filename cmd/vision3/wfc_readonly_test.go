package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	gossh "golang.org/x/crypto/ssh"
)

// useAdminAccount replaces the test server's users with one admin, "boss",
// whose key is signer, stored in a real users.json so the flag can be
// changed while sessions are open.
func useAdminAccount(t *testing.T, signer gossh.Signer, readOnly bool) func(bool) {
	t.Helper()
	um, err := user.NewUserManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := um.AddUser("secret123", "boss", "Boss", ""); err != nil {
		t.Fatal(err)
	}
	set := func(ro bool) {
		u, _ := um.GetUser("boss")
		u.AccessLevel = 255
		u.PublicKeys = []string{string(gossh.MarshalAuthorizedKey(signer.PublicKey()))}
		u.WFCReadOnly = ro
		if err := um.UpdateUser(u); err != nil {
			t.Fatal(err)
		}
	}
	set(readOnly)
	userMgr = um
	return set
}

// useAdminServer wires adminServer, which startWFCTestServer restores, with the production type-in and chat
// hooks and a kick hook that counts its calls.
func useAdminServer(t *testing.T) *atomic.Int32 {
	t.Helper()
	var kicks atomic.Int32
	adminServer = admin.NewServer(admin.ServerConfig{
		Reg: sessionRegistry, Refresh: 20 * time.Millisecond,
		Kick:   func(int, time.Time) error { kicks.Add(1); return nil },
		TypeIn: typeInHook(sessionRegistry),
		Chat:   chatHook(sessionRegistry),
	})
	return &kicks
}

func openAdmin(t *testing.T, c *gossh.Client) *admin.StreamClient {
	t.Helper()
	sc := admin.NewStreamClient(openSubsystem(t, c, "wfc-admin"))
	t.Cleanup(func() { _ = sc.Close() })
	return sc
}

func nodeCommands(start time.Time) []admin.AdminCommand {
	return []admin.AdminCommand{
		{Command: admin.CommandKick, NodeID: 4, ConnectedAt: start},
		{Command: admin.CommandTypeIn, NodeID: 4, ConnectedAt: start, Payload: map[string]any{"on": true}},
		{Command: admin.CommandChat, NodeID: 4, ConnectedAt: start, Payload: map[string]any{"start": true}},
	}
}

func TestWFCReadOnlyAccountIsRefused(t *testing.T) {
	signer := newTestSigner(t)
	addr, tap, start := startWFCTestServer(t, signer)
	useAdminAccount(t, signer, true)
	kicks := useAdminServer(t)
	c := dialWFC(t, addr, gossh.PublicKeys(signer))

	if _, err := snoopOpen(t, c, 4, start); err == nil || err.Error() != admin.ErrReadOnly.Error() {
		t.Fatalf("wfc-snoop err = %v, want %q", err, admin.ErrReadOnly)
	}
	if snoopsAs(tap, "boss") {
		t.Fatal("read-only account is watching the caller")
	}

	sc := openAdmin(t, c)
	ctx := context.Background()
	snap, err := sc.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.ReadOnly {
		t.Fatal("console not told it is read-only")
	}
	for _, cmd := range nodeCommands(start) {
		if _, err := sc.Execute(ctx, cmd); err == nil || err.Error() != admin.ErrReadOnly.Error() {
			t.Fatalf("%s: err = %v, want %q", cmd.Command, err, admin.ErrReadOnly)
		}
	}
	if kicks.Load() != 0 {
		t.Fatal("kick reached the hook")
	}
	if res, err := sc.Execute(ctx, admin.AdminCommand{Command: admin.CommandRefresh}); err != nil || !res.OK {
		t.Fatalf("refresh: %+v %v", res, err)
	}
}

func TestWFCNormalAccountIsNotReadOnly(t *testing.T) {
	signer := newTestSigner(t)
	addr, _, start := startWFCTestServer(t, signer)
	useAdminAccount(t, signer, false)
	kicks := useAdminServer(t)
	c := dialWFC(t, addr, gossh.PublicKeys(signer))

	st, err := snoopOpen(t, c, 4, start)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sc := openAdmin(t, c)
	ctx := context.Background()
	snap, err := sc.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snap.ReadOnly {
		t.Fatal("normal console told it is read-only")
	}
	if _, err := sc.Execute(ctx, nodeCommands(start)[0]); err != nil {
		t.Fatalf("kick: %v", err)
	}
	if kicks.Load() != 1 {
		t.Fatal("kick did not reach the hook")
	}
}

// Setting the flag on a connected console stops its commands, and the
// re-auth closes its snoop, without a reconnect. Clearing it lets the
// console snoop again.
func TestWFCReadOnlySetMidSession(t *testing.T) {
	signer := newTestSigner(t)
	addr, tap, start := startWFCTestServer(t, signer)
	wfcReauthInterval = 20 * time.Millisecond // restored by startWFCTestServer
	setReadOnly := useAdminAccount(t, signer, false)
	kicks := useAdminServer(t)
	c := dialWFC(t, addr, gossh.PublicKeys(signer))

	st, err := snoopOpen(t, c, 4, start)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sc := openAdmin(t, c)
	ctx := context.Background()
	if _, err := sc.Snapshot(ctx); err != nil {
		t.Fatal(err)
	}

	setReadOnly(true)

	if _, err := sc.Execute(ctx, nodeCommands(start)[0]); err == nil || err.Error() != admin.ErrReadOnly.Error() {
		t.Fatalf("kick after the flag was set: err = %v", err)
	}
	if kicks.Load() != 0 {
		t.Fatal("kick reached the hook")
	}

	closed := make(chan error, 1)
	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := st.Read(buf); err != nil {
				closed <- err
				return
			}
		}
	}()
	select {
	case err := <-closed:
		if !errors.Is(err, io.EOF) {
			t.Logf("snoop ended with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("snoop still open after the account became read-only")
	}
	deadline := time.Now().Add(5 * time.Second)
	for snoopsAs(tap, "boss") {
		if time.Now().After(deadline) {
			t.Fatal("read-only account still watching")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for {
		snap, err := sc.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if snap.ReadOnly {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("console never told it became read-only")
		}
		time.Sleep(10 * time.Millisecond)
	}

	setReadOnly(false)
	st2, err := snoopOpen(t, c, 4, start)
	if err != nil {
		t.Fatalf("snoop after clearing the flag: %v", err)
	}
	defer st2.Close()
	if !strings.Contains(st2.Header.Handle, "caller") {
		t.Fatalf("header %+v", st2.Header)
	}
}
