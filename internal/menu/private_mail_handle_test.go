package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Private mail is read only by the handles on it (#467), so the local write
// paths address and sign it by handle (#462). These tests drive the real
// handlers on the shipped PRIVMAIL area, which has real_name_only set.

// newPrivmailEnv is a menuEnv whose Sysop and Caller (levels 255 and 30,
// real names "Sam Sysop" and "Carl Caller") can use the shipped PRIVMAIL
// area, plus extra users. Everyone has a header style set, so the reader
// does not stop to ask for one.
func newPrivmailEnv(t *testing.T, extra ...*user.User) (*menuEnv, *message.MessageArea) {
	t.Helper()
	env := newMenuEnv(t)
	priv, ok := env.e.MessageMgr.GetAreaByTag("PRIVMAIL")
	if !ok {
		t.Fatal("shipped PRIVMAIL area missing")
	}
	if !priv.RealNameOnly {
		t.Fatal("shipped PRIVMAIL is expected to be real_name_only")
	}
	users := append(defaultTestUsers(), extra...)
	users[1].AccessLevel = 30 // PRIVMAIL is s25
	for _, u := range users {
		u.MsgHdr = 2
	}
	env.writeUsers(users...)
	return env, priv
}

// mustMessage returns message n of area, failing the test if it is missing.
func (env *menuEnv) mustMessage(areaID, n int) *message.DisplayMessage {
	env.t.Helper()
	m, err := env.e.MessageMgr.GetMessage(areaID, n)
	if err != nil {
		env.t.Fatalf("message %d: %v", n, err)
	}
	return m
}

// messageCount returns how many messages area holds.
func (env *menuEnv) messageCount(areaID int) int {
	env.t.Helper()
	n, err := env.e.MessageMgr.GetMessageCountForArea(areaID)
	if err != nil {
		env.t.Fatal(err)
	}
	return n
}

// TestPrivateMailRoundTripByHandle is #462's repro: the sysop writes to Caller
// in PRIVMAIL, Caller replies from READPRIVMAIL, and the sysop's mailbox then
// holds the reply. The mail was signed with the real name "Sam Sysop", so the
// reply was addressed there and reached no one.
func TestPrivateMailRoundTripByHandle(t *testing.T) {
	env, priv := newPrivmailEnv(t)

	env.runCmd("COMPOSEMSG", env.sysop, "PRIVMAIL", "Hello\rCaller\rHELLO-BODY\x1a")
	sent := env.mustMessage(priv.ID, 1)
	if sent.From != "Sysop" || sent.To != "Caller" || !sent.IsPrivate {
		t.Fatalf("sent mail: From=%q To=%q private=%v, want From=Sysop To=Caller private", sent.From, sent.To, sent.IsPrivate)
	}

	r := env.runCmd("READPRIVMAIL", env.caller, "", "RREPLY-BODY\x1aQ")
	if !r.has("HELLO-BODY") {
		t.Fatalf("Caller did not get the mail:\n%s", r.text())
	}
	if env.messageCount(priv.ID) != 2 {
		t.Fatalf("reply not written:\n%s", r.text())
	}
	reply := env.mustMessage(priv.ID, 2)
	if reply.From != "Caller" || reply.To != "Sysop" || !reply.IsPrivate {
		t.Fatalf("reply: From=%q To=%q private=%v, want From=Caller To=Sysop private", reply.From, reply.To, reply.IsPrivate)
	}

	r = env.runCmd("READPRIVMAIL", env.sysop, "", "Q")
	if r.has("No private mail found") || !r.has("REPLY-BODY") {
		t.Fatalf("the sysop did not get the reply:\n%s", r.text())
	}
}

// TestComposePrivateMailNotAnonymous pins that private mail is not offered
// anonymity even where the area allows it: an anonymous private message could
// be read by no one but its recipient, and never answered.
func TestComposePrivateMailNotAnonymous(t *testing.T) {
	env, priv := newPrivmailEnv(t)
	allow := true
	priv.AllowAnon = &allow
	cfg := env.e.GetServerConfig()
	cfg.AnonymousLevel = 0
	env.e.SetServerConfig(cfg)

	r := env.runCmd("COMPOSEMSG", env.sysop, "PRIVMAIL", "Hello\rCaller\rHELLO-BODY\x1a")
	if r.has("Anonymous?") {
		t.Errorf("private mail offered the anonymous prompt:\n%s", r.text())
	}
	if env.messageCount(priv.ID) != 1 {
		t.Fatalf("mail not written:\n%s", r.text())
	}
	if m := env.mustMessage(priv.ID, 1); m.From != "Sysop" {
		t.Errorf("From = %q, want Sysop", m.From)
	}
}

// TestComposePublicRealNameOnlyKeepsRealName guards the other side of the
// rule: a public post in a real_name_only area is still signed with the real
// name.
func TestComposePublicRealNameOnlyKeepsRealName(t *testing.T) {
	env, _ := newPrivmailEnv(t)
	id, err := env.e.MessageMgr.AddArea(message.MessageArea{Tag: "REALNAMES", Name: "Real Names", AreaType: "local", RealNameOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	r := env.runCmd("COMPOSEMSG", env.sysop, "REALNAMES", "Hello\r\rPUBLIC-BODY\x1a")
	if env.messageCount(id) != 1 {
		t.Fatalf("post not written:\n%s", r.text())
	}
	if m := env.mustMessage(id, 1); m.From != "Sam Sysop" || m.To != "All" || m.IsPrivate {
		t.Errorf("post: From=%q To=%q private=%v, want From=\"Sam Sysop\" To=All public", m.From, m.To, m.IsPrivate)
	}
}

// TestPrivateReplyToRealNameResolvesToHandle pins that a reply to private
// mail signed with a real name (as COMPOSEMSG signed it before #462) goes to
// the handle of the one account with that real name.
func TestPrivateReplyToRealNameResolvesToHandle(t *testing.T) {
	env, priv := newPrivmailEnv(t)
	if _, err := env.e.MessageMgr.AddPrivateMessage(priv.ID, "Sam Sysop", "Caller", "Legacy", "LEGACY-BODY", ""); err != nil {
		t.Fatal(err)
	}
	r := env.runCmd("READPRIVMAIL", env.caller, "", "RREPLY-BODY\x1aQ")
	if env.messageCount(priv.ID) != 2 {
		t.Fatalf("reply not written:\n%s", r.text())
	}
	if reply := env.mustMessage(priv.ID, 2); reply.To != "Sysop" || reply.From != "Caller" {
		t.Errorf("reply: From=%q To=%q, want From=Caller To=Sysop", reply.From, reply.To)
	}
}

// TestPrivateReplyToUnidentifiedSenderRefused pins that a private reply no
// account could read is refused with a reason instead of being written.
func TestPrivateReplyToUnidentifiedSenderRefused(t *testing.T) {
	for _, tc := range []struct {
		name, from, want string
	}{
		{"unknown", "Nobody Known", "Can't tell which user 'Nobody Known' is"},
		{"ambiguous real name", "Pat Twin", "Can't tell which user 'Pat Twin' is"},
		{"anonymous", "Anonymous", "is anonymous"},
		{"configured anonymous name", "Anonymous Coward", "is anonymous"},
		{"deleted", "Gone", "Gone no longer has an account"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, priv := newPrivmailEnv(t,
				&user.User{ID: 3, Handle: "TwinA", RealName: "Pat Twin", AccessLevel: 30, Validated: true},
				&user.User{ID: 4, Handle: "TwinB", RealName: "Pat Twin", AccessLevel: 30, Validated: true},
				&user.User{ID: 5, Handle: "Gone", RealName: "Gone Away", AccessLevel: 30, Validated: true, DeletedUser: true},
			)
			if _, err := env.e.MessageMgr.AddPrivateMessage(priv.ID, tc.from, "Caller", "Hi", "PARENT-BODY", ""); err != nil {
				t.Fatal(err)
			}
			// If the reply is (wrongly) allowed, the editor takes the body;
			// if it is refused, the reader reads it as commands, so it uses
			// no reader keys.
			r := env.runCmd("READPRIVMAIL", env.caller, "", "Rzzz\x1aQ")
			if !r.has("PARENT-BODY") {
				t.Fatalf("Caller did not get the mail:\n%s", r.text())
			}
			if n := env.messageCount(priv.ID); n != 1 {
				m := env.mustMessage(priv.ID, n)
				t.Fatalf("reply written anyway, To=%q", m.To)
			}
			if !r.has(tc.want) {
				t.Errorf("no %q notice:\n%s", tc.want, r.text())
			}
		})
	}
}

// TestReplyThenNextShowsNextMessage is #462's second bug: after a reply, N
// skipped a message because the reply advanced the message number while the
// reader went on showing the message replied to.
func TestReplyThenNextShowsNextMessage(t *testing.T) {
	check := func(t *testing.T, out string) {
		t.Helper()
		// Keys were R (reply to message 1), N, Q: message 2 is the only
		// other message that should ever be on screen.
		if !strings.Contains(out, "FIRST-BODY") {
			t.Fatalf("first message never shown:\n%s", out)
		}
		if !strings.Contains(out, "SECOND-BODY") {
			t.Errorf("N after the reply did not show message 2:\n%s", out)
		}
		if strings.Contains(out, "THIRD-BODY") {
			t.Errorf("N after the reply skipped to message 3:\n%s", out)
		}
	}

	t.Run("READMSGS", func(t *testing.T) {
		env, _ := newPrivmailEnv(t)
		gen, ok := env.e.MessageMgr.GetAreaByTag("GENERAL")
		if !ok {
			t.Fatal("shipped GENERAL area missing")
		}
		for _, body := range []string{"FIRST-BODY", "SECOND-BODY", "THIRD-BODY"} {
			if _, err := env.e.MessageMgr.AddMessage(gen.ID, "Sysop", "All", "Topic", body, ""); err != nil {
				t.Fatal(err)
			}
		}
		u := env.caller
		u.CurrentMsgConferenceID = gen.ConferenceID
		u.CurrentMessageAreaID = gen.ID
		u.CurrentMessageAreaTag = gen.Tag
		r := env.runCmd("READMSGS", u, "", "RREPLY-TYPED\x1aNQ")
		if env.messageCount(gen.ID) != 4 {
			t.Fatalf("reply not written:\n%s", r.text())
		}
		check(t, r.text())
	})

	t.Run("READPRIVMAIL", func(t *testing.T) {
		env, priv := newPrivmailEnv(t)
		for _, body := range []string{"FIRST-BODY", "SECOND-BODY", "THIRD-BODY"} {
			if _, err := env.e.MessageMgr.AddPrivateMessage(priv.ID, "Sysop", "Caller", "Topic", body, ""); err != nil {
				t.Fatal(err)
			}
		}
		r := env.runCmd("READPRIVMAIL", env.caller, "", "RREPLY-TYPED\x1aNQ")
		if env.messageCount(priv.ID) != 4 {
			t.Fatalf("reply not written:\n%s", r.text())
		}
		check(t, r.text())
	})
}

// TestSendPrivMailRefusesDeletedUser pins that SENDPRIVMAIL, like COMPOSEMSG,
// will not write mail to a deleted account.
func TestSendPrivMailRefusesDeletedUser(t *testing.T) {
	env, priv := newPrivmailEnv(t,
		&user.User{ID: 3, Handle: "Gone", RealName: "Gone Away", AccessLevel: 30, Validated: true, DeletedUser: true})
	r := env.runCmd("SENDPRIVMAIL", env.sysop, "", "Gone\rHello\rBODY\x1a")
	if n := env.messageCount(priv.ID); n != 0 {
		t.Fatalf("mail to a deleted user was written (%d messages):\n%s", n, r.text())
	}
	if !r.has("User 'Gone' not found") {
		t.Errorf("no not-found notice:\n%s", r.text())
	}
}
