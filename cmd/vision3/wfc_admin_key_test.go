package main

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	gossh "golang.org/x/crypto/ssh"
)

// testKeyLine is a throwaway ed25519 public key used only as authorized-key
// fixture data; no private half exists anywhere.
const testKeyLine = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKxXeniKraUkypfTmKhIriFllkZ1EqIlW7Vjq4XzXqV/ test@fixture"

func mustKeyBytes(t *testing.T, line string) []byte {
	t.Helper()
	pub, _, _, _, err := gossh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		t.Fatalf("parse fixture key: %v", err)
	}
	return pub.Marshal()
}

// TestAuthorizeAdminKey verifies the session predicate authorizes only when the
// presented key is still registered to a qualifying account.
func TestAuthorizeAdminKey(t *testing.T) {
	keyBytes := mustKeyBytes(t, testKeyLine)
	adminMinLevel = func() int { return 250 }
	wfcEnabled = func() bool { return true }
	t.Cleanup(func() { userMgr, adminMinLevel, wfcEnabled = nil, nil, nil })

	t.Run("registered sysop is authorized", func(t *testing.T) {
		userMgr = user.NewUserMgrForTest(
			&user.User{Handle: "boss", AccessLevel: 255, PublicKeys: []string{testKeyLine}},
		)
		if !authorizeAdminKey("boss", keyBytes) {
			t.Error("expected sysop with registered key to be authorized")
		}
	})

	t.Run("key removed revokes authorization", func(t *testing.T) {
		userMgr = user.NewUserMgrForTest(
			&user.User{Handle: "boss", AccessLevel: 255}, // key no longer listed
		)
		if authorizeAdminKey("boss", keyBytes) {
			t.Error("expected authorization denied after the key was removed")
		}
	})

	t.Run("soft-deleted account revokes authorization", func(t *testing.T) {
		userMgr = user.NewUserMgrForTest(
			&user.User{Handle: "boss", AccessLevel: 255, PublicKeys: []string{testKeyLine},
				DeletedUser: true},
		)
		if authorizeAdminKey("boss", keyBytes) {
			t.Error("expected authorization denied for a soft-deleted account")
		}
	})

	t.Run("demotion revokes authorization", func(t *testing.T) {
		userMgr = user.NewUserMgrForTest(
			&user.User{Handle: "boss", AccessLevel: 10, PublicKeys: []string{testKeyLine}},
		)
		if authorizeAdminKey("boss", keyBytes) {
			t.Error("expected authorization denied after demotion below CoSysOp")
		}
	})

	t.Run("key belonging to a different account is denied", func(t *testing.T) {
		userMgr = user.NewUserMgrForTest(
			&user.User{Handle: "other", AccessLevel: 255, PublicKeys: []string{testKeyLine}},
		)
		if authorizeAdminKey("boss", keyBytes) {
			t.Error("expected denial when the key maps to a different handle")
		}
	})

	t.Run("wfc disabled revokes authorization", func(t *testing.T) {
		userMgr = user.NewUserMgrForTest(
			&user.User{Handle: "boss", AccessLevel: 255, PublicKeys: []string{testKeyLine}},
		)
		wfcEnabled = func() bool { return false }
		defer func() { wfcEnabled = func() bool { return true } }()
		if authorizeAdminKey("boss", keyBytes) {
			t.Error("expected authorization denied when WFC access is disabled")
		}
	})
}
