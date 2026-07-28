package menu

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestApplyConditionalRegions(t *testing.T) {
	sysop := &user.User{AccessLevel: 255}
	lowUser := &user.User{AccessLevel: 50}

	tests := []struct {
		name string
		in   string
		u    *user.User
		want string
	}{
		{"no markers passthrough", "[M] Message Bases\r\n", lowUser, "[M] Message Bases\r\n"},
		{"pass removes markers", "{{S255}}[*] Sysop{{/}}", sysop, "[*] Sysop"},
		{"fail blanks visible chars", "{{S255}}[*] Sysop{{/}}", lowUser, "         "},
		{"fail preserves surrounding text", "A{{S255}}XX{{/}}B", lowUser, "A  B"},
		{"fail preserves newlines", "{{S255}}AB\r\nCD{{/}}", lowUser, "  \r\n  "},
		{"fail removes pipe color codes", "{{S255}}|15Hi{{/}}", lowUser, "  "},
		{"fail removes 4-char bg pipe codes", "{{S255}}|B15Hi{{/}}", lowUser, "  "},
		{"fail removes ansi escapes", "{{S255}}\x1b[1;31mHi{{/}}", lowUser, "  "},
		{"fail removes tilde coord markers", "{{S255}}~ABHi{{/}}", lowUser, "  "},
		{"pass keeps pipe codes verbatim", "{{S255}}|15Hi{{/}}", sysop, "|15Hi"},
		{"compound acs with pipe operator", "{{S255|S10}}Hi{{/}}", lowUser, "Hi"},
		{"nil user hides region", "{{S255}}Hi{{/}}", nil, "  "},
		{"invalid acs hides region", "{{(((}}Hi{{/}}", sysop, "  "},
		{"unclosed region blanks to EOF", "A{{S255}}Hi there", lowUser, "A        "},
		{"unclosed region shown on pass", "A{{S255}}Hi", sysop, "AHi"},
		{"stray close marker stripped", "A{{/}}B", lowUser, "AB"},
		{"pending validations untouched", "N: {{PENDING_VALIDATIONS}}", lowUser, "N: {{PENDING_VALIDATIONS}}"},
		{"multiple sequential regions", "{{S255}}A{{/}}-{{S10}}B{{/}}", lowUser, " -B"},
		{"open braces without close are literal", "{{S255", lowUser, "{{S255"},
		{"condition spanning lines is literal art", "{{\r\nart}}text", lowUser, "{{\r\nart}}text"},
		{"real region still works after literal braces", "{{\r\n}} {{S255}}Hi{{/}}", lowUser, "{{\r\n}}   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(applyConditionalRegions([]byte(tt.in), tt.u))
			if got != tt.want {
				t.Errorf("applyConditionalRegions(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
