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

// The editor previews |C1-|C7 in the defaults the board's strings.json sets,
// as the BBS draws them, not the built-in ones.
func TestPreviewUsesBoardColorSlots(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() {
		lipgloss.SetColorProfile(old)
		ansi.SetColorSlotDefaults([7]string{})
	})
	path := filepath.Join(t.TempDir(), "strings.json")
	if err := os.WriteFile(path, []byte(`{"defColor1": 12}`), 0o644); err != nil {
		t.Fatal(err)
	}
	builtin := stringeditor.RenderColorString("|C1x", 10)
	loadBoardColorSlots(path)
	got := stringeditor.RenderColorString("|C1x", 10)
	if want := stringeditor.RenderColorString("|12x", 10); got != want {
		t.Errorf("|C1x renders %q, want |12x's %q", got, want)
	}
	if got == builtin {
		t.Errorf("|C1x still renders in the built-in colour")
	}
}
