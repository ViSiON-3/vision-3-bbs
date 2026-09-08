package stringeditor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// shippedTemplate loads templates/configs/strings.json, the factory values the
// editor must be able to display and round-trip without damage.
func shippedTemplate(t *testing.T) map[string]string {
	t.Helper()
	path := filepath.Join("..", "..", "templates", "configs", "strings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var values map[string]string
	if err := json.Unmarshal(data, &values); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return values
}

// TestEscapeForEdit covers the escaped representation of each control character.
func TestEscapeForEdit(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain text unchanged", "Hello, world!", "Hello, world!"},
		{"pipe codes unchanged", "|10Online Nodes:|07", "|10Online Nodes:|07"},
		{"format verbs unchanged", "Node %d: %-20s", "Node %d: %-20s"},
		{"carriage return", "a\rb", `a\rb`},
		{"line feed", "a\nb", `a\nb`},
		{"crlf", "\r\n|12Online Nodes:|07\r\n", `\r\n|12Online Nodes:|07\r\n`},
		{"tab", "a\tb", `a\tb`},
		{"escape", "a\x1bb", `a\eb`},
		{"backslash doubled", `C:\path`, `C:\\path`},
		{"other control as hex", "a\x01b", `a\x01b`},
		{"del as hex", "a\x7fb", `a\x7Fb`},
		{"non-ascii passes through", "café ✓", "café ✓"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EscapeForEdit(tt.in); got != tt.want {
				t.Errorf("EscapeForEdit(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestUnescapeFromEditRejectsBadInput checks that a malformed sequence is
// reported rather than silently guessed at.
func TestUnescapeFromEditRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"unknown escape", `a\qb`},
		{"trailing backslash", `abc\`},
		{"short hex", `a\x4`},
		{"non-hex digits", `a\xZZb`},
		// \x is for control characters only; see below.
		{"printable hex", `a\x41b`},
		{"high byte", `caf\x80`},
		{"top of range", `a\xFFb`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnescapeFromEdit(tt.in)
			if err == nil {
				t.Errorf("UnescapeFromEdit(%q) = %q, want an error", tt.in, got)
			}
		})
	}
}

// TestHexEscapeRejectsNonControlBytes covers a way the codec could lose data.
//
// strings.json is UTF-8, and a byte of 0x80 or above is not valid UTF-8 on its
// own. Writing one would come back as U+FFFD on the next load, so the value
// would not survive the round trip this codec exists to guarantee. Only control
// characters are accepted, which is all EscapeForEdit ever emits.
func TestHexEscapeRejectsNonControlBytes(t *testing.T) {
	// Control characters are accepted.
	for _, in := range []string{`a\x01b`, `a\x1Fb`, `a\x7Fb`, `a\x00b`} {
		if _, err := UnescapeFromEdit(in); err != nil {
			t.Errorf("UnescapeFromEdit(%q) = %v, want it accepted", in, err)
		}
	}
	// Anything that would produce invalid UTF-8, or that should simply be
	// typed literally, is refused.
	for _, in := range []string{`a\x20b`, `a\x41b`, `caf\x80`, `a\xC3b`, `a\xFFb`} {
		if got, err := UnescapeFromEdit(in); err == nil {
			t.Errorf("UnescapeFromEdit(%q) = %q, want an error", in, got)
		}
	}
}

// TestUnescapeOutputIsAlwaysValidUTF8 is the property behind that rule: no
// input the codec accepts may produce a string JSON cannot store unchanged.
func TestUnescapeOutputIsAlwaysValidUTF8(t *testing.T) {
	inputs := []string{
		`plain`, `\r\n\t\e`, `\x00\x01\x1F\x7F`, `caf\xC3`, `\\`,
		"café ✓ 日本語 🚀", `|15Node %d|07: %s\r\n`,
	}
	for _, in := range inputs {
		got, err := UnescapeFromEdit(in)
		if err != nil {
			continue // rejected inputs cannot corrupt anything
		}
		if !utf8.ValidString(got) {
			t.Errorf("UnescapeFromEdit(%q) produced invalid UTF-8: %q", in, got)
			continue
		}
		// And it must survive the JSON round trip the editor performs on save.
		data, err := json.Marshal(map[string]string{"k": got})
		if err != nil {
			t.Errorf("marshalling %q: %v", got, err)
			continue
		}
		var back map[string]string
		if err := json.Unmarshal(data, &back); err != nil {
			t.Errorf("unmarshalling %q: %v", data, err)
			continue
		}
		if back["k"] != got {
			t.Errorf("value changed by a save/load round trip:\n got %q\nwant %q", back["k"], got)
		}
	}
}

// TestEscapeRoundTripsShippedValues is the core data-safety guarantee: every
// factory string must survive being escaped for the editor and unescaped again.
func TestEscapeRoundTripsShippedValues(t *testing.T) {
	for key, want := range shippedTemplate(t) {
		got, err := UnescapeFromEdit(EscapeForEdit(want))
		if err != nil {
			t.Errorf("%s: unescape after escape: %v", key, err)
			continue
		}
		if got != want {
			t.Errorf("%s: round trip = %q, want %q", key, got, want)
		}
	}
}

// TestEscapedValuesAreSingleLine checks that no escaped value can break a
// single-line text input or a one-row list entry.
func TestEscapedValuesAreSingleLine(t *testing.T) {
	for key, value := range shippedTemplate(t) {
		escaped := EscapeForEdit(value)
		if strings.ContainsAny(escaped, "\r\n\t\x1b") {
			t.Errorf("%s: escaped form still contains a control character: %q", key, escaped)
		}
	}
}
