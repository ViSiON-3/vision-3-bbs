package menu

import (
	"bytes"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"golang.org/x/term"
)

func TestArtWidth_UsesWiderPhysicalWidth(t *testing.T) {
	terminal := term.NewTerminal(&bytes.Buffer{}, "")
	var phys atomic.Int32
	phys.Store(120)
	RegisterTerminalPhysicalWidth(terminal, &phys)
	t.Cleanup(func() { ClearTerminalPhysicalWidth(terminal) })

	// A saved 80-column preference on a 120-column window: art is fitted to
	// the window, since that is where the terminal autowraps.
	if got := artWidth(terminal, 80); got != 120 {
		t.Errorf("artWidth(pref 80, physical 120) = %d, want 120", got)
	}

	// A resize is picked up through the registered value.
	phys.Store(100)
	if got := artWidth(terminal, 80); got != 100 {
		t.Errorf("artWidth after resize = %d, want 100", got)
	}

	// A logical width wider than the reported one wins.
	if got := artWidth(terminal, 132); got != 132 {
		t.Errorf("artWidth(pref 132, physical 100) = %d, want 132", got)
	}
}

func TestArtWidth_UnregisteredFallsBackToTermWidth(t *testing.T) {
	terminal := term.NewTerminal(&bytes.Buffer{}, "")
	if got := artWidth(terminal, 80); got != 80 {
		t.Errorf("artWidth(unregistered) = %d, want 80", got)
	}
}

func TestWriteArt_HardWrapsForPhysicalWidth(t *testing.T) {
	var out bytes.Buffer
	terminal := term.NewTerminal(&out, "")
	var phys atomic.Int32
	phys.Store(120)
	RegisterTerminalPhysicalWidth(terminal, &phys)
	t.Cleanup(func() { ClearTerminalPhysicalWidth(terminal) })

	// Two 80-column rows with no line break between them, relying on autowrap.
	art := []byte(strings.Repeat("A", 80) + strings.Repeat("B", 80))
	if err := writeArt(terminal, art, ansi.OutputModeCP437, 80); err != nil {
		t.Fatalf("writeArt: %v", err)
	}
	// term.Terminal expands the inserted LF to CRLF, so just require a line
	// break between the rows.
	if i := bytes.IndexByte(out.Bytes(), 'B'); i < 1 || out.Bytes()[i-1] != '\n' {
		t.Errorf("art not hard-wrapped at column 80 for a 120-column terminal: %q", out.Bytes())
	}
}
