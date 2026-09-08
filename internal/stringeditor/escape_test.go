package stringeditor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
