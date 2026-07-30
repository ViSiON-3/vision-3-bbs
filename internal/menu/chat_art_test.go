package menu

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// chatArtReplace fills fixed-width @NAME####@ placeholders in CHATHEADER.ANS.
// The NET value is a V3Net leaf network name, which is not ASCII-gated, so the
// placeholder must be filled by columns rather than bytes — otherwise the row
// width shifts and a multi-byte name is cut mid-sequence.
func TestChatArtReplaceFillsPlaceholderByColumns(t *testing.T) {
	// A 10-column placeholder: "@NET" + 5 fill chars + "@".
	const placeholder = "@NET#####@"
	tests := []struct {
		name  string
		value string
	}{
		{"ascii short", "FSX"},
		{"ascii exact", "ABCDEFGHIJ"},
		{"ascii long", "ABCDEFGHIJKLMNOP"},
		{"multi-byte short", "café"},
		{"multi-byte overlong", "café société normande"},
		{"cjk overlong", "日本語のネットワーク"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			art := "|" + placeholder + "|"
			got := string(chatArtReplace([]byte(art), "NET", tt.value))

			if !utf8.ValidString(got) {
				t.Fatalf("chatArtReplace produced invalid UTF-8: %q", got)
			}
			inner := strings.TrimSuffix(strings.TrimPrefix(got, "|"), "|")
			if n := utf8.RuneCountInString(inner); n != utf8.RuneCountInString(placeholder) {
				t.Errorf("filled width = %d columns, want %d (%q)", n, utf8.RuneCountInString(placeholder), inner)
			}
		})
	}
}
