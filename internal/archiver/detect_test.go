package archiver

import (
	"os"
	"path/filepath"
	"testing"
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
