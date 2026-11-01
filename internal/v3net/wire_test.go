package v3net

import (
	"strings"
	"testing"
)

// A subject stored before control characters were cleaned on the way in can
// still be sent: the hub would refuse it as written on every sweep.
func TestBuildWireMessageCleansSubject(t *testing.T) {
	msg := BuildWireMessage("net", "gen.general", "node", "Board", "From", "To", "a\x1b[2Jb", "Body", "")
	if msg.Subject != "a [2Jb" {
		t.Errorf("Subject = %q", msg.Subject)
	}
	if err := msg.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// A control character in the sysop's origin or board name must not get every
// post refused by the hub. A raw CP437 byte in one is not a control character.
func TestBuildWireMessageCleansOriginFields(t *testing.T) {
	msg := BuildWireMessage("testnet", "gen.general", "node", "Bo\x1b[2Jard", "From", "To", "Subject", "Body", "My\u009bBBS \x9b")
	if msg.OriginBoard != "Bo [2Jard" {
		t.Errorf("OriginBoard = %q", msg.OriginBoard)
	}
	if msg.Origin != "My BBS \x9b" {
		t.Errorf("Origin = %q", msg.Origin)
	}
	if err := msg.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestBuildWireMessageTearlineIsAlwaysDefault(t *testing.T) {
	msg := BuildWireMessage("testnet", "gen.general", "abc123", "General", "Sysop", "All", "Hello", "body text", "My Cool BBS")
	if !strings.HasPrefix(msg.Tearline, "--- ViSiON/3 ") {
		t.Errorf("expected default ViSiON/3 tearline, got %q", msg.Tearline)
	}
}

func TestBuildWireMessageOriginPassedThrough(t *testing.T) {
	msg := BuildWireMessage("testnet", "gen.general", "abc123", "General", "Sysop", "All", "Hello", "body text", "My Cool BBS - bbs.example.com")
	if msg.Origin != "My Cool BBS - bbs.example.com" {
		t.Errorf("expected origin to be passed through, got %q", msg.Origin)
	}
}

func TestBuildWireMessageEmptyOrigin(t *testing.T) {
	msg := BuildWireMessage("testnet", "gen.general", "abc123", "General", "Sysop", "All", "Hello", "body text", "")
	if msg.Origin != "" {
		t.Errorf("expected empty origin, got %q", msg.Origin)
	}
	if !strings.HasPrefix(msg.Tearline, "--- ViSiON/3 ") {
		t.Errorf("expected default tearline even with empty origin, got %q", msg.Tearline)
	}
}

func TestDefaultTearline(t *testing.T) {
	tl := DefaultTearline()
	if !strings.HasPrefix(tl, "--- ViSiON/3 ") {
		t.Errorf("DefaultTearline() = %q, want prefix '--- ViSiON/3 '", tl)
	}
}
