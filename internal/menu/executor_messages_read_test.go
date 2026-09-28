package menu

import (
	"slices"
	"testing"
)

// TestReadMsgsGuards checks READMSGS refuses a caller who is not logged in
// or has no area, and reports an empty area, without opening the reader.
func TestReadMsgsGuards(t *testing.T) {
	env := newMsgEnv(t)

	if r := env.runCmd("READMSGS", nil, "", ""); !r.has("You must be logged in to read messages.") {
		t.Errorf("anonymous READMSGS output:\n%s", r.text())
	}

	noArea := *env.caller
	noArea.CurrentMessageAreaID = 0
	noArea.CurrentMessageAreaTag = ""
	if r := env.runCmd("READMSGS", &noArea, "", ""); !r.has("No message area selected.") {
		t.Errorf("READMSGS with no area output:\n%s", r.text())
	}

	if r := env.runCmd("READMSGS", env.caller, "", ""); !r.has("No messages in area GENERAL.") {
		t.Errorf("READMSGS in an empty area output:\n%s", r.text())
	}
}

// TestReadMsgsNothingNewAsksForMessageNumber checks that with every message
// read READMSGS asks which one to open: a valid number opens it, Enter
// cancels, and an out-of-range or non-numeric entry is refused.
func TestReadMsgsNothingNewAsksForMessageNumber(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(3)
	env.markRead(generalAreaID, "Caller", 3)

	r := env.runCmd("READMSGS", env.caller, "", "2\rQ")
	if !r.has("No new messages in area GENERAL.", "Total messages: 3.", "Read message # (1-3") {
		t.Errorf("prompt missing; output:\n%s", r.text())
	}
	if got := readerShown(r, 3); !slices.Equal(got, []int{2}) {
		t.Errorf("messages shown = %v, want [2]", got)
	}

	r = env.runCmd("READMSGS", env.caller, "", "\r")
	if got := readerShown(r, 3); len(got) != 0 || r.next != "" {
		t.Errorf("Enter should cancel; shown %v next %q", got, r.next)
	}

	for _, in := range []string{"9\r", "abc\r", "0\r"} {
		r = env.runCmd("READMSGS", env.caller, "", in)
		if !r.has("Invalid message number:") || len(readerShown(r, 3)) != 0 {
			t.Errorf("input %q not refused; output:\n%s", in, r.text())
		}
	}
}

// TestReadMsgsPicksHeaderStyleFirst checks a caller with no header style is
// sent through the header picker before reading, and the style chosen there
// is saved and used.
func TestReadMsgsPicksHeaderStyleFirst(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(1)
	env.caller.MsgHdr = 0

	// SPACE picks the highlighted (first) style, then Q leaves the reader.
	r := env.runCmd("READMSGS", env.caller, "", " Q")
	if !r.has("Please select a message header style.") {
		t.Errorf("header picker not offered; output:\n%s", r.text())
	}
	if got := env.mustDiskUser(2).MsgHdr; got != 1 {
		t.Errorf("saved MsgHdr = %d, want 1", got)
	}
	if !r.has("subj-1-body") {
		t.Errorf("message not shown after picking a header; output:\n%s", r.text())
	}
}

// TestNewscanCommandCurrentArea runs NEWSCAN CURRENT: the scan setup opens
// for the current area only and the reader walks the unread messages,
// advancing the saved pointer.
func TestNewscanCommandCurrentArea(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(3)
	env.markRead(generalAreaID, "Caller", 1)

	// Enter accepts the scan defaults; N reads 2 then 3, Q.
	r := env.runCmd("NEWSCAN", env.caller, "current", "\rNQ")
	if r.err != nil {
		t.Fatalf("NEWSCAN: %v", r.err)
	}
	if got := readerShown(r, 3); !slices.Equal(got, []int{2, 3}) {
		t.Errorf("messages shown = %v, want [2 3]", got)
	}
	if lr := env.diskLastRead(generalAreaID, "Caller"); lr != 3 {
		t.Errorf("lastread = %d, want 3", lr)
	}

	if r := env.runCmd("NEWSCAN", nil, "", ""); r.next != "" || r.err != nil || r.raw != "" {
		t.Errorf("anonymous NEWSCAN should do nothing; next=%q err=%v out=%q", r.next, r.err, r.text())
	}
}
