package v3net

import (
	"strings"
	"testing"
)

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
