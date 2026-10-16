package stringhook

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  Hook
	}{
		{"plain text", "|09Hello", Hook{Kind: Text, Rest: "|09Hello"}},
		{"empty", "", Hook{Kind: Text}},
		{"script", "!prompts", Hook{Kind: Script, Target: "prompts", Prefix: "!prompts"}},
		{"script with args", "!prompts yesno  big", Hook{Kind: Script, Target: "prompts",
			Args: []string{"yesno", "big"}, Prefix: "!prompts yesno  big"}},
		{"bang then colour is text", "!|12Warning", Hook{Kind: Escaped, Rest: "!12Warning", Prefix: "!|"}},
		{"escape", "!|Warning", Hook{Kind: Escaped, Rest: "!Warning", Prefix: "!|"}},
		{"escape keeps verbs", "!|%s left", Hook{Kind: Escaped, Rest: "!%s left", Prefix: "!|"}},
		{"bang punctuation is text", "!! ALERT !!", Hook{Kind: Text, Rest: "!! ALERT !!"}},
		{"multi-line bang is text", "!Hello\r\nworld", Hook{Kind: Text, Rest: "!Hello\r\nworld"}},
		{"file", "%welcome.ans", Hook{Kind: File, Target: "welcome.ans", Prefix: "%welcome.ans"}},
		{"file then text", "%bar.ans |07Press a key: %s", Hook{Kind: File, Target: "bar.ans",
			Rest: "|07Press a key: %s", Prefix: "%bar.ans "}},
		{"file then multi-line text", "%bar.ans a\r\nb", Hook{Kind: File, Target: "bar.ans",
			Rest: "a\r\nb", Prefix: "%bar.ans "}},
		{"format string is text", "%d of %d", Hook{Kind: Text, Rest: "%d of %d"}},
		{"format with suffix is text", "%s (default)", Hook{Kind: Text, Rest: "%s (default)"}},
		{"file without extension is text", "%welcome", Hook{Kind: Text, Rest: "%welcome"}},
		{"percent literal is text", "%", Hook{Kind: Text, Rest: "%"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Parse(tt.value); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parse(%q) = %+v, want %+v", tt.value, got, tt.want)
			}
		})
	}
}

func TestIsHook(t *testing.T) {
	for value, want := range map[string]bool{
		"!door":     true,
		"%a.ans":    true,
		"!|text":    false,
		"text":      false,
		"%d of %d":  false,
		"!! hi !!":  false,
		"%a.ans hi": true,
	} {
		if got := IsHook(value); got != want {
			t.Errorf("IsHook(%q) = %v, want %v", value, got, want)
		}
	}
}
