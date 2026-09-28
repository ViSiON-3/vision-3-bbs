package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TestComposeMessagePostsToNamedArea posts through COMPOSEMSG GENERAL: the
// message lands in GENERAL's base from the caller's handle to All with the
// typed subject and body, public, and the caller's post count is saved.
func TestComposeMessagePostsToNamedArea(t *testing.T) {
	env := newMsgEnv(t)

	r := env.runCmd("COMPOSEMSG", env.caller, "GENERAL", "First post\r\rHello, board.\x1a")
	if r.err != nil {
		t.Fatalf("COMPOSEMSG: %v", r.err)
	}
	if !r.has("Message Posted!") {
		t.Fatalf("post not confirmed; output:\n%s", r.text())
	}
	if n := env.msgCount(generalAreaID); n != 1 {
		t.Fatalf("GENERAL has %d messages, want 1", n)
	}
	m := env.mustMsg(generalAreaID, 1)
	if m.From != "Caller" || m.To != "All" || m.Subject != "First post" || m.IsPrivate {
		t.Errorf("message from/to/subject/private = %q/%q/%q/%v, want Caller/All/First post/false", m.From, m.To, m.Subject, m.IsPrivate)
	}
	if strings.TrimSpace(m.Body) != "Hello, board." {
		t.Errorf("body = %q, want the typed text", m.Body)
	}
	if n := env.msgCount(privmailAreaID); n != 0 {
		t.Errorf("PRIVMAIL gained %d messages from a GENERAL post", n)
	}
	if got := env.mustDiskUser(2).MessagesPosted; got != 1 {
		t.Errorf("saved MessagesPosted = %d, want 1", got)
	}
}

// TestComposeMessageUsesCurrentAreaAndSignature posts with no argument: the
// caller's current area is used, the typed recipient is kept, and the
// auto-signature follows the body. To: comes pre-filled with All, so the
// script backspaces over it first.
func TestComposeMessageUsesCurrentAreaAndSignature(t *testing.T) {
	env := newMsgEnv(t)
	env.caller.AutoSignature = "-- Carl"

	env.runCmd("COMPOSEMSG", env.caller, "", "Hi sysop\r\x7f\x7f\x7fSysop\rA question.\x1a")
	m := env.mustMsg(generalAreaID, 1)
	if m.To != "Sysop" || m.Subject != "Hi sysop" {
		t.Errorf("to/subject = %q/%q, want Sysop/Hi sysop", m.To, m.Subject)
	}
	if !strings.HasPrefix(m.Body, "A question.") || !strings.HasSuffix(strings.TrimSpace(m.Body), "-- Carl") {
		t.Errorf("body = %q, want text then signature", m.Body)
	}
}

// TestComposeMessagePrivateAreaIsPrivate posts in PRIVMAIL: the message
// carries the private flag and, the area being real-names-only, is signed
// with the caller's real name.
func TestComposeMessagePrivateAreaIsPrivate(t *testing.T) {
	env := newMsgEnv(t)

	env.runCmd("COMPOSEMSG", env.caller, "PRIVMAIL", "Psst\rsysop\rJust us.\x1a")
	if n := env.msgCount(privmailAreaID); n != 1 {
		t.Fatalf("PRIVMAIL has %d messages, want 1", n)
	}
	m := env.mustMsg(privmailAreaID, 1)
	if !m.IsPrivate {
		t.Error("PRIVMAIL post is not private")
	}
	if m.From != "Carl Caller" || m.To != "Sysop" {
		t.Errorf("from/to = %q/%q, want Carl Caller/Sysop", m.From, m.To)
	}
}

// TestComposeMessageAnonymous posts anonymously in an area that allows it:
// the author is the configured anonymous name and no signature is added.
func TestComposeMessageAnonymous(t *testing.T) {
	env := newMsgEnv(t)
	area, _ := env.e.MessageMgr.GetAreaByID(generalAreaID)
	updated := *area
	yes := true
	updated.AllowAnon = &yes
	if err := env.e.MessageMgr.UpdateAreaByID(generalAreaID, updated); err != nil {
		t.Fatalf("UpdateAreaByID: %v", err)
	}
	env.sysop.AutoSignature = "-- Sam"

	// Title, To (All), Y at the anonymous prompt, body.
	r := env.runCmd("COMPOSEMSG", env.sysop, "GENERAL", "Secret\r\rYGuess who.\x1a")
	if !r.has("Anonymous?") {
		t.Errorf("anonymous prompt not offered; output:\n%s", r.text())
	}
	if !r.has("Message Posted!") {
		t.Fatalf("anonymous post not confirmed; output:\n%s", r.text())
	}
	m := env.mustMsg(generalAreaID, 1)
	if m.From != "Anonymous Coward" {
		t.Errorf("anonymous post from %q, want Anonymous Coward", m.From)
	}
	if strings.Contains(m.Body, "-- Sam") {
		t.Errorf("anonymous post carries the signature: %q", m.Body)
	}
}

// TestComposeMessageAbandoned checks every way out before saving leaves the
// base untouched: a blank title, ESC at the title confirmed with Y, and an
// editor abort. ESC answered N returns to the title prompt.
func TestComposeMessageAbandoned(t *testing.T) {
	env := newMsgEnv(t)
	for _, tc := range []struct{ name, input, want string }{
		{"blank title", "\r", "Post aborted."},
		{"esc at title", "\x1bY", "Post aborted."},
		{"editor abort", "Title\r\rdraft\x01Y", "Message aborted."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := env.sub(t).runCmd("COMPOSEMSG", env.caller, "GENERAL", tc.input)
			if !r.has(tc.want) {
				t.Errorf("output lacks %q:\n%s", tc.want, r.text())
			}
			if n := env.msgCount(generalAreaID); n != 0 {
				t.Errorf("GENERAL has %d messages, want 0", n)
			}
		})
	}

	// ESC then N keeps composing: the post goes through.
	r := env.runCmd("COMPOSEMSG", env.caller, "GENERAL", "\x1bNKept\r\rbody\x1a")
	if !r.has("Message Posted!") || env.mustMsg(generalAreaID, 1).Subject != "Kept" {
		t.Errorf("post after declining the abort failed; output:\n%s", r.text())
	}
}

// TestComposeMessageRefusals checks COMPOSEMSG refuses without writing when
// the caller is not logged in, has no area, names an unknown area, or lacks
// write access.
func TestComposeMessageRefusals(t *testing.T) {
	env := newMsgEnv(t)

	noArea := *env.caller
	noArea.CurrentMessageAreaID = 0
	noArea.CurrentMessageAreaTag = ""
	lowly := *env.caller
	lowly.AccessLevel = 10

	for _, tc := range []struct {
		name string
		u    *user.User
		args string
		want string
	}{
		{"not logged in, no area", nil, "", "Not logged in and no area specified."},
		{"not logged in, area named", nil, "GENERAL", "You must be logged in to post messages."},
		{"no current area", &noArea, "", "No current message area selected."},
		{"unknown area", env.caller, "NOPE", "Invalid message area: NOPE"},
		{"no write access", &lowly, "GENERAL", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := env.sub(t).runCmd("COMPOSEMSG", tc.u, tc.args, "Title\r\rbody\x1a")
			if tc.want != "" && !r.has(tc.want) {
				t.Errorf("output lacks %q:\n%s", tc.want, r.text())
			}
			if r.has("[Title]") {
				t.Errorf("title prompt shown; output:\n%s", r.text())
			}
			if n := env.msgCount(generalAreaID); n != 0 {
				t.Errorf("GENERAL has %d messages, want 0", n)
			}
		})
	}
}
