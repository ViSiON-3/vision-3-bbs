package message

import "testing"

func TestNormalizeThreadSubject(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Topic", "Topic"},
		{"  Topic  ", "Topic"},
		{"Re: Topic", "Topic"},
		{"RE: re: Re: Topic", "Topic"},
		{"Topic -Re: #12-", "Topic"},                     // Pascal-style reply marker
		{"Re: Topic -Re: #3-  ", "Topic"},                // both at once
		{"Topic -Re: #12- more", "Topic -Re: #12- more"}, // marker only counts at the end
		{"Re:Topic", "Re:Topic"},                         // the prefix needs its space
		{"Regarding: x", "Regarding: x"},
		{"Re: ", "Re:"}, // trimmed before the prefix can match
		{"", ""},
	} {
		if got := NormalizeThreadSubject(tc.in); got != tc.want {
			t.Errorf("NormalizeThreadSubject(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestThreadKeyFoldsCase(t *testing.T) {
	if got := ThreadKey("RE: Hello World -Re: #4-"); got != "hello world" {
		t.Errorf("ThreadKey = %q, want %q", got, "hello world")
	}
}

func TestSubjectsMatchThread(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"Topic", "Re: topic", true},
		{"Re: Topic -Re: #1-", "TOPIC", true},
		{"Topic", "Topic two", false},
		{"", "Re: ", false},
		{"", "  ", true},
	} {
		if got := SubjectsMatchThread(tc.a, tc.b); got != tc.want {
			t.Errorf("SubjectsMatchThread(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
