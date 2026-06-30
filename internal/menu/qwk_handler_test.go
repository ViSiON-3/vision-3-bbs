package menu

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func TestResolveQWKID(t *testing.T) {
	// Explicit QWKID wins and is normalized.
	if got := resolveQWKID(config.ServerConfig{QWKID: "my id!", BoardName: "Whatever"}); got != "MYID" {
		t.Errorf("explicit: want MYID, got %q", got)
	}
	// Blank QWKID falls back to board-name derivation.
	if got := resolveQWKID(config.ServerConfig{QWKID: "", BoardName: "ViSiON/3 BBS"}); got != "VISION3B" {
		t.Errorf("derive: want VISION3B, got %q", got)
	}
	// Both blank/invalid → BBS.
	if got := resolveQWKID(config.ServerConfig{QWKID: "!!!", BoardName: "###"}); got != "BBS" {
		t.Errorf("fallback: want BBS, got %q", got)
	}
}

func TestQwkBBSID(t *testing.T) {
	tests := []struct {
		name      string
		boardName string
		want      string
	}{
		{"simple alphanumeric", "VISION3", "VISION3"},
		{"mixed case", "Vision3 BBS", "VISION3B"},
		{"spaces stripped", "My Cool BBS", "MYCOOLBB"},
		{"special chars stripped", "V!S!O#N/3", "VSON3"},
		{"long name truncated", "LongBoardName123", "LONGBOAR"},
		{"empty string", "", "BBS"},
		{"all special chars", "!@#$%^&*()", "BBS"},
		{"numbers only", "12345678901", "12345678"},
		{"unicode stripped", "Café BBS", "CAFBBS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := qwkBBSID(tt.boardName)
			if got != tt.want {
				t.Errorf("qwkBBSID(%q) = %q, want %q", tt.boardName, got, tt.want)
			}
		})
	}
}

func TestFindREPFile_ExactMatch(t *testing.T) {
	dir := t.TempDir()
	repPath := filepath.Join(dir, "VISION3.REP")
	if err := os.WriteFile(repPath, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	got := findREPFile(dir, "VISION3")
	if got != repPath {
		t.Errorf("findREPFile() = %q, want %q", got, repPath)
	}
}

func TestFindREPFile_CaseInsensitiveMatch(t *testing.T) {
	dir := t.TempDir()
	repPath := filepath.Join(dir, "vision3.rep")
	if err := os.WriteFile(repPath, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	got := findREPFile(dir, "VISION3")
	if got != repPath {
		t.Errorf("findREPFile() = %q, want %q", got, repPath)
	}
}

func TestFindREPFile_FallbackToAnyREP(t *testing.T) {
	dir := t.TempDir()
	// File doesn't match the expected bbsID, but is still a .REP
	repPath := filepath.Join(dir, "OTHER.REP")
	if err := os.WriteFile(repPath, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	got := findREPFile(dir, "VISION3")
	if got != repPath {
		t.Errorf("findREPFile() fallback = %q, want %q", got, repPath)
	}
}

func TestFindREPFile_NoREPFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "VISION3.QWK"), []byte("data"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	got := findREPFile(dir, "VISION3")
	if got != "" {
		t.Errorf("findREPFile() = %q, want empty string", got)
	}
}

func TestFindREPFile_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	got := findREPFile(dir, "VISION3")
	if got != "" {
		t.Errorf("findREPFile() = %q, want empty string", got)
	}
}

func TestFindREPFile_InvalidDir(t *testing.T) {
	got := findREPFile("/nonexistent/path", "VISION3")
	if got != "" {
		t.Errorf("findREPFile() = %q, want empty string for invalid dir", got)
	}
}
