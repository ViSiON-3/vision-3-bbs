package main

import (
	"net"
	"testing"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestAuthorizeAdmin(t *testing.T) {
	// Seed userMgr with a low-access user and a sysop.
	userMgr = user.NewUserMgrForTest(
		&user.User{Handle: "lowly", AccessLevel: 10},
		&user.User{Handle: "boss", AccessLevel: 255},
	)
	adminMinLevel = func() int { return 250 }
	wfcEnabled = func() bool { return true }
	t.Cleanup(func() { userMgr, adminMinLevel, wfcEnabled = nil, nil, nil })

	if authorizeAdmin("lowly") {
		t.Error("expected lowly (level 10) to be denied admin access")
	}
	if !authorizeAdmin("boss") {
		t.Error("expected boss (level 255) to be granted admin access")
	}
}

func TestAuthorizeAdmin_UnknownUser(t *testing.T) {
	userMgr = user.NewUserMgrForTest()
	adminMinLevel = func() int { return 250 }
	wfcEnabled = func() bool { return true }
	t.Cleanup(func() { userMgr, adminMinLevel, wfcEnabled = nil, nil, nil })

	if authorizeAdmin("ghost") {
		t.Error("expected unknown user to be denied admin access")
	}
}

func TestAuthorizeAdmin_WFCDisabled(t *testing.T) {
	userMgr = user.NewUserMgrForTest(
		&user.User{Handle: "boss", AccessLevel: 255},
	)
	adminMinLevel = func() int { return 250 }
	t.Cleanup(func() { userMgr, adminMinLevel, wfcEnabled = nil, nil, nil })

	wfcEnabled = func() bool { return false }
	if authorizeAdmin("boss") {
		t.Error("expected admin access denied when WFC is disabled")
	}

	wfcEnabled = nil
	if authorizeAdmin("boss") {
		t.Error("expected admin access denied when wfcEnabled getter is nil")
	}
}

// stashCtx is the part of ssh.Context wfcPublicKeyHandler uses.
type stashCtx struct {
	ssh.Context
	vals map[any]any
}

func (c *stashCtx) SetValue(k, v any)    { c.vals[k] = v }
func (c *stashCtx) Value(k any) any      { return c.vals[k] }
func (c *stashCtx) RemoteAddr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }

func TestWFCRejectedKeyClearsEarlierStash(t *testing.T) {
	admin, other := newTestSigner(t), newTestSigner(t)
	keyLine := string(gossh.MarshalAuthorizedKey(admin.PublicKey()))
	oldUM, oldMin, oldEn := userMgr, adminMinLevel, wfcEnabled
	t.Cleanup(func() { userMgr, adminMinLevel, wfcEnabled = oldUM, oldMin, oldEn })
	userMgr = user.NewUserMgrForTest(&user.User{Handle: "boss", AccessLevel: 255, PublicKeys: []string{keyLine}})
	adminMinLevel = func() int { return 250 }
	wfcEnabled = func() bool { return true }

	ctx := &stashCtx{vals: map[any]any{}}
	adminKey, err := ssh.ParsePublicKey(admin.PublicKey().Marshal())
	if err != nil {
		t.Fatal(err)
	}
	if !wfcPublicKeyHandler(ctx, adminKey) {
		t.Fatal("admin key rejected")
	}
	otherKey, err := ssh.ParsePublicKey(other.PublicKey().Marshal())
	if err != nil {
		t.Fatal(err)
	}
	if wfcPublicKeyHandler(ctx, otherKey) {
		t.Fatal("unregistered key accepted")
	}
	if h, _ := ctx.Value(wfcAdminHandleKey{}).(string); h != "" {
		t.Fatalf("handle stash = %q after rejected key", h)
	}
	if k, _ := ctx.Value(wfcAdminPubKey{}).([]byte); len(k) != 0 {
		t.Fatal("key stash left after rejected key")
	}
}
