package stringeditor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// newShippedModel builds a Model over a copy of the shipped template, so the
// tests exercise the real multiline values rather than synthetic ones.
func newShippedModel(t *testing.T) Model {
	t.Helper()
	values := shippedTemplate(t)
	data, err := json.Marshal(values)
	if err != nil {
		t.Fatalf("marshalling template: %v", err)
	}
	path := filepath.Join(t.TempDir(), "strings.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writing test strings.json: %v", err)
	}
	m, err := New(path, values)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

// indexOfKey returns the entry index for a metadata key.
func indexOfKey(t *testing.T, m Model, key string) int {
	t.Helper()
	for i, e := range m.entries {
		if e.Key == key {
			return i
		}
	}
	t.Fatalf("key %q not found in metadata", key)
	return -1
}

// resize applies a window size and returns the updated Model.
func resize(t *testing.T, m Model, w, h int) Model {
	t.Helper()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return asModel(t, updated)
}

// TestNoOpEditPreservesMultilineValue covers issue #234: pressing F1 and then
// Enter without typing anything must leave the stored value byte-identical,
// including its carriage returns and line feeds.
func TestNoOpEditPreservesMultilineValue(t *testing.T) {
	m := newShippedModel(t)

	for _, key := range []string{
		"pageOnlineNodesHeader", // leading and trailing CRLF
		"pageNodeListEntry",     // trailing CRLF plus format verbs
		"newUserAccountCreated", // several embedded CRLFs
		"cfgColorSelectPrompt",  // consecutive CRLFs
		"scanInvalidDate",       // long value with CRLFs
	} {
		t.Run(key, func(t *testing.T) {
			want := m.values[key]
			if want == "" {
				t.Fatalf("%s is empty in the shipped template", key)
			}

			m := m
			m.cursor = indexOfKey(t, m, key)
			m.page = m.cursor / m.pageSize

			m = key2(t, m, tea.KeyMsg{Type: tea.KeyF1})
			if m.mode != modeEdit {
				t.Fatalf("mode = %v, want edit after F1", m.mode)
			}
			m = key2(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			if m.mode != modeNavigate {
				t.Fatalf("mode = %v, want navigate after Enter", m.mode)
			}
			if got := m.values[key]; got != want {
				t.Errorf("value changed by a no-op edit:\n got %q\nwant %q", got, want)
			}
			if m.dirty {
				t.Error("dirty = true after a no-op edit")
			}
		})
	}
}

// key2 sends a key message and returns the updated Model.
func key2(t *testing.T, m Model, msg tea.KeyMsg) Model {
	t.Helper()
	updated, _ := m.Update(msg)
	return asModel(t, updated)
}

// TestEditRejectsMalformedEscape checks that a bad escape keeps the sysop in
// the input with their text intact instead of writing a guess to disk.
func TestEditRejectsMalformedEscape(t *testing.T) {
	m := newShippedModel(t)
	m = resize(t, m, 80, 25)
	m.cursor = indexOfKey(t, m, "pageOnlineNodesHeader")
	want := m.values["pageOnlineNodesHeader"]

	m = key2(t, m, tea.KeyMsg{Type: tea.KeyF1})
	m.textInput.SetValue(`\q broken`)
	m = key2(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.mode != modeEdit {
		t.Errorf("mode = %v, want to stay in edit mode", m.mode)
	}
	if m.editErr == "" {
		t.Error("editErr is empty, want an escape-syntax error")
	}
	if got := m.values["pageOnlineNodesHeader"]; got != want {
		t.Errorf("value = %q, want it unchanged as %q", got, want)
	}
	if m.textInput.Value() != `\q broken` {
		t.Errorf("input = %q, want the sysop's text retained", m.textInput.Value())
	}
	// The error clears once they keep typing.
	m = key2(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	if m.editErr != "" {
		t.Errorf("editErr = %q, want it cleared on further input", m.editErr)
	}
}

// TestLongValueSurvivesEdit checks that a value longer than the old 200-char
// input limit is neither truncated on prefill nor on save.
func TestLongValueSurvivesEdit(t *testing.T) {
	m := newShippedModel(t)
	long := "\r\n|15" + strings.Repeat("long custom banner text ", 30) + "%s|07\r\n"
	m.values["pageOnlineNodesHeader"] = long
	m.cursor = indexOfKey(t, m, "pageOnlineNodesHeader")

	m = key2(t, m, tea.KeyMsg{Type: tea.KeyF1})
	if got := m.textInput.Value(); got != EscapeForEdit(long) {
		t.Fatalf("prefill truncated: got %d chars, want %d", len(got), len(EscapeForEdit(long)))
	}
	m = key2(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.values["pageOnlineNodesHeader"]; got != long {
		t.Errorf("value truncated: got %d bytes, want %d", len(got), len(long))
	}
}

// TestViewRowsAreExact checks that the rendered screen occupies exactly the
// terminal's rows and columns at every audited size, with no raw carriage
// return or line feed leaking out of a string value.
func TestViewRowsAreExact(t *testing.T) {
	sizes := []struct{ w, h int }{
		{80, 25}, {100, 30}, {120, 45}, {160, 60}, {60, 15}, {200, 100},
	}
	for _, size := range sizes {
		t.Run(sizeName(size.w, size.h), func(t *testing.T) {
			m := newShippedModel(t)
			m = resize(t, m, size.w, size.h)

			// Walk every page so all 399 values are rendered at least once.
			for page := 0; page < m.numPages; page++ {
				m.page = page
				m.cursor = page * m.pageSize
				if m.cursor >= len(m.entries) {
					m.cursor = len(m.entries) - 1
				}
				checkView(t, m, page)

				// The same must hold with the inline editor open on the row.
				edit := key2(t, m, tea.KeyMsg{Type: tea.KeyF1})
				if edit.mode == modeEdit {
					checkView(t, edit, page)
				}
			}
		})
	}
}

// checkView asserts that a rendered screen fills exactly the terminal's rows
// and never exceeds its columns.
func checkView(t *testing.T, m Model, page int) {
	t.Helper()
	out := m.View()
	if strings.Contains(out, "\r") {
		t.Fatalf("page %d: view contains a raw carriage return", page)
	}
	lines := strings.Split(out, "\n")
	if len(lines) != m.height {
		t.Fatalf("page %d: view has %d rows, want the full terminal height %d",
			page, len(lines), m.height)
	}
	for i, line := range lines {
		if w := visualLen(line); w != m.width {
			t.Fatalf("page %d row %d: width %d, want exactly %d",
				page, i, w, m.width)
		}
	}
}

// TestDialogGeometry checks that a confirmation dialog stays centered on the
// screen and never disturbs the row or column count, at every audited size.
func TestDialogGeometry(t *testing.T) {
	for _, size := range []struct{ w, h int }{
		{80, 25}, {100, 30}, {120, 45}, {160, 60}, {60, 15}, {200, 100},
	} {
		for _, mode := range []editorMode{modeAbortConfirm, modeRevertConfirm, modeDefaultConfirm} {
			m := newShippedModel(t)
			m = resize(t, m, size.w, size.h)
			m.mode = mode

			out := m.View()
			lines := strings.Split(out, "\n")
			if len(lines) != m.height {
				t.Fatalf("%dx%d mode %v: %d rows, want %d", size.w, size.h, mode, len(lines), m.height)
			}
			for i, line := range lines {
				if w := visualLen(line); w != m.width {
					t.Fatalf("%dx%d mode %v row %d: width %d, want exactly %d",
						size.w, size.h, mode, i, w, m.width)
				}
			}
			// The box top border must land on the vertically centered row.
			wantRow := (m.height - 5) / 2
			if !strings.Contains(stripANSI(lines[wantRow]), "╔") {
				t.Errorf("%dx%d mode %v: no dialog border on centered row %d",
					size.w, size.h, mode, wantRow)
			}
		}
	}
}

// TestViewShowsControlCharactersEscaped checks the preview keeps a multiline
// value on one row by drawing its control characters, not executing them.
func TestViewShowsControlCharactersEscaped(t *testing.T) {
	m := newShippedModel(t)
	m = resize(t, m, 100, 30)
	idx := indexOfKey(t, m, "pageOnlineNodesHeader")
	m.cursor = idx
	m.page = idx / m.pageSize

	row := m.renderItem(idx, m.panelWidth())
	if strings.ContainsAny(row, "\r\n") {
		t.Fatalf("rendered row contains a raw control character: %q", row)
	}
	if !strings.Contains(stripANSI(row), `\r\n`) {
		t.Errorf("rendered row does not show the escaped CRLF: %q", stripANSI(row))
	}
}

// TestResizeKeepsSelectionVisible covers repeated small/large transitions: the
// cursor must always land on the page the view is about to draw.
func TestResizeKeepsSelectionVisible(t *testing.T) {
	m := newShippedModel(t)
	m.cursor = 250

	for _, size := range []struct{ w, h int }{
		{80, 25}, {160, 60}, {80, 25}, {200, 100}, {80, 25}, {120, 45},
	} {
		m = resize(t, m, size.w, size.h)
		if m.cursor/m.pageSize != m.page {
			t.Fatalf("at %dx%d: cursor %d is on page %d, view shows page %d",
				size.w, size.h, m.cursor, m.cursor/m.pageSize, m.page)
		}
		if m.page >= m.numPages {
			t.Fatalf("at %dx%d: page %d is past the last page %d",
				size.w, size.h, m.page, m.numPages-1)
		}
	}
}

// TestPageSizeFor documents the adaptive page size and its bounds.
func TestPageSizeFor(t *testing.T) {
	tests := []struct{ height, want int }{
		{10, minItemsPerPage}, // below the supported minimum
		{25, minItemsPerPage}, // the 80x25 baseline
		{30, 24},
		{45, 39},
		{60, 54},
		{100, maxItemsPerPage}, // capped
	}
	for _, tt := range tests {
		if got := pageSizeFor(tt.height); got != tt.want {
			t.Errorf("pageSizeFor(%d) = %d, want %d", tt.height, got, tt.want)
		}
	}
}

// sizeName formats a terminal size for a subtest name.
func sizeName(w, h int) string {
	return itoa(w) + "x" + itoa(h)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// stripANSI removes escape sequences so assertions can look at plain text.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
