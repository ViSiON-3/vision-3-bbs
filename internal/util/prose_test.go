package util

import "testing"

func TestPlural(t *testing.T) {
	for n, want := range map[int]string{0: "votes", 1: "vote", 2: "votes"} {
		if got := Plural(n, "vote", "votes"); got != want {
			t.Errorf("Plural(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestAndList(t *testing.T) {
	for _, c := range []struct {
		words []string
		want  string
	}{
		{nil, ""},
		{[]string{"a"}, "a"},
		{[]string{"a", "b"}, "a and b"},
		{[]string{"a", "b", "c"}, "a, b and c"},
	} {
		if got := AndList(c.words); got != c.want {
			t.Errorf("AndList(%q) = %q, want %q", c.words, got, c.want)
		}
	}
}
