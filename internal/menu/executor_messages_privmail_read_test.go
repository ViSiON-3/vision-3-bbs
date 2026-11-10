package menu

import (
	"strings"
	"testing"
)

// seedPrivmail posts three PRIVMAIL messages: to Caller, to Sysop, and to
// Caller again, plus one public post to Caller that must not count as mail.
func seedPrivmail(env *menuEnv) {
	env.postMsgs(privmailAreaID,
		testMsg{from: "Sysop", to: "Caller", subject: "for-caller-1", private: true},
		testMsg{from: "Caller", to: "Sysop", subject: "for-sysop", private: true},
		testMsg{from: "Sysop", to: "caller", subject: "for-caller-2", private: true},
		testMsg{from: "Sysop", to: "Caller", subject: "public-note"},
	)
}

// TestReadPrivateMailShowsOnlyOwnMail reads PRIVMAIL as each user: the
// caller sees exactly their two messages (N steps over the sysop's), the
// sysop sees only theirs, and the caller's current area is put back.
func TestReadPrivateMailShowsOnlyOwnMail(t *testing.T) {
	env := newMsgEnv(t)
	seedPrivmail(env)

	// N, N: the second N runs past the caller's last message. Then S walks
	// back over the sysop's mail to the first.
	r := env.runCmd("READPRIVMAIL", env.caller, "", "NSQ")
	if !r.has("Found 2 private message(s) for you.", "for-caller-1-body", "for-caller-2-body") {
		t.Errorf("caller's mail missing; output:\n%s", r.text())
	}
	if r.has("for-sysop") || r.has("public-note") {
		t.Errorf("caller was shown mail that is not theirs; output:\n%s", r.text())
	}
	if env.caller.CurrentMessageAreaID != generalAreaID || env.caller.CurrentMessageAreaTag != "GENERAL" {
		t.Errorf("caller left in area %d/%q, want GENERAL restored", env.caller.CurrentMessageAreaID, env.caller.CurrentMessageAreaTag)
	}

	r = env.runCmd("READPRIVMAIL", env.sysop, "", "NQ")
	if !r.has("Found 1 private message(s) for you.", "for-sysop-body") {
		t.Errorf("sysop's mail missing; output:\n%s", r.text())
	}
	if r.has("for-caller") || r.has("public-note") {
		t.Errorf("sysop was shown mail that is not theirs; output:\n%s", r.text())
	}
	if !r.has("End of messages.") {
		t.Errorf("N past the only message should end the reader; output:\n%s", r.text())
	}
}

// TestReadPrivateMailStartsAtFirstUnread checks the reader opens on the
// first of the caller's messages past their last-read pointer.
func TestReadPrivateMailStartsAtFirstUnread(t *testing.T) {
	env := newMsgEnv(t)
	seedPrivmail(env)
	env.markRead(privmailAreaID, "Caller", 1)

	r := env.runCmd("READPRIVMAIL", env.caller, "", "Q")
	if r.has("for-caller-1-body") || !r.has("for-caller-2-body") {
		t.Errorf("reader should open on the unread message 3; output:\n%s", r.text())
	}
	if lr := env.diskLastRead(privmailAreaID, "Caller"); lr != 3 {
		t.Errorf("lastread = %d, want 3", lr)
	}
}

// TestReadPrivateMailNoneForYou checks an empty mailbox and one holding
// only other people's mail both say so without opening the reader.
func TestReadPrivateMailNoneForYou(t *testing.T) {
	env := newMsgEnv(t)
	if r := env.runCmd("READPRIVMAIL", env.caller, "", ""); !r.has("No private mail found.") {
		t.Errorf("empty PRIVMAIL output:\n%s", r.text())
	}
	env.postMsgs(privmailAreaID, testMsg{from: "Caller", to: "Sysop", subject: "for-sysop", private: true})
	r := env.runCmd("READPRIVMAIL", env.caller, "", "")
	if !r.has("No private mail found for you.") || r.has("for-sysop") {
		t.Errorf("mailbox with only others' mail output:\n%s", r.text())
	}
	if r := env.runCmd("READPRIVMAIL", nil, "", ""); !r.has("You must be logged in to read private mail.") {
		t.Errorf("anonymous caller output:\n%s", r.text())
	}
}

// TestReadPrivateMailReplyStaysPrivate replies to a private message: the
// reply is private, addressed to the sender, and the sender finds it in
// their own mailbox.
func TestReadPrivateMailReplyStaysPrivate(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(privmailAreaID, testMsg{from: "Sysop", to: "Caller", subject: "ping", private: true})

	env.runCmd("READPRIVMAIL", env.caller, "", "Rpong\x1aQ")
	if n := env.msgCount(privmailAreaID); n != 2 {
		t.Fatalf("PRIVMAIL has %d messages, want 2", n)
	}
	reply := env.mustMsg(privmailAreaID, 2)
	if !reply.IsPrivate || reply.From != "Caller" || reply.To != "Sysop" || reply.Subject != "Re: ping" {
		t.Errorf("reply private/from/to/subject = %v/%q/%q/%q", reply.IsPrivate, reply.From, reply.To, reply.Subject)
	}

	r := env.runCmd("READPRIVMAIL", env.sysop, "", "Q")
	if !r.has("Found 1 private message(s) for you.", "pong") {
		t.Errorf("sysop cannot see the reply; output:\n%s", r.text())
	}
}

// TestListPrivateMailShowsOnlyOwnMail lists PRIVMAIL: only the caller's
// messages are listed, Enter opens one in the reader, and the caller's area
// is restored afterwards.
func TestListPrivateMailShowsOnlyOwnMail(t *testing.T) {
	env := newMsgEnv(t)
	seedPrivmail(env)

	// The list is newest first: Down to the second entry (message 1),
	// Enter to read it, Q the reader, Q the list.
	r := env.runCmd("LISTPRIVMAIL", env.caller, "", "\x1b[B\rQQ")
	if r.err != nil {
		t.Fatalf("LISTPRIVMAIL: %v", r.err)
	}
	if !r.has("for-caller-1", "for-caller-2") {
		t.Errorf("caller's mail not listed; output:\n%s", r.text())
	}
	if r.has("for-sysop") || r.has("public-note") {
		t.Errorf("list shows mail that is not the caller's; output:\n%s", r.text())
	}
	if !r.has("for-caller-1-body") || r.has("for-caller-2-body") {
		t.Errorf("Enter on the second entry should open message 1 only; output:\n%s", r.text())
	}
	if env.caller.CurrentMessageAreaID != generalAreaID || env.caller.CurrentMessageAreaTag != "GENERAL" {
		t.Errorf("caller left in area %d/%q, want GENERAL restored", env.caller.CurrentMessageAreaID, env.caller.CurrentMessageAreaTag)
	}

	if r := env.runCmd("LISTPRIVMAIL", nil, "", ""); !r.has("You must be logged in to list private mail.") {
		t.Errorf("anonymous caller output:\n%s", r.text())
	}
}

// TestReadPrivateMailDeleteOwn checks a caller can delete their own mail:
// D asks first, N keeps it, Y deletes it and packs the base, and the reader
// goes on to their next message. Mail to others is untouched.
func TestReadPrivateMailDeleteOwn(t *testing.T) {
	env := newMsgEnv(t)
	seedPrivmail(env)

	r := env.runCmd("READPRIVMAIL", env.caller, "", "DNDY?Q")
	if !r.has("Delete this message?") {
		t.Fatalf("delete should be confirmed; output:\n%s", r.text())
	}
	if !r.has(" Del ") {
		t.Errorf("Delete missing from the mailbox bar; output:\n%s", r.text())
	}
	if !r.has("elete Message") {
		t.Errorf("the mailbox help should list Delete; output:\n%s", r.text())
	}
	if n := env.msgCount(privmailAreaID); n != 3 {
		t.Fatalf("PRIVMAIL has %d messages after delete, want 3", n)
	}
	var subjects []string
	for i := 1; i <= 3; i++ {
		subjects = append(subjects, env.mustMsg(privmailAreaID, i).Subject)
	}
	if got := strings.Join(subjects, ","); got != "for-sysop,for-caller-2,public-note" {
		t.Errorf("PRIVMAIL after delete = %s", got)
	}
	if !r.has("for-caller-2-body") {
		t.Errorf("reader should move on to the next mail; output:\n%s", r.text())
	}
}

// TestReadPrivateMailDeleteLastEndsReader deletes a caller's only mail: the
// reader has nothing of theirs left and returns, leaving others' mail.
func TestReadPrivateMailDeleteLastEndsReader(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(privmailAreaID,
		testMsg{from: "Caller", to: "Sysop", subject: "for-sysop", private: true},
		testMsg{from: "Sysop", to: "Caller", subject: "for-caller", private: true},
	)

	r := env.runCmd("READPRIVMAIL", env.caller, "", "DY")
	if r.err != nil {
		t.Fatalf("READPRIVMAIL: %v", r.err)
	}
	if n := env.msgCount(privmailAreaID); n != 1 {
		t.Fatalf("PRIVMAIL has %d messages, want 1", n)
	}
	if s := env.mustMsg(privmailAreaID, 1).Subject; s != "for-sysop" {
		t.Errorf("PRIVMAIL kept %q, want for-sysop", s)
	}
}

// TestReadPrivateMailDeleteRace is the race from review: while the caller
// is at the delete prompt for their message 1, another session deletes it
// and packs, so the sysop's mail moves to number 1. Y must not delete it:
// the message is checked again as the delete happens, and nothing goes.
func TestReadPrivateMailDeleteRace(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(privmailAreaID,
		testMsg{from: "Sysop", to: "Caller", subject: "for-caller", private: true},
		testMsg{from: "Caller", to: "Sysop", subject: "for-sysop", private: true},
	)
	fired := false
	env.whenOutput("Delete this message?", func() {
		fired = true
		if err := env.e.MessageMgr.DeleteMessage(privmailAreaID, 1); err != nil {
			t.Errorf("other session's delete: %v", err)
		}
		if err := env.e.MessageMgr.PackAndLinkArea(privmailAreaID); err != nil {
			t.Errorf("other session's pack: %v", err)
		}
	})
	r := env.runCmd("READPRIVMAIL", env.caller, "", "DYQ")
	if !fired {
		t.Fatalf("the delete prompt never showed; output:\n%s", r.text())
	}
	if n := env.msgCount(privmailAreaID); n != 1 {
		t.Fatalf("PRIVMAIL has %d messages, want the sysop's 1", n)
	}
	if s := env.mustMsg(privmailAreaID, 1).Subject; s != "for-sysop" {
		t.Errorf("PRIVMAIL kept %q, want for-sysop", s)
	}
	if !r.has("Nothing was deleted.") {
		t.Errorf("the caller wasn't told; output:\n%s", r.text())
	}
}

// The same for a co-sysop: a pack under the prompt mustn't turn their
// delete onto the message that moved into the number.
func TestMessageReaderSysopDeleteRace(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(3)
	env.whenOutput("Delete this message?", func() {
		_ = env.e.MessageMgr.DeleteMessage(generalAreaID, 1)
		_ = env.e.MessageMgr.PackAndLinkArea(generalAreaID)
	})
	env.runCmd("READMSGS", env.sysop, "", "DYQ")
	if n := env.msgCount(generalAreaID); n != 2 {
		t.Fatalf("GENERAL has %d messages, want 2", n)
	}
	if s := env.mustMsg(generalAreaID, 1).Subject; s != "subj-2" {
		t.Errorf("message 1 = %q, want subj-2 kept", s)
	}
}
