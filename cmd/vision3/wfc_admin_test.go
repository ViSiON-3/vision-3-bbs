package main

import (
	"testing"

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

	if authorizeAdmin("ghost") {
		t.Error("expected unknown user to be denied admin access")
	}
}

func TestAuthorizeAdmin_WFCDisabled(t *testing.T) {
	userMgr = user.NewUserMgrForTest(
		&user.User{Handle: "boss", AccessLevel: 255},
	)
	adminMinLevel = func() int { return 250 }

	wfcEnabled = func() bool { return false }
	if authorizeAdmin("boss") {
		t.Error("expected admin access denied when WFC is disabled")
	}

	wfcEnabled = nil
	if authorizeAdmin("boss") {
		t.Error("expected admin access denied when wfcEnabled getter is nil")
	}
}
