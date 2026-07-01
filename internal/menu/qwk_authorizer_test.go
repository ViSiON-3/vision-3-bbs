package menu

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestQWKWriteAuthorizer(t *testing.T) {
	lowUser := &user.User{Handle: "newbie", AccessLevel: 10}
	auth := QWKWriteAuthorizer(lowUser)

	// Empty ACS => allowed.
	if !auth(&message.MessageArea{ACSWrite: ""}) {
		t.Error("empty ACS should allow")
	}
	// Security-level gate the user fails.
	if auth(&message.MessageArea{ACSWrite: "s50"}) {
		t.Error("s50 should deny AccessLevel 10")
	}
	// Security-level gate the user passes.
	if !auth(&message.MessageArea{ACSWrite: "s5"}) {
		t.Error("s5 should allow AccessLevel 10")
	}
	// Nil area => denied.
	if auth(nil) {
		t.Error("nil area should deny")
	}
}

func TestResolveQWKIDExported(t *testing.T) {
	// ResolveQWKID is the exported wrapper — same logic as the unexported resolveQWKID.
	// Explicit QWKID wins and is normalized (uppercase, non-alnum stripped):
	// a dirty input exercises NormalizeQWKID.
	if got := ResolveQWKID(config.ServerConfig{QWKID: "my id!", BoardName: "Whatever"}); got != "MYID" {
		t.Errorf("explicit QWKID: got %q, want %q", got, "MYID")
	}
	// Blank QWKID falls back to board-name derivation.
	if got := ResolveQWKID(config.ServerConfig{QWKID: "", BoardName: "ViSiON/3 BBS"}); got != "VISION3B" {
		t.Errorf("board-name fallback: got %q, want %q", got, "VISION3B")
	}
	// Both invalid => fallback "BBS".
	if got := ResolveQWKID(config.ServerConfig{QWKID: "!!!", BoardName: "###"}); got != "BBS" {
		t.Errorf("all-invalid fallback: got %q, want %q", got, "BBS")
	}
}
