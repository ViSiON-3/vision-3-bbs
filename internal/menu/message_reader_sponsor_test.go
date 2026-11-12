package menu

import (
	"testing"
)

// An area's sponsor may delete any message in it from the reader, as a
// co-sysop can: Del is on the bar, D asks first, and Y deletes it and packs
// the base. The sponsor's handle matches whatever its case, and the help
// lists Delete.
func TestMessageReaderSponsorDeletes(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(3)
	sponsorSetSponsor(env, generalAreaID, "caller")

	r := env.runCmd("READMSGS", env.caller, "", "?DY\x1b")
	if !r.has(" Del ") {
		t.Errorf("Del should be on the sponsor's bar; output:\n%s", r.text())
	}
	if !r.has("elete Message") {
		t.Errorf("the sponsor's help should list Delete; output:\n%s", r.text())
	}
	if !r.has("Delete this message?") {
		t.Errorf("delete should be confirmed; output:\n%s", r.text())
	}
	if n := env.msgCount(generalAreaID); n != 2 {
		t.Fatalf("GENERAL has %d messages after the sponsor's delete, want 2", n)
	}
	if got := env.mustMsg(generalAreaID, 1).Subject; got != "subj-2" {
		t.Errorf("message 1 after delete+pack = %q, want subj-2", got)
	}
}

// Sponsoring one area gives nothing in another: a caller who isn't the
// reading area's sponsor isn't offered Delete there.
func TestMessageReaderSponsorOnlyInOwnArea(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(2)
	sponsorSetSponsor(env, generalAreaID, "Someone")

	r := env.runCmd("READMSGS", env.caller, "", "?DY\x1b")
	if r.has("Delete this message?") || r.has("elete Message") || r.has(" Del ") {
		t.Errorf("a caller who doesn't sponsor GENERAL was offered delete; output:\n%s", r.text())
	}
	if n := env.msgCount(generalAreaID); n != 2 {
		t.Errorf("GENERAL has %d messages, want 2", n)
	}
}

// A sponsor the sysop removes while the delete is being confirmed deletes
// nothing, and is told why.
func TestMessageReaderSponsorRemovedBeforeConfirm(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(2)
	sponsorSetSponsor(env, generalAreaID, "Caller")
	env.whenOutput("Delete this message?", func() { sponsorSetSponsor(env, generalAreaID, "") })

	r := env.runCmd("READMSGS", env.caller, "", "DY\x1b")
	if n := env.msgCount(generalAreaID); n != 2 {
		t.Errorf("GENERAL has %d messages, want 2: the delete went ahead after the sponsor was removed", n)
	}
	if !r.has("You can no longer delete messages in this area.") {
		t.Errorf("no notice; output:\n%s", r.text())
	}
}
