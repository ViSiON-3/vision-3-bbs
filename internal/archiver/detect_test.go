package archiver

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDetectFile checks signature precedence, enabled extension fallback,
// and handling of invalid signatures, short inputs, and inaccessible files.
func TestDetectFile(t *testing.T) {
	cfg := Config{Archivers: []Archiver{
		{ID: "zip", Extension: ".zip", Magic: "504B0304", Enabled: true},
		{ID: "rar", Extension: ".rar", Magic: "52617221", Enabled: true},
		{ID: "disabled", Extension: ".off", Magic: "444953", Enabled: false},
		{ID: "invalid", Extension: ".invalid", Magic: "xyz", Enabled: true},
		{ID: "lha", Extension: ".lha", Enabled: true},
	}}
	for _, tt := range []struct{ name, data, want string }{
		{"NODELIST.Z75", "PK\x03\x04payload", "zip"},
		{"misnamed.rar", "PK\x03\x04payload", "zip"},
		{"misnamed.zip", "Rar!payload", "rar"},
		{"legacy.LHA", "legacy", "lha"},
		{"unknown.bin", "unknown", ""},
		{"disabled.off", "DIS", ""},
		{"invalid.bin", "xyz", ""},
		{"short.bin", "PK", ""},
		{"corrupt.Z75", "PK\x03\x04", "zip"},
		{"text.ZIP", "not zip", "zip"},
		{"empty.bin", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tt.name)
			if err := os.WriteFile(path, []byte(tt.data), 0600); err != nil {
				t.Fatal(err)
			}
			a, ok, err := cfg.DetectFile(path)
			if err != nil || ok != (tt.want != "") || a.ID != tt.want {
				t.Fatalf("got %q, %v, %v; want %q", a.ID, ok, err, tt.want)
			}
		})
	}
	if _, ok, err := cfg.DetectFile(filepath.Join(t.TempDir(), "missing.zip")); err == nil || ok {
		t.Fatalf("missing: %v, %v", ok, err)
	}
}

// A signature can sit past the start of the file: LHA's "-lh" method ID
// follows a two-byte header, so an LHA nodelist named NODELIST.L75 is found
// by content. A file too short to reach the offset doesn't match it.
func TestDetectFileMagicOffset(t *testing.T) {
	cfg := Config{Archivers: []Archiver{
		{ID: "zip", Extension: ".zip", Magic: "504B0304", Enabled: true},
		{ID: "lha", Extension: ".lha", Magic: "2D6C68", MagicOffset: 2, Enabled: true},
		{ID: "neg", Extension: ".neg", Magic: "41", MagicOffset: -1, Enabled: true},
	}}
	for _, tt := range []struct{ name, data, want string }{
		{"NODELIST.L75", "\x2a\x10-lh5-payload", "lha"},
		{"lzh.zip", "\x2a\x10-lh0-", "lha"},
		{"short.bin", "\x2a\x10-l", ""},
		{"lh-at-start.bin", "-lh5-", ""},
		{"NODELIST.Z75", "PK\x03\x04", "zip"},
		{"negative.bin", "A", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tt.name)
			if err := os.WriteFile(path, []byte(tt.data), 0600); err != nil {
				t.Fatal(err)
			}
			a, ok, err := cfg.DetectFile(path)
			if err != nil || ok != (tt.want != "") || a.ID != tt.want {
				t.Fatalf("got %q, %v, %v; want %q", a.ID, ok, err, tt.want)
			}
			if got := cfg.IsSupportedFile(path); got != (tt.want != "") {
				t.Errorf("IsSupportedFile = %v", got)
			}
		})
	}
	if cfg.IsSupportedFile(filepath.Join(t.TempDir(), "missing.zip")) {
		t.Error("a missing file is supported")
	}
	def := DefaultConfig()
	lha, _ := def.FindByID("lha")
	if lha.Magic != "2D6C68" || lha.MagicOffset != 2 {
		t.Errorf("built-in lha = %q at %d", lha.Magic, lha.MagicOffset)
	}
}

// Only a regular file is opened or can be an archive: a FIFO would block
// the open until something writes to it, and a directory named .zip is no
// ZIP.
func TestDetectFileNotRegular(t *testing.T) {
	cfg := DefaultConfig()
	dir := filepath.Join(t.TempDir(), "folder.zip")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if a, ok, err := cfg.DetectFile(dir); ok || err != nil {
		t.Errorf("directory: %q, %v, %v", a.ID, ok, err)
	}
	fifo := filepath.Join(t.TempDir(), "notes.zip")
	if err := mkfifo(fifo); err != nil {
		t.Skipf("no FIFOs here: %v", err)
	}
	done := make(chan bool, 1)
	go func() { done <- cfg.IsSupportedFile(fifo) }()
	select {
	case ok := <-done:
		if ok {
			t.Error("a FIFO is supported")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DetectFile blocked opening a FIFO")
	}
}
