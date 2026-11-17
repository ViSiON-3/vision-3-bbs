package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/stringeditor"
)

// The editor previews |C1-|C7 in the board's defaults, as the BBS draws
// them, not the built-in ones: here the legacy defColor1 in strings.json and
// the theme's callerStat.
func TestPreviewUsesBoardColorSlots(t *testing.T) {
	testPreviewUsesBoardColorSlots(t, false)
}

// The same when the editor runs in the configs directory and is given
// strings.json: the theme is still found in the board's menus.
func TestPreviewUsesBoardColorSlotsRelativePath(t *testing.T) {
	testPreviewUsesBoardColorSlots(t, true)
}

func testPreviewUsesBoardColorSlots(t *testing.T, relative bool) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() {
		lipgloss.SetColorProfile(old)
		ansi.SetColorSlotDefaults([7]string{})
	})
	root := t.TempDir()
	path := filepath.Join(root, "configs", "strings.json")
	for name, body := range map[string]string{
		path: `{"defColor1": 12}`,
		filepath.Join(root, "menus", "v3", "theme.json"): `{"callerStat": 10}`,
	} {
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	builtin := stringeditor.RenderColorString("|C1x", 10)
	if relative {
		t.Chdir(filepath.Dir(path))
		path = "strings.json"
	}
	loadBoardColorSlots(path)
	got := stringeditor.RenderColorString("|C1x", 10)
	if want := stringeditor.RenderColorString("|12x", 10); got != want {
		t.Errorf("|C1x renders %q, want |12x's %q", got, want)
	}
	if got == builtin {
		t.Errorf("|C1x still renders in the built-in colour")
	}
	if got, want := stringeditor.RenderColorString("|C4x", 10), stringeditor.RenderColorString("|10x", 10); got != want {
		t.Errorf("|C4x renders %q, want the theme's |10x %q", got, want)
	}
}
