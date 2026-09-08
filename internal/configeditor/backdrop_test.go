package configeditor

import (
	"bytes"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestModel_BackdropArtStableAcrossResize(t *testing.T) {
	m, err := New("testdata")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(m.backdropArt) == 0 {
		t.Fatal("New should pick a backdrop art")
	}
	chosen := m.backdropArt
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := mm.(Model)
	if !bytes.Equal(m2.backdropArt, chosen) {
		t.Fatal("backdrop art must not change on resize")
	}
	if m2.backdrop == nil || m2.backdrop.Width() != 120 || m2.backdrop.Height() != 40 {
		t.Fatalf("backdrop not rebuilt at new size: %+v", m2.backdrop)
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
	if m2.backdrop == nil || m2.backdrop.Width() != 100 || m2.backdrop.Height() != 30 {
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
	m2.backdrop = loadBackdropFrom(nil, 100, 30)
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
	m2.backdrop = loadBackdropFrom(nil, 100, 30)
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
	m2.backdrop = loadBackdropFrom(nil, 100, 30)
	fbOut := m2.View()
	if artOut == fbOut {
		t.Fatal("wizard form background not sourced from m.backdrop (art and fallback render identically)")
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
	if lines[1] != m2.backdrop.Line(1) {
		t.Fatal("top-pad row 1 does not match backdrop.line(1): row-counter off-by-one in art mode")
	}
}
