package menu

import (
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
