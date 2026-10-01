package main

import (
	"testing"

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

func TestWFCVerifiedKeyNamesTheKeyOwner(t *testing.T) {
	boss, chief, other := newTestSigner(t), newTestSigner(t), newTestSigner(t)
	line := func(s gossh.Signer) string { return string(gossh.MarshalAuthorizedKey(s.PublicKey())) }
	oldUM, oldMin, oldEn := userMgr, adminMinLevel, wfcEnabled
	t.Cleanup(func() { userMgr, adminMinLevel, wfcEnabled = oldUM, oldMin, oldEn })
	userMgr = user.NewUserMgrForTest(
		&user.User{Handle: "boss", AccessLevel: 255, PublicKeys: []string{line(boss)}},
		&user.User{Handle: "chief", AccessLevel: 255, PublicKeys: []string{line(chief)}},
	)
	adminMinLevel = func() int { return 250 }
	wfcEnabled = func() bool { return true }

	in := &gossh.Permissions{Extensions: map[string]string{"keep": "me"}}
	out, err := wfcVerifiedKey(nil, chief.PublicKey(), in, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Extensions[wfcHandleExt] != "chief" || out.Extensions[wfcKeyExt] != string(chief.PublicKey().Marshal()) {
		t.Fatalf("extensions %v, want chief's identity", out.Extensions)
	}
	if out.Extensions["keep"] != "me" {
		t.Fatal("existing extension dropped")
	}
	if _, ok := in.Extensions[wfcHandleExt]; ok {
		t.Fatal("incoming permissions modified")
	}
	if _, err := wfcVerifiedKey(nil, other.PublicKey(), &gossh.Permissions{}, ""); err == nil {
		t.Fatal("unregistered key verified as an admin")
	}
}
