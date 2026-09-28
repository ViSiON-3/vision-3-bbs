package menu

import (
	"strings"
	"testing"
)

// TestSendPrivateMailDelivers sends mail with SENDPRIVMAIL: it lands in
// PRIVMAIL, private, from the sender's handle to the recipient's handle as
// the recipient spells it, with the typed subject and body, and the
// sender's post count is saved.
func TestSendPrivateMailDelivers(t *testing.T) {
	env := newMsgEnv(t)
	env.caller.AutoSignature = "-- Carl"

	r := env.runCmd("SENDPRIVMAIL", env.caller, "", "sysop\rLunch?\rNoon at the diner.\x1a")
	if r.err != nil {
		t.Fatalf("SENDPRIVMAIL: %v", r.err)
	}
	if !r.has("Private message sent to Sysop!") {
		t.Fatalf("send not confirmed; output:\n%s", r.text())
	}
	if n := env.msgCount(privmailAreaID); n != 1 {
		t.Fatalf("PRIVMAIL has %d messages, want 1", n)
	}
	m := env.mustMsg(privmailAreaID, 1)
	if !m.IsPrivate || m.From != "Caller" || m.To != "Sysop" || m.Subject != "Lunch?" {
		t.Errorf("mail private/from/to/subject = %v/%q/%q/%q", m.IsPrivate, m.From, m.To, m.Subject)
	}
	if !strings.HasPrefix(m.Body, "Noon at the diner.") || !strings.HasSuffix(strings.TrimSpace(m.Body), "-- Carl") {
		t.Errorf("body = %q, want text then signature", m.Body)
	}
	if n := env.msgCount(generalAreaID); n != 0 {
		t.Errorf("GENERAL gained %d messages", n)
	}
	if got := env.mustDiskUser(2).MessagesPosted; got != 1 {
		t.Errorf("saved MessagesPosted = %d, want 1", got)
	}
}

// TestSendPrivateMailAbandoned checks each way the mail can be dropped
// before it is saved writes nothing: an unknown or blank recipient, a blank
// subject, ESC confirmed at either prompt, and an editor abort.
func TestSendPrivateMailAbandoned(t *testing.T) {
	env := newMsgEnv(t)
	for _, tc := range []struct{ name, input, want string }{
		{"unknown recipient", "nobody\r", "User 'nobody' not found."},
		{"blank recipient", "\r", "Recipient cannot be empty."},
		{"blank subject", "Sysop\r\r", "Post aborted."},
		{"esc at recipient", "\x1bY", "Post aborted."},
		{"esc at subject", "Sysop\r\x1bY", "Post aborted."},
		{"editor abort", "Sysop\rHi\rdraft\x01Y", "Message aborted."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := env.sub(t).runCmd("SENDPRIVMAIL", env.caller, "", tc.input)
			if !r.has(tc.want) {
				t.Errorf("output lacks %q:\n%s", tc.want, r.text())
			}
			if n := env.msgCount(privmailAreaID); n != 0 {
				t.Errorf("PRIVMAIL has %d messages, want 0", n)
			}
		})
	}

	// ESC answered N at each prompt carries on to a sent mail.
	r := env.runCmd("SENDPRIVMAIL", env.caller, "", "\x1bNSysop\r\x1bNHi\rbody\x1a")
	if !r.has("Private message sent to Sysop!") {
		t.Errorf("mail after declining the aborts not sent; output:\n%s", r.text())
	}

	if r := env.runCmd("SENDPRIVMAIL", nil, "", "Sysop\r"); !r.has("You must be logged in to send private mail.") {
		t.Errorf("anonymous caller output:\n%s", r.text())
	}
}
