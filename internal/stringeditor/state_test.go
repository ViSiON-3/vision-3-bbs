package stringeditor

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// TestStateOf covers the four ways a row can look blank plus the two ways it
// can hold text, which is the distinction #234's investigation got wrong.
func TestStateOf(t *testing.T) {
	m := newTestModel(t)
	m.shippedDefaults = map[string]string{
		"defPrompt":   "factory",
		"pauseString": "factory pause",
	}
	// searchNoResults has a runtime fallback; quoteTitle does not.
	if _, ok := config.StringFallbacks["searchNoResults"]; !ok {
		t.Fatal("test assumes searchNoResults has a runtime fallback")
	}
	if _, ok := config.StringFallbacks["quoteTitle"]; ok {
		t.Fatal("test assumes quoteTitle has no runtime fallback")
	}

	m.values = map[string]string{
		"defPrompt":       "mine",          // differs from the shipped default
		"pauseString":     "factory pause", // matches the shipped default
		"quoteTitle":      "",              // present, empty, no fallback
		"searchNoResults": "",              // present, empty, runtime fills it
		// quoteStartLine and searchFilesPrompt are absent entirely.
	}

	tests := []struct {
		key  string
		want valueState
	}{
		{"defPrompt", stateCustom},
		{"pauseString", stateDefault},
		{"quoteTitle", stateEmpty},
		{"searchNoResults", stateFallback},
		{"quoteStartLine", stateUnset},       // absent, no fallback
		{"searchFilesPrompt", stateFallback}, // absent, but runtime fills it
		{"_extra3", stateReserved},
	}
	for _, tt := range tests {
		if got := m.stateOf(tt.key); got != tt.want {
			t.Errorf("stateOf(%q) = %v, want %v", tt.key, got, tt.want)
		}
	}
}

// TestPreviewValueShowsFallback checks that a row the runtime fills is not
// drawn as blank, since blank would read as "unused".
func TestPreviewValueShowsFallback(t *testing.T) {
	m := newTestModel(t)
	m.values = map[string]string{}

	got, isFallback := m.previewValue("searchNoResults")
	if !isFallback {
		t.Error("searchNoResults should preview as a fallback")
	}
	if got != config.StringFallbacks["searchNoResults"] {
		t.Errorf("preview = %q, want the runtime fallback", got)
	}

	if _, isFallback := m.previewValue("quoteStartLine"); isFallback {
		t.Error("quoteStartLine has no fallback and must not claim one")
	}
}

// TestReservedFilter covers hiding and showing the placeholder entries while
// keeping catalog numbering stable.
func TestReservedFilter(t *testing.T) {
	m := newTestModel(t)

	if m.showReserved {
		t.Fatal("reserved entries should be hidden by default")
	}
	for _, e := range m.entries {
		if isReservedKey(e.Key) {
			t.Fatalf("reserved entry %q listed while hidden", e.Key)
		}
	}
	hidden := len(m.entries)
	if hidden >= len(m.catalog) {
		t.Fatalf("filter removed nothing: %d listed of %d catalog", hidden, len(m.catalog))
	}

	// Numbering follows the catalog, not the filtered position. The first
	// reserved entry is at catalog index 22, so entries before it line up
	// either way and only the ones after it prove anything.
	if m.entries[0].Number != 1 {
		t.Errorf("first entry Number = %d, want 1", m.entries[0].Number)
	}
	gaps := 0
	for i, e := range m.entries {
		if e.Number != i+1 {
			gaps++
		}
		if isReservedKey(e.Key) {
			t.Fatalf("reserved entry %q survived the filter", e.Key)
		}
	}
	if gaps == 0 {
		t.Error("every listed entry's Number equals its listed position, so the " +
			"numbers are positional and would shift when reserved entries are hidden")
	}
	if want := len(m.catalog) - hidden; gaps != len(m.entries)-catalogIndexOfFirstReserved(t, m) {
		t.Logf("%d entries carry a shifted number (%d reserved hidden)", gaps, want)
	}

	// Ctrl-R reveals them, and the cursor stays on the same string.
	m.cursor = 30
	key := m.entries[m.cursor].Key
	m = key2(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if !m.showReserved {
		t.Fatal("ctrl+r did not enable reserved entries")
	}
	if len(m.entries) != len(m.catalog) {
		t.Errorf("listed %d entries, want the full catalog of %d", len(m.entries), len(m.catalog))
	}
	if got := m.entries[m.cursor].Key; got != key {
		t.Errorf("cursor moved to %q, want it kept on %q", got, key)
	}
	if m.page != m.cursor/m.pageSize {
		t.Errorf("page %d does not show cursor %d", m.page, m.cursor)
	}

	// And toggling back hides them again.
	m = key2(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if m.showReserved || len(m.entries) != hidden {
		t.Errorf("toggle back listed %d entries, want %d", len(m.entries), hidden)
	}
}

// catalogIndexOfFirstReserved returns the catalog position of the first
// reserved entry, which is where listed numbering starts to diverge.
func catalogIndexOfFirstReserved(t *testing.T, m Model) int {
	t.Helper()
	for i, e := range m.catalog {
		if isReservedKey(e.Key) {
			return i
		}
	}
	t.Fatal("the catalog has no reserved entries, so the filter proves nothing")
	return 0
}

// TestRestoreDefaultUsesRuntimeFallback checks F4 works for a string the
// shipped template does not carry.
func TestRestoreDefaultUsesRuntimeFallback(t *testing.T) {
	m := newTestModel(t)
	m.shippedDefaults = map[string]string{} // template has nothing for it

	want, ok := config.StringFallbacks["searchNoResults"]
	if !ok {
		t.Fatal("test assumes searchNoResults has a runtime fallback")
	}
	m.cursor = indexOfKey(t, m, "searchNoResults")

	m = key2(t, m, tea.KeyMsg{Type: tea.KeyF4})
	if m.mode != modeDefaultConfirm {
		t.Fatalf("mode = %v, want default confirm (message: %q)", m.mode, m.message)
	}
	m = key2(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if got := m.values["searchNoResults"]; got != want {
		t.Errorf("value = %q, want the runtime fallback %q", got, want)
	}
}

// TestSavePreservesUnknownKeys checks that a legacy or hand-added key survives
// a save even though the editor has no metadata for it.
func TestSavePreservesUnknownKeys(t *testing.T) {
	m := newTestModel(t)
	m.values["Read_Feedback"] = "Read Feedback?"
	m.values["someSysopAddition"] = "|15hello"

	if err := SaveStrings(m.filePath, m.values); err != nil {
		t.Fatalf("SaveStrings: %v", err)
	}
	reloaded, err := LoadStrings(m.filePath, nil)
	if err != nil {
		t.Fatalf("LoadStrings: %v", err)
	}
	for _, key := range []string{"Read_Feedback", "someSysopAddition"} {
		if reloaded[key] != m.values[key] {
			t.Errorf("key %q lost on save: got %q, want %q", key, reloaded[key], m.values[key])
		}
	}
}
