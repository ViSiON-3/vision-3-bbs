package configeditor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

func TestRasterizeArt_Dimensions(t *testing.T) {
	grid := rasterizeArt([]byte("hello"))
	if len(grid) != artHeight {
		t.Fatalf("rows = %d, want %d", len(grid), artHeight)
	}
	for i, row := range grid {
		if len(row) != artWidth {
			t.Fatalf("row %d width = %d, want %d", i, len(row), artWidth)
		}
	}
}

func TestRasterizeArt_PlainText(t *testing.T) {
	grid := rasterizeArt([]byte("AB"))
	if grid[0][0].ch != 'A' || grid[0][1].ch != 'B' {
		t.Fatalf("got %q%q, want AB", grid[0][0].ch, grid[0][1].ch)
	}
	if grid[0][2].ch != ' ' {
		t.Fatalf("cell[0][2] = %q, want space", grid[0][2].ch)
	}
}

func TestRasterizeArt_SGRColors(t *testing.T) {
	// ESC[1;33;44m X -> ANSI SGR fg 33 (yellow) and bg 44 (blue).
	// DOS/ANSI.SYS terminals remap the ANSI 0-7 color index to the CGA
	// palette order by swapping bit0 and bit2 (red<->blue channels), which
	// is what the fg/bg fields here store, matching dosColors in colors.go
	// (index 1 = Blue, index 4 = Red, index 6 = Brown, index 14 = Yellow).
	// ANSI yellow (3) -> CGA brown (6); bold sets the bright bit (+8) = 14.
	// ANSI blue (4) -> CGA blue (1).
	grid := rasterizeArt([]byte("\x1b[1;33;44mX"))
	c := grid[0][0]
	if c.ch != 'X' {
		t.Fatalf("ch = %q, want X", c.ch)
	}
	if c.fg != 14 { // ANSI yellow(3)->CGA brown(6), bold(+8) = 14 (Yellow)
		t.Fatalf("fg = %d, want 14", c.fg)
	}
	if c.bg != 1 { // ANSI blue(4)->CGA blue(1)
		t.Fatalf("bg = %d, want 1", c.bg)
	}
}

func TestRasterizeArt_Wrap(t *testing.T) {
	// 81 'X' chars: 80 fill row 0, the 81st wraps to row 1 col 0.
	data := make([]byte, 81)
	for i := range data {
		data[i] = 'X'
	}
	grid := rasterizeArt(data)
	if grid[0][79].ch != 'X' {
		t.Fatalf("row0 col79 = %q, want X", grid[0][79].ch)
	}
	if grid[1][0].ch != 'X' {
		t.Fatalf("row1 col0 = %q, want X (wrap)", grid[1][0].ch)
	}
}

func TestRasterizeArt_CursorPosition(t *testing.T) {
	// ESC[3;5H places cursor at row 3 col 5 (1-based) -> grid[2][4].
	grid := rasterizeArt([]byte("\x1b[3;5HZ"))
	if grid[2][4].ch != 'Z' {
		t.Fatalf("grid[2][4] = %q, want Z", grid[2][4].ch)
	}
}

func TestLoadBackdrop_CentersArt(t *testing.T) {
	b := loadBackdrop(100, 30)
	if !b.art {
		t.Fatal("expected art mode from embedded asset")
	}
	if b.width != 100 || b.height != 30 {
		t.Fatalf("size = %dx%d, want 100x30", b.width, b.height)
	}
	if len(b.cells) != 30 || len(b.cells[0]) != 100 {
		t.Fatalf("grid = %dx%d, want 100x30", len(b.cells), len(b.cells[0]))
	}
	// Art (80x25) centered in 100x30: startCol=10, startRow=2.
	// Margin cells are black spaces.
	if c := b.cells[0][0]; c.ch != ' ' || c.bg != 0 {
		t.Fatalf("top-left margin = %+v, want black space", c)
	}
}

func TestLoadBackdrop_ExactSize(t *testing.T) {
	b := loadBackdrop(80, 25)
	if len(b.cells) != 25 || len(b.cells[0]) != 80 {
		t.Fatalf("grid = %dx%d, want 80x25", len(b.cells), len(b.cells[0]))
	}
}

func TestBackdrop_SegmentVisibleWidth(t *testing.T) {
	b := loadBackdrop(80, 25)
	seg := b.segment(5, 0, 10)
	if got := ansiVisibleLen(seg); got != 10 {
		t.Fatalf("segment visible width = %d, want 10", got)
	}
}

func TestBackdrop_LineVisibleWidth(t *testing.T) {
	b := loadBackdrop(90, 25)
	if got := ansiVisibleLen(b.line(3)); got != 90 {
		t.Fatalf("line visible width = %d, want 90", got)
	}
}

func TestLoadBackdropFrom_EmptyAssetFallsBack(t *testing.T) {
	b := loadBackdropFrom(nil, 100, 30)
	if b.art {
		t.Fatal("empty asset should yield art:false fallback")
	}
	if !strings.Contains(b.segment(0, 0, 4), "░") {
		t.Fatalf("empty-asset fallback should render ░, got %q", b.segment(0, 0, 4))
	}
}

func TestBackdrop_FallbackUsesShade(t *testing.T) {
	b := &backdrop{width: 80, height: 25, art: false}
	seg := b.segment(0, 0, 4)
	if !strings.Contains(seg, "░") {
		t.Fatalf("fallback segment should contain ░, got %q", seg)
	}
}

func TestBackdrop_SegmentOutOfBounds(t *testing.T) {
	b := loadBackdrop(80, 25)
	// row beyond height must not panic and must pad to requested width.
	seg := b.segment(999, 0, 5)
	if got := ansiVisibleLen(seg); got != 5 {
		t.Fatalf("oob segment width = %d, want 5", got)
	}
}

// ansiVisibleLen counts runes outside ANSI SGR escape sequences.
func ansiVisibleLen(s string) int {
	return ansi.VisibleLength(s)
}

func TestBackdrop_SegmentRowOOBPadsBlack(t *testing.T) {
	b := loadBackdrop(80, 25)
	seg := b.segment(999, 0, 6)
	if strings.Contains(seg, "░") {
		t.Fatalf("row-OOB in art mode should pad black, not ░: %q", seg)
	}
	if got := ansi.VisibleLength(seg); got != 6 {
		t.Fatalf("row-OOB segment width = %d, want 6", got)
	}
}

func TestBackdrop_SegmentColOOBPadsWidth(t *testing.T) {
	b := loadBackdrop(80, 25)
	seg := b.segment(5, 78, 6) // starts at col 78, runs 6 cols → 4 beyond width 80
	if got := ansi.VisibleLength(seg); got != 6 {
		t.Fatalf("col-OOB segment width = %d, want 6", got)
	}
}

func TestModel_BackdropBuiltAndResized(t *testing.T) {
	m, err := New("testdata")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if m.backdrop == nil {
		t.Fatal("backdrop nil after New")
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m2 := updated.(Model)
	if m2.backdrop == nil || m2.backdrop.width != 100 || m2.backdrop.height != 30 {
		t.Fatalf("backdrop not rebuilt on resize: %+v", m2.backdrop)
	}
}

func TestViewTopMenu_BackgroundFromBackdrop(t *testing.T) {
	m, err := New("testdata")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m2 := mm.(Model)
	artOut := m2.View()

	// Swap to a fallback (art-less) backdrop; the background must change,
	// proving the view pulls its background from m.backdrop rather than a
	// hardcoded ░ fill.
	m2.backdrop = &backdrop{width: 100, height: 30, art: false}
	fbOut := m2.View()

	if artOut == fbOut {
		t.Fatal("top menu background not sourced from m.backdrop (art and fallback render identically)")
	}
}

func TestViewCategoryMenu_BackgroundFromBackdrop(t *testing.T) {
	m, err := New("testdata")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.mode = modeCategoryMenu
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m2 := mm.(Model)
	m2.mode = modeCategoryMenu
	artOut := m2.View()
	m2.backdrop = &backdrop{width: 100, height: 30, art: false}
	fbOut := m2.View()
	if artOut == fbOut {
		t.Fatal("category menu background not sourced from m.backdrop")
	}
}

func TestViewWizardForm_BackgroundFromBackdrop(t *testing.T) {
	m, err := New("testdata")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m2 := mm.(Model)

	// Enter the leaf wizard form the same way the record list's Insert key
	// does (see newLeafWizardModel in wizard_test.go).
	m2.recordType = "v3netleaf"
	m2.mode = modeRecordList
	result, _ := m2.updateRecordList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m2 = result.(Model)
	if m2.mode != modeWizardForm {
		t.Fatalf("expected modeWizardForm, got %v", m2.mode)
	}

	artOut := m2.View()
	m2.backdrop = &backdrop{width: 100, height: 30, art: false}
	fbOut := m2.View()
	if artOut == fbOut {
		t.Fatal("wizard form background not sourced from m.backdrop (art and fallback render identically)")
	}
}

func TestLoadBackdrop_OddMargin(t *testing.T) {
	b := loadBackdrop(81, 25) // (81-80)/2 = 0 → art starts at col 0
	// Row 0 col 0 should be an art cell region (not guaranteed non-space),
	// and the extra column of slack is on the right (col 80) as a black margin.
	if len(b.cells[0]) != 81 {
		t.Fatalf("width = %d, want 81", len(b.cells[0]))
	}
	if c := b.cells[0][80]; c.bg != 0 {
		t.Fatalf("right slack col 80 bg = %d, want 0 (black)", c.bg)
	}
}

func TestNew_NoSplashByDefault(t *testing.T) {
	m, err := New("testdata")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if m.splashActive {
		t.Fatal("New must not enable splash (would break interaction tests)")
	}
}

func TestSplash_KeyDismissesToMenu(t *testing.T) {
	m, err := New("testdata")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m = m.WithStartupSplash()
	if !m.splashActive {
		t.Fatal("WithStartupSplash should set splashActive")
	}
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m2 := mm.(Model)
	if m2.splashActive {
		t.Fatal("keypress should dismiss splash")
	}
	if m2.mode != modeTopMenu {
		t.Fatalf("mode after skip = %v, want topMenu", m2.mode)
	}
}

func TestSplash_TickDismissesToMenu(t *testing.T) {
	m, _ := New("testdata")
	m = m.WithStartupSplash()
	mm, _ := m.Update(splashDoneMsg{})
	if mm.(Model).splashActive {
		t.Fatal("splashDoneMsg should dismiss splash")
	}
}

func TestSplash_ViewIsBackdropOnly(t *testing.T) {
	m, _ := New("testdata")
	m = m.WithStartupSplash()
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m2 := mm.(Model)
	splash := m2.View()
	// Splash shows no menu box: the top-menu box header must be absent.
	if strings.Contains(splash, "ViSiON/3 Configuration") {
		t.Fatal("splash should not render the menu box header")
	}
	// And it must differ from the revealed top menu.
	m2.splashActive = false
	if splash == m2.View() {
		t.Fatal("splash view should differ from top-menu view")
	}
}

// TestViewTopMenu_ArtRowAlignment guards against an off-by-one in the per-line
// row counter: a top-padding row (rendered as a full backdrop line) must equal
// the backdrop's line at that exact absolute row index. At 100x30 the global
// header is row 0 and row 1 is the first top-padding row.
func TestViewTopMenu_ArtRowAlignment(t *testing.T) {
	m, err := New("testdata")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m2 := mm.(Model)
	lines := strings.Split(m2.View(), "\n")
	if len(lines) < 2 {
		t.Fatalf("view has %d lines, want >=2", len(lines))
	}
	if lines[1] != m2.backdrop.line(1) {
		t.Fatal("top-pad row 1 does not match backdrop.line(1): row-counter off-by-one in art mode")
	}
}
