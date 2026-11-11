package stringeditor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// TestPickerInsertsAtCursor opens the picker from edit mode, chooses a colour
// and checks the codes land at the cursor with the cursor after them.
func TestPickerInsertsAtCursor(t *testing.T) {
	m := newTestModel(t)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ab")})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyLeft}) // cursor between a and b
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF2})
	if m.mode != modePicker {
		t.Fatalf("after F2 mode = %v, want modePicker", m.mode)
	}
	// Default choice is white with no background.
	if got := m.pickerCode(); got != "|15" {
		t.Fatalf("initial code = %q, want |15", got)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyLeft}) // |14
	m = key(t, m, tea.KeyMsg{Type: tea.KeyDown}) // background row
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRight})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRight}) // -- -> B0 -> B1
	if got := m.pickerCode(); got != "|14|B1" {
		t.Fatalf("code = %q, want |14|B1", got)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeEdit {
		t.Fatalf("after Enter mode = %v, want modeEdit", m.mode)
	}
	if got := m.textInput.Value(); got != "a|14|B1b" {
		t.Errorf("value = %q, want a|14|B1b", got)
	}
	if got := m.textInput.Position(); got != len("a|14|B1") {
		t.Errorf("cursor = %d, want %d", got, len("a|14|B1"))
	}
	// Accepting the edit stores the codes unchanged.
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.values[m.entries[m.cursor].Key]; got != "a|14|B1b" {
		t.Errorf("stored = %q", got)
	}
}

// TestPickerBeforeDigit checks a background code never swallows a digit that
// follows the cursor: a bright foreground goes last, and a dark one with a
// background is refused with the input unchanged.
func TestPickerBeforeDigit(t *testing.T) {
	for _, d := range "012345" {
		m := newTestModel(t)
		m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(string(d))})
		m = key(t, m, tea.KeyMsg{Type: tea.KeyHome})
		m.pick.FG, m.pick.BG = 15, 1
		m = key(t, m, tea.KeyMsg{Type: tea.KeyF2})
		m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		want := "|B1|15" + string(d)
		if got := m.textInput.Value(); got != want {
			t.Errorf("before %c: value %q, want %q", d, got, want)
		}
		out := string(ansi.ReplacePipeCodes([]byte(m.textInput.Value())))
		if !strings.HasSuffix(out, "m"+string(d)) || strings.Contains(out, "\x1b[10") {
			t.Errorf("before %c: rendered %q, digit swallowed", d, out)
		}
	}
	// 6-9 are never a bright background, so the usual order stands.
	m := newTestModel(t)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("7")})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyHome})
	m.pick.FG, m.pick.BG = 15, 1
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF2})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.textInput.Value(); got != "|15|B17" {
		t.Errorf("before 7: value %q", got)
	}
	// A dark foreground with a background before a digit is refused.
	m = newTestModel(t)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("0")})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyHome})
	m.pick.FG, m.pick.BG = 4, 1
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF2})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeEdit || m.textInput.Value() != "0" || !strings.Contains(m.editErr, "|B10") {
		t.Errorf("dark fg before digit: mode %v value %q err %q", m.mode, m.textInput.Value(), m.editErr)
	}
}

// TestPickerCodeSurvivesRendering checks the inserted pair keeps both colours
// once the BBS expands it: a dark foreground resets attributes, so it has to
// come before the background.
func TestPickerCodeSurvivesRendering(t *testing.T) {
	m := newTestModel(t)
	m.pick.FG, m.pick.BG = 4, 2 // |04 expands to ESC[0;31m
	out := string(ansi.ReplacePipeCodes([]byte(m.pickerCode() + "x")))
	if !strings.HasSuffix(out, "\x1b[42mx") {
		t.Errorf("rendered %q: background lost", out)
	}
}

// TestBackgroundSwatchesMatchRuntime checks the editor's background colours
// follow the codes the BBS emits (|B1 red, |B4 blue), not the DOS order of
// the foreground codes.
func TestBackgroundSwatchesMatchRuntime(t *testing.T) {
	if dosBgColors[1] != dosColors[dosRed] || dosBgColors[4] != dosColors[dosBlue] {
		t.Errorf("B1=%q B4=%q; want red %q and blue %q", dosBgColors[1], dosBgColors[4], dosColors[dosRed], dosColors[dosBlue])
	}
	if dosBgColors[0] != dosColors[dosBlack] || dosBgColors[7] != dosColors[dosLightGray] {
		t.Errorf("B0=%q B7=%q", dosBgColors[0], dosBgColors[7])
	}
}

// TestPickerCancelKeepsValue checks Esc returns to the input unchanged, and
// that the choice made is remembered for the next opening.
func TestPickerCancelKeepsValue(t *testing.T) {
	m := newTestModel(t)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF2})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRight}) // |00
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.mode != modeEdit || m.textInput.Value() != "x" {
		t.Fatalf("after Esc: mode %v value %q", m.mode, m.textInput.Value())
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF2})
	if got := m.pickerCode(); got != "|00" {
		t.Errorf("choice not kept: %q", got)
	}
}

// TestPickerWraps checks the rows wrap at both ends, including the
// no-background cell.
func TestPickerWraps(t *testing.T) {
	m := newTestModel(t)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF2})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRight}) // 15 -> 0
	if m.pick.FG != 0 {
		t.Errorf("fg after wrap = %d, want 0", m.pick.FG)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyLeft}) // none -> B7
	if m.pick.BG != 7 {
		t.Errorf("bg after wrap = %d, want 7", m.pick.BG)
	}
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRight}) // B7 -> none
	if m.pick.BG != pickerNoBG {
		t.Errorf("bg after wrap = %d, want none", m.pick.BG)
	}
}

// TestPickerView checks the dialog is drawn over the edit screen with the
// sample in the chosen codes, and that the edit row is still shown under it.
func TestPickerView(t *testing.T) {
	m := newTestModel(t)
	m = resize(t, m, 80, 25)
	m = key(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = key(t, m, tea.KeyMsg{Type: tea.KeyF2})
	view := m.View()
	for _, want := range []string{"Insert Colour", "|15 ", pickerSample, "Enter insert", helpTextEdit} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q", want)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if w := visualLen(line); w != 80 {
			t.Errorf("row width %d, want 80: %q", w, line)
		}
	}
}
