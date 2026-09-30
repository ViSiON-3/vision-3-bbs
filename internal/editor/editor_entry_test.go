package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
	"github.com/gliderlabs/ssh"
)

// ptySession is a scripted session that reports a PTY of a given size, which
// testterm.Session on its own never does.
type ptySession struct {
	*testterm.Session
	width, height int
	resize        chan ssh.Window
}

func (p *ptySession) Pty() (ssh.Pty, <-chan ssh.Window, bool) {
	return ssh.Pty{Window: ssh.Window{Width: p.width, Height: p.height}}, p.resize, true
}

// installEditorConfig points RunEditorWithMetadata at a menu set and a config
// directory of its own, so the test does not depend on the repository's files
// or on the directory it runs from.
func installEditorConfig(t *testing.T) {
	t.Helper()
	tagline := "   ViSiON/3 Edit" + strings.Repeat(" ", 42) + "Press ESCape For Help"
	menuSet := writeMenuSet(t, map[string]string{
		"FSEDITOR.ANS":  infoBarTemplate,
		"FSEDITORF.ANS": "row one is rebuilt\r\n" + tagline,
	})

	configDir := t.TempDir()
	for name, content := range map[string]string{
		"config.json": `{"boardName": "Test Board", "timezone": "UTC"}`,
		"strings.json": `{
			"quoteTop": "^N wrote:  ",
			"quoteBottom": "-- end of ^N --",
			"QuotePrefix": "^N> ",
			"yesPromptText": "Yep",
			"noPromptText": "Nope",
			"abortMessagePrompt": "Really quit?"
		}`,
	} {
		if err := os.WriteFile(filepath.Join(configDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	t.Setenv("VISION3_MENU_PATH", menuSet)
	t.Setenv("VISION3_CONFIG_PATH", configDir)
}

// The whole path a caller takes: metadata into the header, the board name into
// the footer, quote styling from strings.json, and the edited text back out.
func TestRunEditorWithMetadata(t *testing.T) {
	installEditorConfig(t)
	tt := testterm.New(80, 24)
	// Type a line, open the quote picker (CTRL-Q), quote the first line, close
	// the picker (CTRL-Q again), type the reply and save (CTRL-Z).
	sess := testterm.NewSession(tt, "hi\r"+"\x11"+" "+"\x11"+"bye"+"\x1a")

	content, saved, err := RunEditorWithMetadata("", sess, tt, ansi.OutputModeUTF8,
		"Hello", "bob", "alice", false,
		"Bucko", "Re: doors", "08/28/26", "9:15 pm", false, []string{"quoted text"},
		nil, EditorContext{NodeNumber: 7, NextMsgNum: 42, ConfArea: "Local > General"})
	if err != nil || !saved {
		t.Fatalf("RunEditorWithMetadata: saved=%v err=%v", saved, err)
	}

	want := "hi\nBucko wrote:\nBucko> quoted text\n-- end of Bucko --\n\nbye"
	if got := stripANSI(content); got != want {
		t.Errorf("content = %q, want %q", got, want)
	}

	if got, want := tt.Row(1), "To: bob      From: alice"; got != want {
		t.Errorf("Row(1) = %q, want %q", got, want)
	}
	if got, want := tt.Row(2), "Subj: Hello         [Ins]"; got != want {
		t.Errorf("Row(2) = %q, want %q", got, want)
	}
	if got, want := tt.Row(3), "[░42]───────▌ Local > General ▐───────#Node:░░7]"; got != want {
		t.Errorf("Row(3) = %q, want %q", got, want)
	}
	if got := tt.Row(5); got != "hi" {
		t.Errorf("Row(5) = %q, want the first line of the message on the first editing row", got)
	}
	if got := tt.Row(23); !strings.HasPrefix(got, " └─▌Test Board▐") {
		t.Errorf("Row(23) = %q, want the footer carrying the board name", got)
	}
	if got := tt.Row(24); got != "Saving..." {
		t.Errorf("Row(24) = %q, want %q", got, "Saving...")
	}
}

// The abort prompt uses the question and the Yes/No labels from strings.json.
func TestRunEditorWithMetadataAbort(t *testing.T) {
	installEditorConfig(t)
	tt := testterm.New(80, 24)
	sess := testterm.NewSession(tt, " more"+"\x01"+"y")

	content, saved, err := RunEditorWithMetadata("draft", sess, tt, ansi.OutputModeUTF8,
		"Hello", "bob", "alice", false, "", "", "", "", false, nil, nil)
	if err != nil || saved {
		t.Fatalf("RunEditorWithMetadata: saved=%v err=%v, want an unsaved abort", saved, err)
	}
	// The initial content is loaded with the cursor at its end.
	if content != "draft more" {
		t.Errorf("content = %q, want %q", content, "draft more")
	}
	if got, want := tt.Row(24), " Really quit?   Yep    Nope"; got != want {
		t.Errorf("Row(24) = %q, want %q", got, want)
	}
	if got := highlighted(tt, 24); got != " Nope " {
		t.Errorf("highlighted = %q, want %q", got, " Nope ")
	}
}

// The editor sizes itself from the caller's PTY, but never below 80x24.
func TestRunEditorWithMetadataSizesToPTY(t *testing.T) {
	for _, tc := range []struct {
		name                string
		ptyWidth, ptyHeight int
		wantRow, wantWidth  int // where the footer's first row lands, and how wide it is
	}{
		{"large PTY", 100, 30, 29, 99},
		{"PTY smaller than the minimum", 40, 10, 23, 79},
	} {
		t.Run(tc.name, func(t *testing.T) {
			installEditorConfig(t)
			tt := testterm.New(100, 30)
			sess := &ptySession{
				Session: testterm.NewSession(tt, "hi\x1a"),
				width:   tc.ptyWidth, height: tc.ptyHeight,
				resize: make(chan ssh.Window),
			}

			content, saved, err := RunEditorWithMetadata("", sess, tt, ansi.OutputModeUTF8,
				"Hello", "bob", "alice", false, "", "", "", "", false, nil, nil)
			if err != nil || !saved || content != "hi" {
				t.Fatalf("RunEditorWithMetadata = (%q, %v, %v), want (\"hi\", true, nil)", content, saved, err)
			}

			row := tt.Row(tc.wantRow)
			if !strings.HasPrefix(row, " └─▌Test Board▐") {
				t.Errorf("Row(%d) = %q, want the footer there", tc.wantRow, row)
			}
			if got := len([]rune(row)); got != tc.wantWidth {
				t.Errorf("footer row is %d columns, want %d", got, tc.wantWidth)
			}
		})
	}
}

// A caller-supplied InputHandler is used as-is and left open for the caller.
func TestRunEditorWithMetadataSharesInputHandler(t *testing.T) {
	installEditorConfig(t)
	tt := testterm.New(80, 24)
	sess := testterm.NewSession(tt, "hi\x1a"+"menu")
	shared := NewInputHandler(sess)
	defer shared.CloseAndWait()

	content, saved, err := RunEditorWithMetadata("", sess, tt, ansi.OutputModeUTF8,
		"Hello", "bob", "alice", false, "", "", "", "", false, nil, shared)
	if err != nil || !saved || content != "hi" {
		t.Fatalf("RunEditorWithMetadata = (%q, %v, %v), want (\"hi\", true, nil)", content, saved, err)
	}

	// Keys typed after the save belong to whoever reads next.
	if key, err := shared.ReadKeyWithTimeout(5 * time.Second); err != nil || key != 'm' {
		t.Errorf("next key on the shared handler = %q, %v; want 'm'", key, err)
	}
}

// Without an ssh.Session there is no terminal to edit on; the text comes back
// untouched and unsaved.
func TestRunEditorWithMetadataNeedsASession(t *testing.T) {
	var out strings.Builder
	content, saved, err := RunEditorWithMetadata("draft", strings.NewReader("x\x1a"), &out, ansi.OutputModeUTF8,
		"Hello", "bob", "alice", false, "", "", "", "", false, nil, nil)
	if content != "draft" || saved || err != nil {
		t.Errorf("RunEditorWithMetadata = (%q, %v, %v), want (\"draft\", false, nil)", content, saved, err)
	}
	if out.Len() != 0 {
		t.Errorf("wrote %q to a non-session output", out.String())
	}
}

func TestResolveEditorPaths(t *testing.T) {
	menuDir, configDir := t.TempDir(), t.TempDir()

	t.Run("environment paths that exist are used", func(t *testing.T) {
		t.Setenv("VISION3_MENU_PATH", menuDir)
		t.Setenv("VISION3_CONFIG_PATH", configDir)
		if menu, cfg := resolveEditorPaths(); menu != menuDir || cfg != configDir {
			t.Errorf("resolveEditorPaths() = (%q, %q), want (%q, %q)", menu, cfg, menuDir, configDir)
		}
	})

	t.Run("defaults when nothing is set or found", func(t *testing.T) {
		t.Setenv("VISION3_MENU_PATH", "")
		t.Setenv("VISION3_CONFIG_PATH", "")
		t.Chdir(t.TempDir())
		if menu, cfg := resolveEditorPaths(); menu != "menus/v3" || cfg != "configs" {
			t.Errorf("resolveEditorPaths() = (%q, %q), want the defaults (\"menus/v3\", \"configs\")", menu, cfg)
		}
	})

	t.Run("missing environment paths fall back to the working directory", func(t *testing.T) {
		root := t.TempDir()
		for _, dir := range []string{"menus/v3", "configs"} {
			if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		t.Chdir(root)
		cwd, err := os.Getwd() // as the editor sees it, symlinks and all
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("VISION3_MENU_PATH", filepath.Join(menuDir, "gone"))
		t.Setenv("VISION3_CONFIG_PATH", configDir)

		menu, cfg := resolveEditorPaths()
		if menu != filepath.Join(cwd, "menus/v3") || cfg != filepath.Join(cwd, "configs") {
			t.Errorf("resolveEditorPaths() = (%q, %q), want the pair under %q", menu, cfg, cwd)
		}
	})

	t.Run("missing paths with no fallback are returned as given", func(t *testing.T) {
		gone := filepath.Join(menuDir, "gone")
		t.Setenv("VISION3_MENU_PATH", gone)
		t.Setenv("VISION3_CONFIG_PATH", configDir)
		t.Chdir(t.TempDir())
		if menu, cfg := resolveEditorPaths(); menu != gone || cfg != configDir {
			t.Errorf("resolveEditorPaths() = (%q, %q), want (%q, %q)", menu, cfg, gone, configDir)
		}
	})
}
