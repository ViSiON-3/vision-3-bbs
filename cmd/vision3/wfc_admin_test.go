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

	if authorizeAdmin("ghost") {
		t.Error("expected unknown user to be denied admin access")
	}
}
