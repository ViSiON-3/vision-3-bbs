package configeditor

import "testing"

func TestSanitizeEventID(t *testing.T) {
	cases := map[string]string{
		"My Event!":     "my_event",
		"Foo--Bar":      "foo_bar",
		"a@@@b":         "a_b",
		"___trim___":    "trim",
		"ALREADY_clean": "already_clean",
		"  spaced  ":    "spaced",
	}
	for in, want := range cases {
		if got := sanitizeEventID(in); got != want {
			t.Errorf("sanitizeEventID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeTag(t *testing.T) {
	cases := map[string]string{
		"GENERAL":      "GENERAL",
		"GEN/ERAL":     "GENERAL",
		"a:b*c?":       "abc",
		"../etc":       "etc",
		"  spaced  ":   "spaced",
		"back\\slash":  "backslash",
		"pipe|and<gt>": "pipeandgt",
	}
	for in, want := range cases {
		if got := sanitizeTag(in); got != want {
			t.Errorf("sanitizeTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDefaultLocalBoardName(t *testing.T) {
	if got := defaultLocalBoardName("fsxnet", "General"); got != "Fsxnet General" {
		t.Errorf("defaultLocalBoardName = %q, want %q", got, "Fsxnet General")
	}
	// Missing pieces return the area name unchanged.
	if got := defaultLocalBoardName("", "General"); got != "General" {
		t.Errorf("defaultLocalBoardName(empty network) = %q, want General", got)
	}
	if got := defaultLocalBoardName("fsxnet", ""); got != "" {
		t.Errorf("defaultLocalBoardName(empty area) = %q, want empty", got)
	}
}
