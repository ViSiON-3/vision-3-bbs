package menu

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/perm"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// groupRoles defines ops (mail from members and co-sysops) and announce
// (mail from anyone at level 10 and up).
const groupRoles = `{"roles": [], "groups": [
	{"name": "ops"},
	{"name": "announce", "mail_acs": "S10"},
	{"name": "empty"}
]}`

// newGroupMailEnv is newMsgEnv with group ops of Caller (2), Ann (3), Bob
// (4), a deleted member (5) and a banned one (6), and Outsider (7) in no
// group.
func newGroupMailEnv(t *testing.T) *menuEnv {
	t.Helper()
	useRoles(t, groupRoles)
	env := newMsgEnv(t)
	users := env.um.GetAllUsers()
	for _, u := range users {
		if u.ID == 2 {
			u.Groups = []string{"ops"}
		}
	}
	member := func(id int, handle string, level int, validated, deleted bool) *user.User {
		return &user.User{ID: id, Handle: handle, AccessLevel: level, Validated: validated, DeletedUser: deleted,
			TimeLimit: 60, MsgHdr: 2, Groups: []string{"OPS"}}
	}
	users = append(users,
		member(3, "Ann", 30, true, false),
		member(4, "Bob", 30, true, false),
		member(5, "Gone", 30, true, true),
		member(6, "Banned", 0, false, false),
		&user.User{ID: 7, Handle: "Outsider", AccessLevel: 30, Validated: true, TimeLimit: 60, MsgHdr: 2},
	)
	env.writeUsers(users...)
	return env
}

// privmailCopies is every message in PRIVMAIL.
func privmailCopies(env *menuEnv) []*message.DisplayMessage {
	var out []*message.DisplayMessage
	for n := 1; n <= env.msgCount(privmailAreaID); n++ {
		out = append(out, env.mustMsg(privmailAreaID, n))
	}
	return out
}

func recipients(msgs []*message.DisplayMessage) string {
	var to []string
	for _, m := range msgs {
		to = append(to, m.To)
	}
	return strings.Join(to, ",")
}

// SENDPRIVMAIL to @ops writes one private copy per member who can receive
// it, from the sender's handle, all marked with the group and one id; the
// sender, deleted and banned members get none.
func TestGroupMailDeliversACopyPerMember(t *testing.T) {
	env := newGroupMailEnv(t)
	r := env.runCmd("SENDPRIVMAIL", env.caller, "", "@OPS\rMeeting\rTonight at 9.\x1a")
	if !r.has("Private message sent to @ops!") {
		t.Fatalf("output:\n%s", r.text())
	}
	copies := privmailCopies(env)
	if got := recipients(copies); got != "Ann,Bob" {
		t.Fatalf("copies to %s, want Ann,Bob", got)
	}
	id := copies[0].MailGroupID
	for _, m := range copies {
		if !m.IsPrivate || m.From != "Caller" || m.Subject != "Meeting" || m.MailGroup != "ops" || m.MailGroupID != id || id == "" {
			t.Errorf("copy = %+v", m)
		}
	}
	if got := env.mustDiskUser(2).MessagesPosted; got != 1 {
		t.Errorf("MessagesPosted = %d, want 1 for the letter", got)
	}
}

// Ann's copy shows To: @ops, never her handle or another member's.
func TestGroupMailHeaderShowsTheGroup(t *testing.T) {
	env := newGroupMailEnv(t)
	env.runCmd("SENDPRIVMAIL", env.caller, "", "@ops\rMeeting\rTonight.\x1a")
	ann, _ := env.um.GetUserByID(3)
	r := env.runCmd("READPRIVMAIL", ann, "", "Q")
	if !r.has("@ops", "Meeting") {
		t.Errorf("output:\n%s", r.text())
	}
	if r.has("Bob") {
		t.Errorf("Ann's copy names another member:\n%s", r.text())
	}
}

// Who may send: members and co-sysops for a group with no mail_acs, the
// mail_acs where there is one; refused before the editor opens.
func TestGroupMailSenders(t *testing.T) {
	env := newGroupMailEnv(t)
	out, _ := env.um.GetUserByID(7)
	r := env.runCmd("SENDPRIVMAIL", out, "", "@ops\rHi\r")
	if !r.has("You may not send mail to @ops.") || r.has("Subject") {
		t.Errorf("outsider to ops:\n%s", r.text())
	}
	if n := env.msgCount(privmailAreaID); n != 0 {
		t.Fatalf("PRIVMAIL has %d messages", n)
	}
	r = env.sub(t).runCmd("SENDPRIVMAIL", env.sysop, "", "@ops\rFrom the sysop\rHello.\x1a")
	if !r.has("Private message sent to @ops!") {
		t.Errorf("sysop to ops:\n%s", r.text())
	}
	r = env.sub(t).runCmd("SENDPRIVMAIL", out, "", "@announce\rNews\rHello.\x1a")
	if !r.has("Nobody in @announce can receive mail.") {
		t.Errorf("outsider to announce (no members):\n%s", r.text())
	}
	r = env.sub(t).runCmd("SENDPRIVMAIL", env.caller, "", "@empty\r")
	if !r.has("You may not send mail to @empty.") && !r.has("Nobody in @empty can receive mail.") {
		t.Errorf("caller to empty:\n%s", r.text())
	}
	r = env.sub(t).runCmd("SENDPRIVMAIL", env.caller, "", "@nogroup\r")
	if !r.has("User '@nogroup' not found.") {
		t.Errorf("undefined group:\n%s", r.text())
	}
}

// A handle that is exactly the address is that user, not the group.
func TestGroupMailHandleWins(t *testing.T) {
	env := newGroupMailEnv(t)
	users := env.um.GetAllUsers()
	users = append(users, &user.User{ID: 8, Handle: "@ops", AccessLevel: 30, Validated: true, TimeLimit: 60, MsgHdr: 2})
	env.writeUsers(users...)
	r := env.runCmd("SENDPRIVMAIL", env.caller, "", "@ops\rHi\rHello.\x1a")
	if !r.has("Private message sent to @ops!") {
		t.Fatalf("output:\n%s", r.text())
	}
	if got := recipients(privmailCopies(env)); got != "@ops" {
		t.Errorf("mail went to %s, want the user @ops", got)
	}
}

// Sent mail lists the letter once; D unsends the copies nobody has read and
// leaves the ones that were.
func TestGroupMailSentOnceAndUnsend(t *testing.T) {
	env := newGroupMailEnv(t)
	env.runCmd("SENDPRIVMAIL", env.caller, "", "@ops\rMeeting\rTonight.\x1a")
	copies := privmailCopies(env)
	env.markRead(privmailAreaID, "Bob", copies[1].MsgNum)

	r := env.run(runSentPrivateMail, env.caller, "", "L\x1bQ")
	if !r.has("[1-1 of 1 messages]", "Posted to @ops") {
		t.Errorf("the letter isn't listed once, to @ops:\n%s", r.text())
	}
	r = env.sub(t).run(runSentPrivateMail, env.caller, "", "DYQ")
	if !r.has("Unsent from 1 member(s) of @ops; 1 had already read it.") {
		t.Errorf("output:\n%s", r.text())
	}
	if got := recipients(privmailCopies(env)); got != "Bob" {
		t.Errorf("copies left for %s, want Bob", got)
	}
}

// A member replying to a group copy is asked; yes writes to the group (the
// original sender included, the replier not), no writes to the sender only.
func TestGroupMailReply(t *testing.T) {
	env := newGroupMailEnv(t)
	env.runCmd("SENDPRIVMAIL", env.caller, "", "@ops\rMeeting\rTonight.\x1a")
	ann, _ := env.um.GetUserByID(3)

	r := env.sub(t).runCmd("READPRIVMAIL", ann, "", "RY"+"See you.\x1a"+"Q")
	if !r.has("Reply to all of @ops", "Private message sent to @ops!") {
		t.Fatalf("output:\n%s", r.text())
	}
	copies := privmailCopies(env)
	var replies []*message.DisplayMessage
	for _, m := range copies {
		if m.From == "Ann" {
			replies = append(replies, m)
		}
	}
	if got := recipients(replies); got != "Caller,Bob" {
		t.Errorf("group reply went to %s, want Caller,Bob", got)
	}

	r = env.sub(t).runCmd("READPRIVMAIL", ann, "", "RN"+"Just you.\x1a"+"Q")
	last := privmailCopies(env)[len(privmailCopies(env))-1]
	if last.From != "Ann" || last.To != "Caller" || last.MailGroup != "" {
		t.Errorf("private reply = %+v\n%s", last, r.text())
	}
}

// COMPOSEMSG in PRIVMAIL takes @group at To.
func TestGroupMailCompose(t *testing.T) {
	env := newGroupMailEnv(t)
	area, _ := env.e.MessageMgr.GetAreaByID(privmailAreaID)
	r := env.runCmd("COMPOSEMSG", env.caller, area.Tag, "Meeting\r@ops\rTonight.\x1a")
	if !r.has("Private message sent to @ops!") {
		t.Fatalf("output:\n%s", r.text())
	}
	if got := recipients(privmailCopies(env)); got != "Ann,Bob" {
		t.Errorf("copies to %s", got)
	}
}

func TestNewHandlesCannotStartWithAt(t *testing.T) {
	if validateHandle("@ops") {
		t.Error("signup accepts @ops")
	}
	if err := user.ValidateHandleEdit("Bob", "@bob"); err == nil {
		t.Error("rename to @bob accepted")
	}
	if err := user.ValidateHandleEdit("@old", "@old"); err != nil {
		t.Errorf("an existing @ handle can't be kept: %v", err)
	}
	if !validateHandle("b@b") {
		t.Error("an @ inside a handle is refused")
	}
}

// A group with more members than one letter may reach is refused before the
// editor opens.
func TestGroupMailTooManyMembers(t *testing.T) {
	useRoles(t, groupRoles)
	env := newMsgEnv(t)
	users := env.um.GetAllUsers()
	for _, u := range users {
		if u.ID == 2 {
			u.Groups = []string{"ops"}
		}
	}
	for i := 0; i <= maxGroupMailRecipients; i++ {
		users = append(users, &user.User{ID: 100 + i, Handle: "member" + strconv.Itoa(i),
			AccessLevel: 10, Validated: true, Groups: []string{"ops"}})
	}
	env.writeUsers(users...)
	r := env.runCmd("SENDPRIVMAIL", env.caller, "", "@ops\rHi\r")
	if !r.has("@ops has more than 500 members") || r.has("Subject") {
		t.Errorf("output:\n%s", r.text())
	}
}

// The sender's copy names one member. With the group gone from roles.json,
// the sender's reply is refused rather than sent privately to that member;
// a member's reply, which only ever names the sender, goes to the sender.
func TestGroupMailReplyAfterGroupRemoved(t *testing.T) {
	env := newGroupMailEnv(t)
	env.runCmd("SENDPRIVMAIL", env.caller, "", "@ops\rMeeting\rTonight.\x1a")
	before := env.msgCount(privmailAreaID)
	useRoles(t, `{"roles": [], "groups": []}`)

	r := env.sub(t).run(runSentPrivateMail, env.caller, "", "R"+"Hello.\x1a"+"Q")
	if !r.has("@ops is no longer a group.") {
		t.Errorf("output:\n%s", r.text())
	}
	if n := env.msgCount(privmailAreaID); n != before {
		t.Fatalf("the sender's reply was written (%d messages, was %d): %+v", n, before, privmailCopies(env)[n-1])
	}

	ann, _ := env.um.GetUserByID(3)
	r = env.sub(t).runCmd("READPRIVMAIL", ann, "", "R"+"Just you.\x1a"+"Q")
	last := privmailCopies(env)[env.msgCount(privmailAreaID)-1]
	if r.has("Reply to all") || last.From != "Ann" || last.To != "Caller" {
		t.Errorf("member's reply = %+v\n%s", last, r.text())
	}
}

// Sent mail lists a group letter once even when another letter landed
// between its copies.
func TestGroupMailSentOnceWhenCopiesAreApart(t *testing.T) {
	env := newGroupMailEnv(t)
	post := func(to, group string) {
		t.Helper()
		if _, err := env.e.MessageMgr.Post(privmailAreaID, "Caller", to, "Meeting", "Tonight.", message.PostOptions{
			Private: true, SenderHandle: "Caller", MailGroup: group, MailGroupID: map[string]string{"ops": "abc"}[group],
		}); err != nil {
			t.Fatal(err)
		}
	}
	post("Ann", "ops")
	post("Outsider", "")
	post("Bob", "ops")
	r := env.run(runSentPrivateMail, env.caller, "", "L\x1bQ")
	if !r.has("[1-2 of 2 messages]") {
		t.Errorf("sent mail doesn't list two letters:\n%s", r.text())
	}
}

// The group is looked up again when the letter is sent: a mail_acs
// tightened, or the group removed, while the editor was open counts.
func TestGroupMailRecheckedAtSend(t *testing.T) {
	env := newGroupMailEnv(t)
	ann, _ := env.um.GetUserByID(3)
	g, _ := perm.Current().Group("ops")
	if msg := env.e.checkGroupMail(env.um, ann, g, nil, time.Now()); msg != "" {
		t.Fatalf("checkGroupMail = %q", msg)
	}
	send := func() error {
		_, err := env.e.sendGroupMail(env.um, ann, g, privmailAreaID, "Ann", "News", "Hello.", nil, time.Now())
		return err
	}
	useRoles(t, `{"roles": [], "groups": [{"name": "ops", "mail_acs": "S255"}]}`)
	if err := send(); !errors.Is(err, errGroupMailNotAllowed) {
		t.Errorf("tightened mail_acs: err = %v", err)
	}
	useRoles(t, `{"roles": [], "groups": []}`)
	if err := send(); !errors.Is(err, errGroupMailGone) {
		t.Errorf("removed group: err = %v", err)
	}
	if n := env.msgCount(privmailAreaID); n != 0 {
		t.Errorf("%d copies written", n)
	}
}

// A group reply checks mail_acs against the call as it is: time-left terms
// count from when the call started, not from the reply.
func TestGroupMailReplyUsesSessionStart(t *testing.T) {
	env := newGroupMailEnv(t)
	useRoles(t, `{"roles": [], "groups": [{"name": "ops", "mail_acs": "T50"}]}`)
	env.runCmd("SENDPRIVMAIL", env.caller, "", "@ops\rMeeting\rTonight.\x1a")
	ann, _ := env.um.GetUserByID(3)
	late := env.sub(t)
	late.calledFor = 30 * time.Minute
	r := late.runCmd("READPRIVMAIL", ann, "", "RY"+"Q")
	if !r.has("You may not send mail to @ops.") {
		t.Errorf("30 minutes into a 60-minute call passed T50:\n%s", r.text())
	}
}
