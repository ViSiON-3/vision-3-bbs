package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// newDisplayExecutor builds an executor over a temp menu set with an ansi/ dir.
func newDisplayExecutor(t *testing.T) *MenuExecutor {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "ansi"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return &MenuExecutor{
		MenuSetPath:    root,
		RootConfigPath: root,
		LoadedStrings:  config.StringsConfig{ExecFileLoadError: "Cannot load %s"},
	}
}

func TestDisplayFile(t *testing.T) {
	e := newDisplayExecutor(t)
	content := "Hello |15World"
	if err := os.WriteFile(filepath.Join(e.MenuSetPath, "ansi", "WELCOME.ANS"), []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	t.Run("renders file with pipe codes translated", func(t *testing.T) {
		ts := newTestSession("")
		if err := e.displayFile(newTestTerminal(ts), "WELCOME.ANS", ansi.OutputModeAuto); err != nil {
			t.Fatalf("displayFile: %v", err)
		}
		out := ts.output()
		if !strings.Contains(out, "Hello") || !strings.Contains(out, "World") {
			t.Errorf("output missing content: %q", out)
		}
		if strings.Contains(out, "|15") {
			t.Errorf("pipe code not translated: %q", out)
		}
	})

	t.Run("clearFirst prepends clear sequence", func(t *testing.T) {
		ts := newTestSession("")
		if err := e.displayFile(newTestTerminal(ts), "WELCOME.ANS", ansi.OutputModeAuto, true); err != nil {
			t.Fatalf("displayFile: %v", err)
		}
		if !strings.HasPrefix(ts.output(), ansi.ClearScreen()) {
			t.Errorf("output should start with clear sequence: %q", ts.output())
		}
	})

	t.Run("CP437 mode writes raw bytes", func(t *testing.T) {
		// Pipe codes are expanded in every mode; what CP437 mode guarantees
		// is that the resulting bytes — including high CP437 bytes like
		// 0xB1 (▒) — are written verbatim, with no UTF-8 translation.
		raw := append([]byte("Raw |15Block "), 0xB1)
		if err := os.WriteFile(filepath.Join(e.MenuSetPath, "ansi", "RAW.ANS"), raw, 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
		ts := newTestSession("")
		if err := e.displayFile(newTestTerminal(ts), "RAW.ANS", ansi.OutputModeCP437); err != nil {
			t.Fatalf("displayFile: %v", err)
		}
		want := string(ansi.ReplacePipeCodes(raw))
		if got := ts.output(); got != want {
			t.Errorf("output = %q, want raw %q", got, want)
		}
	})

	t.Run("missing file returns error and shows load-error string", func(t *testing.T) {
		ts := newTestSession("")
		err := e.displayFile(newTestTerminal(ts), "NOPE.ANS", ansi.OutputModeAuto)
		if err == nil {
			t.Fatal("missing file should return an error")
		}
		if !strings.Contains(ts.output(), "Cannot load NOPE.ANS") {
			t.Errorf("user-facing error not written: %q", ts.output())
		}
	})
}
