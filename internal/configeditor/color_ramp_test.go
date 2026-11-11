package configeditor

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// openRamp opens Theme -> Toasts -> Toast Fade Colors in the ramp editor.
func openRamp(t *testing.T) Model {
	t.Helper()
	m, _ := newThemeModel(t)
	m = press(t, m, "C", "3")
	if m.mode != modeSysConfigEdit || m.sysFields[m.editField].Label != "Fade Colors" {
		t.Fatalf("mode=%v field=%q", m.mode, m.sysFields[m.editField].Label)
	}
	m = press(t, m, "enter")
	if m.mode != modeColorRamp {
		t.Fatalf("mode = %v, want the ramp editor", m.mode)
	}
	return m
}

// The ramp opens on the theme's colours; steps are chosen with the shared
// colour picker, added, moved and removed; S saves the field and Esc
// leaves it as it was.
func TestColorRampEdit(t *testing.T) {
	m := openRamp(t)
	if got := strings.Join(intsToStr(m.ramp.steps), ","); got != "8,7,15" {
		t.Fatalf("opened on %s", got)
	}
	wantScreen(t, m, "Toast Fade Colors", "Steps:", "Preview:")

	// Step 2 (07) becomes 09 through the picker.
	m = press(t, m, "right", "enter")
	if !m.ramp.picking || m.ramp.pick.FG != 7 {
		t.Fatalf("picker open=%v on %d", m.ramp.picking, m.ramp.pick.FG)
	}
	m = press(t, m, "right", "right", "enter")
	// Insert a copy after it, move it first, then drop the last step.
	m = press(t, m, "insert", "ctrl+left", "ctrl+left", "end")
	m = press(t, m, "right", "right", "right", "delete")
	if got := strings.Join(intsToStr(m.ramp.steps), ","); got != "9,8,9" {
		t.Fatalf("steps %s, want 9,8,9", got)
	}

	// Esc throws the edit away.
	m = press(t, m, "esc")
	if m.mode != modeSysConfigEdit || m.configs.Theme.ToastFadeColors != "8,7,15" || m.dirty {
		t.Fatalf("esc: mode=%v colors=%q dirty=%v", m.mode, m.configs.Theme.ToastFadeColors, m.dirty)
	}

	// S keeps it.
	m = press(t, m, "enter", "right", "enter", "left", "enter", "s")
	if m.mode != modeSysConfigEdit || m.configs.Theme.ToastFadeColors != "8,6,15" || !m.dirty {
		t.Fatalf("save: mode=%v colors=%q dirty=%v", m.mode, m.configs.Theme.ToastFadeColors, m.dirty)
	}
}

// A ramp holds 1 to 8 steps, and D puts the default back.
func TestColorRampLimitsAndDefault(t *testing.T) {
	m := openRamp(t)
	for range 10 {
		m = press(t, m, "insert")
	}
	if len(m.ramp.steps) != 8 || !strings.Contains(m.ramp.errMsg, "At most 8") {
		t.Errorf("%d steps, message %q", len(m.ramp.steps), m.ramp.errMsg)
	}
	for range 10 {
		m = press(t, m, "delete")
	}
	if len(m.ramp.steps) != 1 || !strings.Contains(m.ramp.errMsg, "at least one") {
		t.Errorf("%d steps, message %q", len(m.ramp.steps), m.ramp.errMsg)
	}
	m = press(t, m, "d")
	if got := strings.Join(intsToStr(m.ramp.steps), ","); got != "8,7,15" {
		t.Errorf("default gave %s", got)
	}
}

// The preview plays the ramp in, holds, plays it out reversed, then shows
// nothing, and loops; a tick from an earlier opening does nothing.
func TestColorRampPreview(t *testing.T) {
	step := 100 * time.Millisecond
	steps := []int{8, 7, 15}
	hold, gap := int(rampHold/step), int(rampGap/step)
	var seq []int
	for f := 0; f < 2*len(steps)+hold+gap; f++ {
		c, shown := rampFrame(steps, f, step)
		if !shown {
			c = -1
		}
		seq = append(seq, c)
	}
	want := append([]int{8, 7, 15}, make([]int, hold)...)
	for i := range hold {
		want[3+i] = 15
	}
	want = append(want, 15, 7, 8)
	for range gap {
		want = append(want, -1)
	}
	if strings.Join(intsToStr(seq), ",") != strings.Join(intsToStr(want), ",") {
		t.Errorf("frames %v, want %v", seq, want)
	}
	if c, _ := rampFrame(steps, len(want), step); c != 8 {
		t.Errorf("did not loop: %d", c)
	}

	m := openRamp(t)
	got, _ := m.Update(rampTickMsg{gen: m.ramp.gen - 1})
	if got.(Model).ramp.frame != 0 {
		t.Error("a stale tick moved the preview")
	}
	got, cmd := m.Update(rampTickMsg{gen: m.ramp.gen})
	if got.(Model).ramp.frame != 1 || cmd == nil {
		t.Error("a current tick did not move the preview and schedule the next")
	}
}

// The dialog fits an 80-column screen, open or picking.
func TestColorRampFits80(t *testing.T) {
	m := openRamp(t)
	for _, mm := range []Model{m, press(t, m, "enter")} {
		for i, line := range strings.Split(mm.View(), "\n") {
			if w := lipgloss.Width(line); w > 80 {
				t.Errorf("line %d is %d wide", i, w)
			}
		}
	}
}

func intsToStr(ns []int) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = strconv.Itoa(n)
	}
	return out
}
