package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
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

// fakeDirectory is a handleDirectory over fixed handles and IDs.
type fakeDirectory struct {
	handles map[string]bool
	ids     map[int]bool
}

func (f fakeDirectory) HandleExists(h string) bool { return f.handles[strings.ToLower(h)] }
func (f fakeDirectory) AccountExists(id int) bool  { return f.ids[id] }

// Mail to a renamed account keeps its old To but still reaches the account
// by ID, so it isn't undeliverable and a sysop can't read it under that
// exception (#1061 review).
func TestUndeliverableMailRespectsRecipientID(t *testing.T) {
	dir := fakeDirectory{handles: map[string]bool{"annie": true}, ids: map[int]bool{4: true}}
	for _, tc := range []struct {
		name string
		m    message.DisplayMessage
		want bool
	}{
		{"renamed recipient, by ID", message.DisplayMessage{To: "Ann", RecipientID: 4}, false},
		{"recorded account gone", message.DisplayMessage{To: "Ann", RecipientID: 9}, true},
		{"no ID, handle unknown", message.DisplayMessage{To: "Real Name"}, true},
		{"no ID, handle known", message.DisplayMessage{To: "Annie"}, false},
	} {
		if got := undeliverableMail(dir, &tc.m); got != tc.want {
			t.Errorf("%s: undeliverable = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A recipient renamed before reading reads under the new handle; the sender
// then can't unsend the letter as if it were unread (#1061 review).
func TestUnsendSeesReadUnderTheNewHandle(t *testing.T) {
	env := newMsgEnv(t)
	env.runCmd("SENDPRIVMAIL", env.caller, "", "sysop\rLunch?\rNoon.\x1a")
	boss, _ := env.um.GetUserByID(env.sysop.ID)
	boss.Handle = "Boss"
	if err := env.um.UpdateUserByID(boss); err != nil {
		t.Fatal(err)
	}
	env.markRead(privmailAreaID, "Boss", 1)
	m := env.mustMsg(privmailAreaID, 1)
	if unsendableMail(env.e, env.um, privmailAreaID, m, env.caller.Handle, env.caller.ID) {
		t.Error("a letter read under the recipient's new handle counts as unsendable")
	}
}

// A private reply goes to the account the letter recorded: its new handle
// after a rename, and never a newcomer who took the old one; a recorded
// account that is gone refuses the reply (#1061 review).
func TestPrivateReplyGoesToTheRecordedAccount(t *testing.T) {
	env := newMsgEnv(t)
	sam, _ := env.um.AddUser("password1", "Sam", "Sam Smith", "")
	gone, _ := env.um.AddUser("password1", "Gone", "Gone Away", "")
	m := &message.DisplayMessage{IsPrivate: true, From: "Sam", To: "Caller", SenderID: sam.ID, RecipientID: env.caller.ID}

	renamed, _ := env.um.GetUserByID(sam.ID)
	renamed.Handle = "Samuel"
	if err := env.um.UpdateUserByID(renamed); err != nil {
		t.Fatal(err)
	}
	handle, id, _, ok := privateReplyAccount(env.um, m, env.caller, "Anonymous")
	if !ok || handle != "Samuel" || id != sam.ID {
		t.Errorf("reply to a renamed sender: %q #%d %v", handle, id, ok)
	}

	stored, _ := env.um.GetUserByID(gone.ID)
	stored.DeletedUser = true
	if err := env.um.UpdateUserByID(stored); err != nil {
		t.Fatal(err)
	}
	m.SenderID = gone.ID
	if _, _, reason, ok := privateReplyAccount(env.um, m, env.caller, "Anonymous"); ok || !strings.Contains(reason, "no longer exists") {
		t.Errorf("reply to a deleted account: ok=%v %q", ok, reason)
	}
}
