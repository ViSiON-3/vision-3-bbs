package stringeditor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/stringhistory"
)

// TestStateOf covers the ways a row can look blank plus the ways it can
// hold text, which is the distinction #234's investigation got wrong.
func TestStateOf(t *testing.T) {
	m := newTestModel(t)
	m.shippedDefaults = map[string]string{
		"defPrompt":       "factory",
		"pauseString":     "factory pause",
		"quoteTitle":      "factory title",
		"searchNoResults": "factory none",
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
		"quoteTitle":      "",              // set blank, no fallback
		"searchNoResults": "",              // set blank, runtime fills it
		// quoteStartLine and searchFilesPrompt are left out.
	}

	tests := []struct {
		key  string
		want valueState
	}{
		{"defPrompt", stateCustom},
		{"pauseString", stateDefault},
		{"quoteTitle", stateEmpty},
		{"searchNoResults", stateFallback},
		{"quoteStartLine", stateUnset},      // left out, no default at all
		{"searchFilesPrompt", stateDefault}, // left out; the runtime's default prints
		{"_extra3", stateReserved},
	}
	for _, tt := range tests {
		if got := m.stateOf(tt.key); got != tt.want {
			t.Errorf("stateOf(%q) = %v, want %v", tt.key, got, tt.want)
		}
	}
}

// TestStateOfMissingDefault covers a key whose default applies only while it
// is left out: blanking it switches the string off rather than restoring it.
func TestStateOfMissingDefault(t *testing.T) {
	m := newTestModel(t)
	m.values = map[string]string{}
	if got := m.stateOf("quoteTopNet"); got != stateDefault {
		t.Errorf("absent quoteTopNet: stateOf = %v, want stateDefault", got)
	}
	if got, fb := m.previewValue("quoteTopNet"); fb || got != config.StringMissingDefaults["quoteTopNet"] {
		t.Errorf("absent quoteTopNet: previewValue = (%q, %v), want the default", got, fb)
	}

	m.values["quoteTopNet"] = ""
	if got := m.stateOf("quoteTopNet"); got != stateEmpty {
		t.Errorf("blank quoteTopNet: stateOf = %v, want stateEmpty", got)
	}
	if got, fb := m.previewValue("quoteTopNet"); fb || got != "" {
		t.Errorf("blank quoteTopNet: previewValue = (%q, %v), want blank", got, fb)
	}
}

// TestPreviewValueShowsFallback checks that a blank row the runtime fills is
// not drawn as blank, since blank would read as "unused", and that a string
// left out shows its default.
func TestPreviewValueShowsFallback(t *testing.T) {
	m := newTestModel(t)
	m.shippedDefaults = map[string]string{"searchNoResults": "factory none"}
	m.values = map[string]string{"searchNoResults": ""}

	got, isFallback := m.previewValue("searchNoResults")
	if !isFallback || got != config.StringFallbacks["searchNoResults"] {
		t.Errorf("blank searchNoResults preview = (%q, %v), want the runtime fallback, dimmed", got, isFallback)
	}

	delete(m.values, "searchNoResults")
	if got, isFallback := m.previewValue("searchNoResults"); isFallback || got != "factory none" {
		t.Errorf("left out: preview = (%q, %v), want the shipped default", got, isFallback)
	}

	if got, isFallback := m.previewValue("quoteStartLine"); isFallback || got != "" {
		t.Errorf("quoteStartLine has no default: preview = (%q, %v)", got, isFallback)
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
// shipped template does not carry: it takes the key out, and the runtime's
// default prints.
func TestRestoreDefaultUsesRuntimeFallback(t *testing.T) {
	m := newTestModel(t)
	m.shippedDefaults = map[string]string{} // template has nothing for it

	want, ok := config.StringFallbacks["searchNoResults"]
	if !ok {
		t.Fatal("test assumes searchNoResults has a runtime fallback")
	}
	m.values["searchNoResults"] = "mine"
	m.cursor = indexOfKey(t, m, "searchNoResults")

	m = key2(t, m, tea.KeyMsg{Type: tea.KeyF4})
	if m.mode != modeDefaultConfirm {
		t.Fatalf("mode = %v, want default confirm (message: %q)", m.mode, m.message)
	}
	m = key2(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if _, set := m.values["searchNoResults"]; set {
		t.Error("F4 left the key in strings.json")
	}
	if got := m.getValue("searchNoResults"); got != want {
		t.Errorf("value = %q, want the runtime fallback %q", got, want)
	}

	// F4 again: already the default.
	m = key2(t, m, tea.KeyMsg{Type: tea.KeyF4})
	if m.mode != modeNavigate {
		t.Errorf("F4 on a default opened %v", m.mode)
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
	reloaded, err := LoadStrings(m.filePath)
	if err != nil {
		t.Fatalf("LoadStrings: %v", err)
	}
	for _, key := range []string{"Read_Feedback", "someSysopAddition"} {
		if reloaded[key] != m.values[key] {
			t.Errorf("key %q lost on save: got %q, want %q", key, reloaded[key], m.values[key])
		}
	}
}

// TestSaveWritesOnlyOverrides checks a save keeps the sysop's own strings,
// unknown keys and non-string entries, leaves out what prints the default,
// and fingerprints each customised string's default.
func TestSaveWritesOnlyOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "strings.json")
	if err := os.WriteFile(path, []byte(`{"defPrompt": "factory", "pauseString": "mine", "someSysopAddition": "x", "defColor1": 12}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := New(path, map[string]string{"defPrompt": "factory", "pauseString": "factory pause"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["defPrompt"]; ok {
		t.Errorf("default defPrompt written: %s", data)
	}
	for _, k := range []string{"pauseString", "someSysopAddition", "defColor1", config.StringBasesKey} {
		if _, ok := doc[k]; !ok {
			t.Errorf("%s missing: %s", k, data)
		}
	}
	bases := config.StringBases(doc)
	if want := stringhistory.Hash("factory pause"); bases["pauseString"] != want || len(bases) != 1 {
		t.Errorf("bases = %v; want pauseString %s only", bases, want)
	}
}

// TestChangedDefault checks a customised string whose default changed since
// it was set shows as changed, F5 keeps it (refreshing the fingerprint), and
// F4 takes the new default.
func TestChangedDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "strings.json")
	if err := os.WriteFile(path, []byte(`{"pauseString": "mine", "defPrompt": "mine too", "_defaults": {"pauseString": "0000000000000000", "defPrompt": "0000000000000000"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := New(path, map[string]string{"pauseString": "new pause", "defPrompt": "new prompt"})
	if err != nil {
		t.Fatal(err)
	}
	if m.stateOf("pauseString") != stateChanged {
		t.Fatalf("pauseString state = %v; want changed", m.stateOf("pauseString"))
	}
	if m.dirty {
		t.Error("dirty before any change")
	}

	m.cursor = indexOfKey(t, m, "pauseString")
	m = key2(t, m, tea.KeyMsg{Type: tea.KeyF5})
	if m.stateOf("pauseString") != stateCustom || !m.dirty {
		t.Errorf("after F5: state %v, dirty %v; want custom, dirty", m.stateOf("pauseString"), m.dirty)
	}
	m.cursor = indexOfKey(t, m, "defPrompt")
	m = key2(t, m, tea.KeyMsg{Type: tea.KeyF4})
	m = key2(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["defPrompt"]; ok {
		t.Errorf("defPrompt kept after F4: %s", data)
	}
	if bases := config.StringBases(doc); bases["pauseString"] != stringhistory.Hash("new pause") || len(bases) != 1 {
		t.Errorf("bases = %v; want pauseString's refreshed to the new default", bases)
	}
}

// TestNewTreatsOlderShippedTextAsDefault checks the editor agrees with
// doctor --fix: text a key shipped with in an earlier release is the
// default, not the sysop's, and a save leaves it out.
func TestNewTreatsOlderShippedTextAsDefault(t *testing.T) {
	old := config.StringSuperseded["chatHelp"][1]
	data, _ := json.Marshal(map[string]string{"chatHelp": old})
	path := filepath.Join(t.TempDir(), "strings.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := New(path, map[string]string{"chatHelp": "current help"})
	if err != nil {
		t.Fatal(err)
	}
	if _, set := m.values["chatHelp"]; set || m.stateOf("chatHelp") != stateDefault {
		t.Errorf("older shipped chatHelp: set %v, state %v; want left out, default", set, m.stateOf("chatHelp"))
	}
}

// TestF3F4Guards checks F3 with nothing to revert and F4 on a string with no
// default at all say so rather than opening a dialog.
func TestF3F4Guards(t *testing.T) {
	m := newTestModel(t)
	m.cursor = indexOfKey(t, m, "quoteStartLine")
	m = key2(t, m, tea.KeyMsg{Type: tea.KeyF3})
	if m.mode != modeNavigate || m.message != "Nothing to revert" {
		t.Errorf("F3 on an untouched string: mode %v, message %q", m.mode, m.message)
	}
	m.values["quoteStartLine"] = "mine"
	m = key2(t, m, tea.KeyMsg{Type: tea.KeyF4})
	if m.mode != modeNavigate || m.values["quoteStartLine"] != "mine" {
		t.Errorf("F4 with no default: mode %v, value %q; want the value kept", m.mode, m.values["quoteStartLine"])
	}
}
