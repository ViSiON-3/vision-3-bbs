package menu

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// The author edits their own post with E: Enter keeps the subject, the text
// opens in the editor, and the saved message ends with who edited it. The
// help lists Edit on their post.
func TestReaderAuthorEditsOwnPost(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(generalAreaID, testMsg{from: "Caller", to: "All", subject: "mine"})

	r := env.runCmd("READMSGS", env.caller, "", "?E\rmore \x1aQ")
	if !r.has("dit Message", "Message saved.") {
		t.Errorf("output:\n%s", r.text())
	}
	m := env.mustMsg(generalAreaID, 1)
	if m.Subject != "mine" || m.From != "Caller" || m.To != "All" {
		t.Errorf("header = %q from %q to %q, want it kept", m.Subject, m.From, m.To)
	}
	if !strings.Contains(m.Body, "more") || !strings.Contains(m.Body, "mine-body") {
		t.Errorf("body = %q, want the typed text and the old text", m.Body)
	}
	if !editedLine.MatchString(lastLine(m.Body)) || !strings.HasPrefix(lastLine(m.Body), " * Edited by Caller on "+time.Now().Format("01/02/06")) ||
		!strings.Contains(m.Body, "\n\n * Edited by ") {
		t.Errorf("body = %q, want it to end with a blank line and the edited line", m.Body)
	}
	if n := env.msgCount(generalAreaID); n != 1 {
		t.Errorf("GENERAL has %d messages, want 1", n)
	}
}

// lastLine is the last line of s.
func lastLine(s string) string { return s[strings.LastIndex(s, "\n")+1:] }

// The edited line is stamped in the board's time zone, as the editor's clock
// shows it, not the host's.
func TestReaderEditLineInBoardTimezone(t *testing.T) {
	env := newMsgEnv(t)
	cfg := env.e.GetServerConfig()
	cfg.Timezone = "Pacific/Kiritimati" // UTC+14: never the host's zone and hour
	env.e.SetServerConfig(cfg)
	env.postMsgs(generalAreaID, testMsg{from: "Caller", to: "All", subject: "mine"})

	before := time.Now()
	env.runCmd("READMSGS", env.caller, "", "E\r\x1aQ")
	after := time.Now()
	loc, err := time.LoadLocation("Pacific/Kiritimati")
	if err != nil {
		t.Skip("no time zone database:", err)
	}
	got := lastLine(env.mustMsg(generalAreaID, 1).Body)
	want := func(at time.Time) string {
		return " * Edited by Caller on " + strings.ToLower(at.In(loc).Format("01/02/06 at 3:04 PM"))
	}
	if got != want(before) && got != want(after) {
		t.Errorf("edited line = %q, want %q (the board's zone)", got, want(after))
	}
}

// The subject can be changed at its prompt; a message edited again carries
// only its latest edited line.
func TestReaderEditChangesSubjectOnce(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(generalAreaID, testMsg{from: "Caller", to: "All", subject: "mine"})

	env.runCmd("READMSGS", env.caller, "", "E\b\b\b\brenamed\r\x1aQ")
	// Message 1 has been read now, so READMSGS asks which to read.
	r := env.runCmd("READMSGS", env.caller, "", "1\rE2\r\x1aQ")
	m := env.mustMsg(generalAreaID, 1)
	if m.Subject != "renamed2" {
		t.Errorf("subject = %q, want renamed2 after two edits; output:\n%s", m.Subject, r.text())
	}
	if n := strings.Count(m.Body, " * Edited by "); n != 1 {
		t.Errorf("body has %d edited lines, want 1:\n%q", n, m.Body)
	}
}

// Clearing the subject at its prompt keeps the one it had.
func TestReaderEditBlankSubjectKeeps(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(generalAreaID, testMsg{from: "Caller", to: "All", subject: "mine"})
	env.runCmd("READMSGS", env.caller, "", "E\b\b\b\b\r\x1aQ")
	if m := env.mustMsg(generalAreaID, 1); m.Subject != "mine" || !strings.Contains(m.Body, " * Edited by ") {
		t.Errorf("subject %q body %q, want the subject kept and the edit saved", m.Subject, m.Body)
	}
}

// Someone else's post can't be edited: E does nothing and the help doesn't
// list it. The sysop and the area's sponsor may edit it, and their edits
// are signed with their own handle.
func TestReaderEditOthersPost(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(1)

	r := env.runCmd("READMSGS", env.caller, "", "?EQ")
	if r.has("dit Message") || r.has("Subject:") {
		t.Errorf("caller was offered the edit; output:\n%s", r.text())
	}
	if m := env.mustMsg(generalAreaID, 1); m.Body != "subj-1-body" {
		t.Errorf("body = %q, want it unchanged", m.Body)
	}

	// The sysop edits the caller's post.
	env.postMsgs(generalAreaID, testMsg{from: "Caller", to: "All", subject: "theirs"})
	env.runCmd("READMSGS", env.sysop, "", "NE\r\x1aQ")
	if b := env.mustMsg(generalAreaID, 2).Body; !strings.Contains(b, " * Edited by Sysop on ") {
		t.Errorf("sysop's edit: body = %q", b)
	}

	// A fresh board: the caller has read message 1 above, so READMSGS would
	// now ask which message to read.
	env = newMsgEnv(t)
	env.generalMsgs(1)
	sponsorSetSponsor(env, generalAreaID, "caller")
	env.runCmd("READMSGS", env.caller, "", "E\r\x1aQ")
	if b := env.mustMsg(generalAreaID, 1).Body; !strings.Contains(b, " * Edited by Caller on ") {
		t.Errorf("sponsor's edit: body = %q", b)
	}
}

// Private mail isn't edited, even by its sender or the sysop.
func TestReaderEditNotInPrivateMail(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(privmailAreaID, testMsg{from: "Sysop", to: "Sysop", subject: "note", private: true})
	env.sysop.CurrentMessageAreaID, env.sysop.CurrentMessageAreaTag = privmailAreaID, "PRIVMAIL"

	r := env.runCmd("READMSGS", env.sysop, "", "E\r\x1aQ")
	if r.has("Subject:") {
		t.Errorf("edit offered in PRIVMAIL; output:\n%s", r.text())
	}
	if b := env.mustMsg(privmailAreaID, 1).Body; b != "note-body" {
		t.Errorf("body = %q, want it unchanged", b)
	}
}

// A message deleted while the editor is open isn't saved over, and the
// caller is told; nor is one whose sponsor is removed meanwhile.
func TestReaderEditChangedMeanwhile(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(1)
	env.postMsgs(generalAreaID, testMsg{from: "Caller", to: "All", subject: "mine"})
	env.whenOutput("Subject:", func() {
		if err := env.e.MessageMgr.DeleteMessage(generalAreaID, 2); err != nil {
			t.Error(err)
		}
	})
	r := env.runCmd("READMSGS", env.caller, "", "NE\r\x1aQ")
	if !r.has("That message was changed or deleted elsewhere. Nothing was saved.") {
		t.Errorf("output:\n%s", r.text())
	}

	// Another session edits it first: this edit doesn't overwrite theirs.
	env3 := newMsgEnv(t)
	env3.postMsgs(generalAreaID, testMsg{from: "Caller", to: "All", subject: "mine"})
	env3.whenOutput("Subject:", func() {
		err := env3.e.MessageMgr.EditMessageIf(generalAreaID, 1, func(m *message.DisplayMessage) (string, string, bool) {
			return m.Subject, "edited elsewhere", true
		})
		if err != nil {
			t.Error(err)
		}
	})
	r = env3.runCmd("READMSGS", env3.caller, "", "E\r\x1aQ")
	if !r.has("That message was changed or deleted elsewhere. Nothing was saved.") {
		t.Errorf("output:\n%s", r.text())
	}
	if b := env3.mustMsg(generalAreaID, 1).Body; b != "edited elsewhere" {
		t.Errorf("body = %q, want the other session's edit kept", b)
	}

	env2 := newMsgEnv(t)
	env2.generalMsgs(1)
	sponsorSetSponsor(env2, generalAreaID, "caller")
	env2.whenOutput("Subject:", func() { sponsorSetSponsor(env2, generalAreaID, "") })
	r = env2.runCmd("READMSGS", env2.caller, "", "E\r\x1aQ")
	if !r.has("You can no longer edit this message. Nothing was saved.") {
		t.Errorf("output:\n%s", r.text())
	}
	if b := env2.mustMsg(generalAreaID, 1).Body; b != "subj-1-body" {
		t.Errorf("body = %q, want it unchanged", b)
	}
}

// Leaving the editor without saving changes nothing.
func TestReaderEditCancelled(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(generalAreaID, testMsg{from: "Caller", to: "All", subject: "mine"})
	r := env.runCmd("READMSGS", env.caller, "", "E\rdraft\x01YQ")
	if !r.has("Edit cancelled.") {
		t.Errorf("output:\n%s", r.text())
	}
	if b := env.mustMsg(generalAreaID, 1).Body; b != "mine-body" {
		t.Errorf("body = %q, want it unchanged", b)
	}
}

// A message whose text is in another character set isn't offered: the
// editor would show it undecoded.
func TestReaderEditNotOtherCharset(t *testing.T) {
	env := newMsgEnv(t)
	if _, err := env.e.MessageMgr.Post(generalAreaID, "Caller", "All", "dos", "caf\x82", message.PostOptions{Kludges: []string{"CHRS: CP437 2"}}); err != nil {
		t.Fatal(err)
	}
	r := env.runCmd("READMSGS", env.caller, "", "E\r\x1aQ")
	if r.has("Subject:") {
		t.Errorf("edit offered on a CP437 message; output:\n%s", r.text())
	}
}

// Unlabelled text that isn't valid UTF-8 is an older program's raw CP437:
// the editor would rewrite its bytes, so it isn't offered either.
func TestReaderEditNotUnlabelledCP437(t *testing.T) {
	env := newMsgEnv(t)
	if _, err := env.e.MessageMgr.Post(generalAreaID, "Caller", "All", "dos", "caf\x82 \xb2\xdb", message.PostOptions{}); err != nil {
		t.Fatal(err)
	}
	r := env.runCmd("READMSGS", env.sysop, "", "?E\r\x1aQ")
	if r.has("dit Message") || r.has("Subject:") {
		t.Errorf("edit offered on unlabelled CP437; output:\n%s", r.text())
	}
	if b := env.mustMsg(generalAreaID, 1).Body; b != "caf\x82 \xb2\xdb" {
		t.Errorf("body = %q, want it unchanged", b)
	}
}

// A private message that reached a local public area has been delivered, so
// it isn't edited there either, by its sender or the sysop.
func TestReaderEditNotPrivateInLocalArea(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(generalAreaID, testMsg{from: "Caller", to: "Sysop", subject: "psst", private: true})
	for _, u := range []*user.User{env.caller, env.sysop} {
		r := env.runCmd("READMSGS", u, "", "?E\r\x1aQ")
		if r.has("dit Message") || r.has("Subject:") {
			t.Errorf("%s was offered the edit; output:\n%s", u.Handle, r.text())
		}
	}
	if b := env.mustMsg(generalAreaID, 1).Body; b != "psst-body" {
		t.Errorf("body = %q, want it unchanged", b)
	}
}

// numberedText is n lines, "line 1" to "line n".
func numberedText(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	return strings.Join(lines, "\n")
}

// A message longer than the editor holds, as from QWK, isn't opened in it:
// saving would drop the lines past the end. One that just fits is.
func TestReaderEditTooLong(t *testing.T) {
	env := newMsgEnv(t)
	long := numberedText(editor.MaxLines + 1)
	if _, err := env.e.MessageMgr.Post(generalAreaID, "Caller", "All", "long", long, message.PostOptions{}); err != nil {
		t.Fatal(err)
	}
	r := env.runCmd("READMSGS", env.sysop, "", "E\r\x1aQ")
	if !r.has(fmt.Sprintf("This message is %d lines long", editor.MaxLines+1)) || r.has("Subject:") {
		t.Errorf("output:\n%s", r.text())
	}
	if b := env.mustMsg(generalAreaID, 1).Body; b != long {
		t.Errorf("body changed: %d lines, want it unchanged", strings.Count(b, "\n")+1)
	}

	env = newMsgEnv(t)
	full := numberedText(editor.MaxLines)
	if _, err := env.e.MessageMgr.Post(generalAreaID, "Caller", "All", "full", full, message.PostOptions{}); err != nil {
		t.Fatal(err)
	}
	r = env.runCmd("READMSGS", env.sysop, "", "E\r\x1aQ")
	if !r.has("Message saved.") {
		t.Errorf("a %d-line message wasn't edited; output:\n%s", editor.MaxLines, r.text())
	}
	if b := env.mustMsg(generalAreaID, 1).Body; !strings.HasPrefix(b, full+"\n\n * Edited by Sysop on ") {
		t.Errorf("body = %q, want every line kept", b)
	}
}

// A local area a V3Net leaf binds by tag shares its messages with the
// network, so an edit there would change only this board's copy.
func TestReaderEditNotV3NetBound(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(generalAreaID, testMsg{from: "Caller", to: "All", subject: "mine"})
	env.e.MessageMgr.IsV3NetArea = func(id int) bool { return id == generalAreaID }
	r := env.runCmd("READMSGS", env.sysop, "", "?E\r\x1aQ")
	if r.has("dit Message") || r.has("Subject:") {
		t.Errorf("edit offered in a bound area; output:\n%s", r.text())
	}
	if b := env.mustMsg(generalAreaID, 1).Body; b != "mine-body" {
		t.Errorf("body = %q, want it unchanged", b)
	}
}

func TestEditableArea(t *testing.T) {
	mm := &message.MessageManager{IsV3NetArea: func(id int) bool { return id == 9 }}
	for _, tc := range []struct {
		area *message.MessageArea
		want bool
	}{
		{&message.MessageArea{Tag: "GENERAL", AreaType: "local"}, true},
		{&message.MessageArea{Tag: "GENERAL", AreaType: ""}, true},
		{&message.MessageArea{Tag: "PRIVMAIL", AreaType: "local"}, false},
		{&message.MessageArea{Tag: "FSX_GEN", AreaType: "echomail"}, false},
		{&message.MessageArea{Tag: "NETMAIL", AreaType: "netmail"}, false},
		{&message.MessageArea{Tag: "DOVE", AreaType: "qwknet"}, false},
		{&message.MessageArea{Tag: "FELONY", AreaType: "v3net"}, false},
		{&message.MessageArea{ID: 9, Tag: "BOUND", AreaType: "local"}, false},
		{nil, false},
	} {
		if got := editableArea(mm, tc.area); got != tc.want {
			t.Errorf("editableArea(%+v) = %v, want %v", tc.area, got, tc.want)
		}
	}
}

func TestOwnMessage(t *testing.T) {
	u := &user.User{Handle: "Caller", RealName: "Carl Caller"}
	for _, tc := range []struct {
		from, sender string
		want         bool
	}{
		{"caller", "", true},            // signed with the handle, any case
		{"Carl Caller", "Caller", true}, // a real-name post keeps the handle
		{"Carl Caller", "", false},      // a real name alone isn't enough
		{"Someone", "", false},          // another caller's post
		{"Anonymous", "", false},        // anonymous: no author
		{"Caller", "Someone", false},    // the kept handle decides
		{"", "", false},                 // no sender at all
	} {
		m := &message.DisplayMessage{From: tc.from, SenderHandle: tc.sender}
		if got := ownMessage(m, u, "Anonymous"); got != tc.want {
			t.Errorf("ownMessage(from %q, sender %q) = %v, want %v", tc.from, tc.sender, got, tc.want)
		}
	}
	if ownMessage(&message.DisplayMessage{From: "Anonymous"}, &user.User{Handle: "Anonymous"}, "Anonymous") {
		t.Error("a caller whose handle is the anonymous name owns anonymous posts")
	}
}

func TestEditedLine(t *testing.T) {
	when := time.Date(2026, 11, 12, 13, 5, 0, 0, time.UTC)
	got := withEditedLine("hello\n\n", "Caller", when)
	if got != "hello\n\n * Edited by Caller on 11/12/26 at 1:05 pm" {
		t.Errorf("withEditedLine = %q", got)
	}
	if back := withoutEditedLine(got); back != "hello" {
		t.Errorf("withoutEditedLine = %q, want hello", back)
	}
	for _, body := range []string{"hello", " * Edited by nobody", "x\n * Edited by Caller on yesterday"} {
		if back := withoutEditedLine(body); back != body {
			t.Errorf("withoutEditedLine(%q) = %q, want it unchanged", body, back)
		}
	}
	if back := withoutEditedLine(" * Edited by Caller on 11/12/26 at 1:05 pm"); back != "" {
		t.Errorf("a body that is only the edited line: %q", back)
	}
}
