package menu

import (
	"strings"
	"testing"
)

// TestPromptAndComposeByNumberAndTag lists the areas and posts to the one
// chosen, first by list number and then by tag typed in lower case.
func TestPromptAndComposeByNumberAndTag(t *testing.T) {
	env := newMsgEnv(t)

	r := env.runCmd("PROMPTANDCOMPOSEMESSAGE", env.caller, "", "1\rBy number\r\rone\x1a")
	if !r.has("General Discussion", "Private Mail", "Enter Area # or Tag to Post In") {
		t.Errorf("area list or prompt missing; output:\n%s", r.text())
	}
	if !r.has("Message Posted!") {
		t.Fatalf("post by number not confirmed; output:\n%s", r.text())
	}

	env.runCmd("PROMPTANDCOMPOSEMESSAGE", env.caller, "", "general\rBy tag\r\rtwo\x1a")
	if n := env.msgCount(generalAreaID); n != 2 {
		t.Fatalf("GENERAL has %d messages, want 2", n)
	}
	for i, want := range []string{"By number", "By tag"} {
		if got := env.mustMsg(generalAreaID, i+1).Subject; got != want {
			t.Errorf("message %d subject = %q, want %q", i+1, got, want)
		}
	}
}

// TestPromptAndComposeRefusals checks Enter cancels, an unknown area and an
// area the caller cannot write to are refused, and a caller who is not
// logged in is turned away, all without posting.
func TestPromptAndComposeRefusals(t *testing.T) {
	env := newMsgEnv(t)
	lowly := *env.caller
	lowly.AccessLevel = 20 // may write GENERAL (s20) but not PRIVMAIL (s25)

	cases := []struct {
		name, input, want string
		lowly             bool
	}{
		{"enter cancels", "\r", "Post cancelled.", false},
		{"unknown number", "7\r", "Invalid area: 7", false},
		{"unknown tag", "nope\r", "Invalid area: nope", false},
		{"no write access", "2\r", "Access denied to post in area: Private Mail", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := env.caller
			if tc.lowly {
				u = &lowly
			}
			r := env.sub(t).runCmd("PROMPTANDCOMPOSEMESSAGE", u, "", tc.input+"Title\r\rbody\x1a")
			if !r.has(tc.want) {
				t.Errorf("output lacks %q:\n%s", tc.want, r.text())
			}
			if n := env.msgCount(generalAreaID) + env.msgCount(privmailAreaID); n != 0 {
				t.Errorf("%d messages posted, want 0", n)
			}
		})
	}

	if r := env.runCmd("PROMPTANDCOMPOSEMESSAGE", nil, "", "1\r"); !r.has("You must be logged in to post messages.") {
		t.Errorf("anonymous caller output:\n%s", r.text())
	}
}

// TestGenerateReplySubject checks "Re: " is added once and never doubled,
// whatever the case of an existing prefix.
func TestGenerateReplySubject(t *testing.T) {
	for in, want := range map[string]string{
		"hello":      "Re: hello",
		"Re: hello":  "Re: hello",
		"RE:hello":   "RE:hello",
		"  re: x":    "  re: x",
		"":           "Re: ",
		"Reply soon": "Re: Reply soon",
	} {
		if got := generateReplySubject(in); got != want {
			t.Errorf("generateReplySubject(%q) = %q, want %q", in, got, want)
		}
	}
	if strings.Count(generateReplySubject(generateReplySubject("x")), "Re:") != 1 {
		t.Error("replying to a reply doubled the prefix")
	}
}
