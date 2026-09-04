package menu

import "testing"

func TestExpandOptionalGroups(t *testing.T) {
	ph := map[string]string{
		"|UH": "Felonius",
		"|GL": "ViSiON/3",
		"|UN": "",     // SysOp-only note: empty for ordinary users
		"|CC": "None", // has a non-empty default
		"|TL": "   ",  // whitespace counts as empty
	}

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			// The MAIN.MNU case this was written for.
			name: "drops parens when the note is empty",
			in:   "|UH from |GL |{(|UN)|}",
			want: "|UH from |GL ",
		},
		{
			name: "keeps the group when the note is set",
			in:   "|UH from |GL |{(|UN)|}",
			want: "|UH from |GL (|UN)",
		},
		{
			name: "no groups is a passthrough",
			in:   "|UH from |GL",
			want: "|UH from |GL",
		},
		{
			name: "group with a populated placeholder survives",
			in:   "a|{[|GL]|}b",
			want: "a[|GL]b",
		},
		{
			name: "group with a non-empty default survives",
			in:   "a|{[|CC]|}b",
			want: "a[|CC]b",
		},
		{
			name: "whitespace-only value counts as empty",
			in:   "a|{[|TL]|}b",
			want: "ab",
		},
		{
			name: "group with no placeholder is kept, not eaten",
			in:   "a|{literal|}b",
			want: "aliteralb",
		},
		{
			name: "mixed group survives if any placeholder has a value",
			in:   "|{|GL/|UN|}",
			want: "|GL/|UN",
		},
		{
			name: "multiple groups resolve independently",
			in:   "|{(|UN)|}|{[|GL]|}|{<|UN>|}",
			want: "[|GL]",
		},
		{
			name: "unmatched opener degrades to visible text",
			in:   "a|{(|UN)b",
			want: "a|{(|UN)b",
		},
		{
			name: "empty group contributes nothing",
			in:   "a|{|}b",
			want: "ab",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := ph
			if c.name == "keeps the group when the note is set" {
				m = map[string]string{"|UH": "Felonius", "|GL": "ViSiON/3", "|UN": "SysOp"}
			}
			if got := expandOptionalGroups(c.in, m); got != c.want {
				t.Errorf("expandOptionalGroups(%q)\n got  %q\n want %q", c.in, got, c.want)
			}
		})
	}
}

// The end-to-end shape: groups are resolved before substitution, so the
// rendered prompt has no stray decoration.
func TestExpandOptionalGroupsThenSubstitute(t *testing.T) {
	tmpl := "|UH from |GL |{(|UN)|}"

	for _, tc := range []struct {
		note string
		want string
	}{
		{note: "SysOp", want: "Felonius from ViSiON/3 (SysOp)"},
		{note: "", want: "Felonius from ViSiON/3 "},
	} {
		ph := map[string]string{"|UH": "Felonius", "|GL": "ViSiON/3", "|UN": tc.note}
		out := expandOptionalGroups(tmpl, ph)
		for k, v := range ph {
			out = replaceAllSimple(out, k, v)
		}
		if out != tc.want {
			t.Errorf("note=%q: got %q, want %q", tc.note, out, tc.want)
		}
	}
}

func replaceAllSimple(s, old, new string) string {
	if old == "" {
		return s
	}
	out := ""
	for {
		i := indexOf(s, old)
		if i < 0 {
			return out + s
		}
		out += s[:i] + new
		s = s[i+len(old):]
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
