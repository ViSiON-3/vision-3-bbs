package menu

import (
	"testing"
)

// Private mail records the sender's and recipient's account IDs (#1048), so
// a renamed recipient keeps reading it and a new account given the old
// handle doesn't.
func TestPrivateMailFollowsTheAccount(t *testing.T) {
	env := newMsgEnv(t)
	env.runCmd("SENDPRIVMAIL", env.caller, "", "sysop\rLunch?\rNoon.\x1a")
	m := env.mustMsg(privmailAreaID, 1)
	if m.SenderID != env.caller.ID || m.RecipientID != env.sysop.ID {
		t.Fatalf("IDs recorded: sender %d recipient %d, want %d and %d", m.SenderID, m.RecipientID, env.caller.ID, env.sysop.ID)
	}

	boss, _ := env.um.GetUserByID(env.sysop.ID)
	boss.Handle = "Boss"
	if err := env.um.UpdateUserByID(boss); err != nil {
		t.Fatal(err)
	}
	boss, _ = env.um.GetUserByID(env.sysop.ID)
	boss.MsgHdr, boss.CurrentMsgConferenceID = 2, 1
	if r := env.sub(t).runCmd("READPRIVMAIL", boss, "", "Q"); !r.has("Lunch?") {
		t.Errorf("the renamed recipient lost the mail:\n%s", r.text())
	}

	newcomer, err := env.um.AddUser("password1", "Sysop", "New Person", "")
	if err != nil {
		t.Skipf("the old handle can't be registered here (%v); the ID check is covered in the message package", err)
	}
	newcomer.Validated, newcomer.AccessLevel, newcomer.MsgHdr, newcomer.CurrentMsgConferenceID = true, 10, 2, 1
	if r := env.sub(t).runCmd("READPRIVMAIL", newcomer, "", "Q"); r.has("Lunch?") {
		t.Errorf("a new account with the old handle read the mail:\n%s", r.text())
	}
}

// A post records its author's ID, so an author who was renamed can still
// edit it as their own (#1048).
func TestRenamedAuthorStillOwnsPost(t *testing.T) {
	env := newMsgEnv(t)
	r := env.runCmd("COMPOSEMSG", env.caller, "", "Mine\r\rOriginal words.\x1a")
	if r.err != nil {
		t.Fatal(r.err)
	}
	n := env.msgCount(generalAreaID)
	if m := env.mustMsg(generalAreaID, n); m.SenderID != env.caller.ID {
		t.Fatalf("post SenderID = %d, want %d", m.SenderID, env.caller.ID)
	}
	carl, _ := env.um.GetUserByID(env.caller.ID)
	carl.Handle = "Carl"
	if err := env.um.UpdateUserByID(carl); err != nil {
		t.Fatal(err)
	}
	carl, _ = env.um.GetUserByID(env.caller.ID)
	if !ownMessage(env.mustMsg(generalAreaID, n), carl, "Anonymous") {
		t.Error("the renamed author doesn't own their post")
	}
}
