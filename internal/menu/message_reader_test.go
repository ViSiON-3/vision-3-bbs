package menu

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestBuildNameWithAddr_ShortFits(t *testing.T) {
	if got := buildNameWithAddr("Bob", "21:1/100"); got != "Bob (21:1/100)" {
		t.Errorf("buildNameWithAddr = %q, want %q", got, "Bob (21:1/100)")
	}
}

func TestBuildNameWithAddr_TruncatesPreservingSuffix(t *testing.T) {
	name := strings.Repeat("X", 60)
	got := buildNameWithAddr(name, "21:1/100")
	if !strings.HasSuffix(got, " (21:1/100)") {
		t.Errorf("address suffix not preserved: %q", got)
	}
	if utf8.RuneCountInString(got) > 45 {
		t.Errorf("result %d runes, want <= 45", utf8.RuneCountInString(got))
	}
}

func TestBuildNameWithAddr_ShortNameLongAddrNoPanic(t *testing.T) {
	// Regression: a short name with a very long address forced nameMax to 3,
	// and the old byte-slice name[:3] panicked when len(name) < 3.
	longAddr := strings.Repeat("a", 50)
	got := buildNameWithAddr("Jo", longAddr) // must not panic
	if !strings.HasSuffix(got, "("+longAddr+")") {
		t.Errorf("suffix not preserved: %q", got)
	}
	if !strings.HasPrefix(got, "Jo") {
		t.Errorf("short name should be preserved as-is: %q", got)
	}
}

func TestBuildNameWithAddr_MultibyteTruncationStaysValid(t *testing.T) {
	// Multibyte name that must be truncated: result must remain valid UTF-8
	// (the old byte-slice could split a rune).
	name := strings.Repeat("é", 50) // 50 runes, 100 bytes
	got := buildNameWithAddr(name, "1:2/3")
	if !utf8.ValidString(got) {
		t.Errorf("truncation produced invalid UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, " (1:2/3)") {
		t.Errorf("suffix not preserved: %q", got)
	}
}

func TestSaveHeaderSelection_Persists(t *testing.T) {
	um, err := user.NewUserManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewUserManager: %v", err)
	}
	u, err := um.AddUser("password", "Bob", "Real Name", "Loc")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	if err := saveHeaderSelection(um, u, 3, 1); err != nil {
		t.Fatalf("saveHeaderSelection: %v", err)
	}
	if u.MsgHdr != 3 {
		t.Errorf("in-memory MsgHdr = %d, want 3", u.MsgHdr)
	}
	got, ok := um.GetUserByID(u.ID)
	if !ok {
		t.Fatal("user not found after save")
	}
	if got.MsgHdr != 3 {
		t.Errorf("persisted MsgHdr = %d, want 3", got.MsgHdr)
	}
}

// Message-feature fixtures shared by the handler tests for the reader,
// composer, private mail, lists and newscan. They run on the shipped areas:
// GENERAL (ID 1, read s10, write s20) and PRIVMAIL (ID 2, s25, real names
// only), both in conference 1 (LOCAL).
const (
	generalAreaID  = 1
	privmailAreaID = 2
)

// newMsgEnv is newMenuEnv with both users sitting in GENERAL, using header
// style 2 so the reader never opens the header picker, and the caller raised
// to level 30 so they may post in GENERAL and use PRIVMAIL.
func newMsgEnv(t *testing.T) *menuEnv {
	t.Helper()
	env := newMenuEnv(t)
	users := defaultTestUsers()
	for _, u := range users {
		u.MsgHdr = 2
		u.CurrentMsgConferenceID = 1
		u.CurrentMsgConferenceTag = "LOCAL"
		u.CurrentMessageAreaID = generalAreaID
		u.CurrentMessageAreaTag = "GENERAL"
	}
	users[1].AccessLevel = 30
	env.writeUsers(users...)
	return env
}

// testMsg is one message to seed; its body is Subject + "-body".
type testMsg struct {
	from, to, subject string
	private           bool
}

// postMsgs seeds msgs into areaID through the message manager, in order.
func (env *menuEnv) postMsgs(areaID int, msgs ...testMsg) {
	env.t.Helper()
	for _, m := range msgs {
		add := env.e.MessageMgr.AddMessage
		if m.private {
			add = env.e.MessageMgr.AddPrivateMessage
		}
		if _, err := add(areaID, m.from, m.to, m.subject, m.subject+"-body", ""); err != nil {
			env.t.Fatalf("seed %q: %v", m.subject, err)
		}
	}
}

// generalMsgs posts n public messages from Sysop to All in GENERAL with
// subjects subj-1..subj-n.
func (env *menuEnv) generalMsgs(n int) {
	env.t.Helper()
	for i := 1; i <= n; i++ {
		env.postMsgs(generalAreaID, testMsg{from: "Sysop", to: "All", subject: fmt.Sprintf("subj-%d", i)})
	}
}

// mustMsg reads message n of areaID, failing the test if it is missing.
func (env *menuEnv) mustMsg(areaID, n int) *message.DisplayMessage {
	env.t.Helper()
	m, err := env.e.MessageMgr.GetMessage(areaID, n)
	if err != nil {
		env.t.Fatalf("GetMessage(%d, %d): %v", areaID, n, err)
	}
	return m
}

// msgCount is areaID's message count.
func (env *menuEnv) msgCount(areaID int) int {
	env.t.Helper()
	n, err := env.e.MessageMgr.GetMessageCountForArea(areaID)
	if err != nil {
		env.t.Fatalf("GetMessageCountForArea(%d): %v", areaID, err)
	}
	return n
}

// diskLastRead is handle's last-read pointer in areaID as a freshly opened
// message manager sees it, so assertions check what reached the JAM base.
func (env *menuEnv) diskLastRead(areaID int, handle string) int {
	env.t.Helper()
	mm, err := message.NewMessageManager(env.dataDir(), env.cfgDir(), "TestBBS", nil)
	if err != nil {
		env.t.Fatalf("reopen message manager: %v", err)
	}
	defer func() { _ = mm.Close() }()
	lr, err := mm.GetLastRead(areaID, handle)
	if err != nil {
		env.t.Fatalf("GetLastRead: %v", err)
	}
	return lr
}

// markRead sets handle's last-read pointer in areaID to n.
func (env *menuEnv) markRead(areaID int, handle string, n int) {
	env.t.Helper()
	if err := env.e.MessageMgr.SetLastRead(areaID, handle, n); err != nil {
		env.t.Fatalf("SetLastRead: %v", err)
	}
}

// readerShown lists which of subj-1..subj-n the output rendered.
func readerShown(r runResult, n int) []int {
	var shown []int
	for i := 1; i <= n; i++ {
		if r.has(fmt.Sprintf("\"subj-%d\"", i)) {
			shown = append(shown, i)
		}
	}
	return shown
}

// TestMessageReaderNextAdvancesAndPersistsLastRead reads from the first
// unread message: N moves to the next one, N on the last ends the reader,
// and the last-read pointer lands on the last message shown.
func TestMessageReaderNextAdvancesAndPersistsLastRead(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(3)
	env.markRead(generalAreaID, "Caller", 1)

	r := env.runCmd("READMSGS", env.caller, "", "NN")
	if r.err != nil {
		t.Fatalf("READMSGS: %v", r.err)
	}
	if got := readerShown(r, 3); !slices.Equal(got, []int{2, 3}) {
		t.Errorf("messages shown = %v, want [2 3] (starting at the first unread)", got)
	}
	if !r.has("End of messages.") {
		t.Errorf("N on the last message should say so; output:\n%s", r.text())
	}
	if r.next != "" {
		t.Errorf("next = %q, want \"\" when reading runs off the end", r.next)
	}
	if lr := env.diskLastRead(generalAreaID, "Caller"); lr != 3 {
		t.Errorf("persisted lastread = %d, want 3", lr)
	}
}

// TestMessageReaderPrevAndFirstMessage checks S steps back one message, that
// S on message 1 stays put with a notice, and that paging back never moves
// the last-read pointer backwards.
func TestMessageReaderPrevAndFirstMessage(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(3)
	env.markRead(generalAreaID, "Caller", 1)

	// Start on 2, S to 1, S again (already first), Q.
	r := env.runCmd("READMSGS", env.caller, "", "SSQ")
	if got := readerShown(r, 3); !slices.Equal(got, []int{1, 2}) {
		t.Errorf("messages shown = %v, want [1 2]", got)
	}
	if !r.has("Already at first message.") {
		t.Errorf("S on message 1 should say so; output:\n%s", r.text())
	}
	if r.next != "QUIT_NEWSCAN" {
		t.Errorf("next = %q, want QUIT_NEWSCAN after Q", r.next)
	}
	if lr := env.diskLastRead(generalAreaID, "Caller"); lr != 2 {
		t.Errorf("lastread = %d, want 2 (paging back must not lower it)", lr)
	}
}

// TestMessageReaderJump checks J jumps to the typed message number, that an
// out-of-range number is refused without moving, and that rereading old
// messages leaves the last-read pointer where it was.
func TestMessageReaderJump(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(5)
	env.markRead(generalAreaID, "Caller", 5)

	// All read, so READMSGS asks which message: 1. Jump to 4, then try 9.
	r := env.runCmd("READMSGS", env.caller, "", "1\rJ4\rJ9\rQ")
	if got := readerShown(r, 5); !slices.Equal(got, []int{1, 4}) {
		t.Errorf("messages shown = %v, want [1 4]", got)
	}
	if !r.has("Jump to message # (1/5)") {
		t.Errorf("jump prompt missing the message count; output:\n%s", r.text())
	}
	if !r.has("Invalid message number!") {
		t.Errorf("jump to 9 of 5 should be refused; output:\n%s", r.text())
	}
	if lr := env.diskLastRead(generalAreaID, "Caller"); lr != 5 {
		t.Errorf("lastread = %d, want 5 (reading old messages must not lower it)", lr)
	}
}

// TestMessageReaderThread checks T forward finds the next message in the
// thread (skipping unrelated subjects), T backward finds the earlier one,
// and a direction with no match says so.
func TestMessageReaderThread(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(generalAreaID,
		testMsg{from: "Sysop", to: "All", subject: "cats"},
		testMsg{from: "Sysop", to: "All", subject: "dogs"},
		testMsg{from: "Caller", to: "Sysop", subject: "Re: cats"},
	)

	// Read from 1: thread forward lands on 3 ("Re: cats"), thread forward
	// again finds nothing, thread back returns to 1.
	r := env.runCmd("READMSGS", env.caller, "", "TFTFTBQ")
	txt := r.text()
	if strings.Contains(txt, "\"dogs\"") {
		t.Errorf("thread search stopped on an unrelated subject; output:\n%s", txt)
	}
	if !r.has("\"Re: cats\"") {
		t.Errorf("thread forward did not reach the reply; output:\n%s", txt)
	}
	if !r.has("No forward thread found!") {
		t.Errorf("thread forward past the last reply should say so; output:\n%s", txt)
	}
	if n := strings.Count(txt, "\"cats\""); n < 2 {
		t.Errorf("thread back should redisplay the original (shown %d times)", n)
	}
}

// TestMessageReaderReplyPostsThreadedReply replies to a message through the
// editor: the reply lands in the same base addressed to the author, with a
// "Re:" subject, the typed body, a link to its parent, and the caller's post
// count saved.
func TestMessageReaderReplyPostsThreadedReply(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(generalAreaID, testMsg{from: "Sysop", to: "All", subject: "hello"})

	r := env.runCmd("READMSGS", env.caller, "", "RThanks for this\x1aQ")
	if r.err != nil {
		t.Fatalf("READMSGS: %v", r.err)
	}
	if !r.has("Reply posted successfully!") {
		t.Fatalf("reply not confirmed; output:\n%s", r.text())
	}
	if n := env.msgCount(generalAreaID); n != 2 {
		t.Fatalf("GENERAL has %d messages, want 2", n)
	}
	reply := env.mustMsg(generalAreaID, 2)
	if reply.From != "Caller" || reply.To != "Sysop" || reply.Subject != "Re: hello" {
		t.Errorf("reply from/to/subject = %q/%q/%q, want Caller/Sysop/Re: hello", reply.From, reply.To, reply.Subject)
	}
	if !strings.Contains(reply.Body, "Thanks for this") {
		t.Errorf("reply body = %q, want the typed text", reply.Body)
	}
	if reply.ReplyToNum != 1 || reply.IsPrivate {
		t.Errorf("reply ReplyToNum=%d private=%v, want 1/false", reply.ReplyToNum, reply.IsPrivate)
	}
	if got := env.mustDiskUser(2).MessagesPosted; got != 1 {
		t.Errorf("saved MessagesPosted = %d, want 1", got)
	}
}

// TestMessageReaderReplyAbortedPostsNothing aborts the reply editor: nothing
// is written and the reader says the reply was cancelled.
func TestMessageReaderReplyAbortedPostsNothing(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(generalAreaID, testMsg{from: "Sysop", to: "All", subject: "hello"})

	// CTRL-A, Y confirms the abort.
	r := env.runCmd("READMSGS", env.caller, "", "Rdraft\x01YQ")
	if !r.has("Reply cancelled.") {
		t.Errorf("aborted reply not reported; output:\n%s", r.text())
	}
	if n := env.msgCount(generalAreaID); n != 1 {
		t.Errorf("GENERAL has %d messages after an aborted reply, want 1", n)
	}
}

// TestMessageReaderReplyAppendsSignature checks a saved auto-signature is
// added below the reply body.
func TestMessageReaderReplyAppendsSignature(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(generalAreaID, testMsg{from: "Sysop", to: "All", subject: "hello"})
	env.caller.AutoSignature = "-- Carl"

	env.runCmd("READMSGS", env.caller, "", "Rhi\x1aQ")
	if body := env.mustMsg(generalAreaID, 2).Body; !strings.Contains(body, "hi") || !strings.HasSuffix(strings.TrimSpace(body), "-- Carl") {
		t.Errorf("reply body = %q, want the text followed by the signature", body)
	}
}

// TestMessageReaderPostFromReader checks P composes a new message in the
// area being read and the reader carries on with the new total.
func TestMessageReaderPostFromReader(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(1)

	// P: title, To (Enter = All), body, save; then N reaches the new post.
	r := env.runCmd("READMSGS", env.caller, "", "Pnew topic\r\rfresh body\x1aNQ")
	if !r.has("Message Posted!") {
		t.Fatalf("post from the reader not confirmed; output:\n%s", r.text())
	}
	m := env.mustMsg(generalAreaID, 2)
	if m.From != "Caller" || m.To != "All" || m.Subject != "new topic" || !strings.Contains(m.Body, "fresh body") {
		t.Errorf("posted message = %q/%q/%q/%q", m.From, m.To, m.Subject, m.Body)
	}
	if !r.has("\"new topic\"") {
		t.Errorf("N after posting should reach the new message; output:\n%s", r.text())
	}
}

// TestMessageReaderSysopDelete checks D (offered to co-sysops and up) asks
// first, deletes on Y and packs the base, and is ignored for a caller.
func TestMessageReaderSysopDelete(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(3)
	env.markRead(generalAreaID, "Caller", 3)

	// Sysop reads from 1: D then N keeps it, D then Y deletes it.
	r := env.runCmd("READMSGS", env.sysop, "", "DNDYQ")
	if !r.has("Delete this message?") {
		t.Errorf("delete should be confirmed; output:\n%s", r.text())
	}
	if n := env.msgCount(generalAreaID); n != 2 {
		t.Fatalf("GENERAL has %d messages after delete, want 2", n)
	}
	if got := env.mustMsg(generalAreaID, 1).Subject; got != "subj-2" {
		t.Errorf("message 1 after delete+pack = %q, want subj-2", got)
	}

	// The caller's D is not a command: nothing is asked or deleted.
	r = env.runCmd("READMSGS", env.caller, "", "1\rDQ")
	if r.has("Delete this message?") {
		t.Error("a caller was offered delete")
	}
	if n := env.msgCount(generalAreaID); n != 2 {
		t.Errorf("caller's D changed the count to %d", n)
	}
}

// TestMessageReaderSysopDeleteLastMessageEndsReader deletes the only
// message: the reader has nothing left to show and returns.
func TestMessageReaderSysopDeleteLastMessageEndsReader(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(1)

	r := env.runCmd("READMSGS", env.sysop, "", "DY")
	if r.err != nil {
		t.Fatalf("READMSGS: %v", r.err)
	}
	if n := env.msgCount(generalAreaID); n != 0 {
		t.Errorf("GENERAL has %d messages, want 0", n)
	}
}

// TestMessageReaderHelpMailAndEsc checks ? shows the command help (with
// Delete only for a sysop), M reports mail reply as unavailable, and ESC
// leaves the reader like Q.
func TestMessageReaderHelpMailAndEsc(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(1)

	r := env.runCmd("READMSGS", env.caller, "", "?M\x1b")
	if !r.has("Message Reader Help", "Thread Search") {
		t.Errorf("? did not show help; output:\n%s", r.text())
	}
	if r.has("elete Message") {
		t.Error("caller's help lists Delete")
	}
	if !r.has("Mail reply not yet implemented.") {
		t.Errorf("M notice missing; output:\n%s", r.text())
	}
	if r.next != "QUIT_NEWSCAN" {
		t.Errorf("next = %q, want QUIT_NEWSCAN after ESC", r.next)
	}

	r = env.runCmd("READMSGS", env.sysop, "", "?Q")
	if !r.has("elete Message") {
		t.Error("sysop's help should list Delete")
	}
}

// TestMessageReaderLightbarSelectsCommand checks the command bar: an
// unbound key opens it, Right moves to Reply... and Enter on Next advances;
// a hotkey typed in the bar runs that command.
func TestMessageReaderLightbarSelectsCommand(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(3)
	env.markRead(generalAreaID, "Caller", 3)

	// x opens the bar on Next, Enter picks it (1 -> 2). Right arrow enters
	// the bar at Reply; Left back to Next, Enter (2 -> 3). Then q in a
	// freshly opened bar quits.
	r := env.runCmd("READMSGS", env.caller, "", "1\rx\r\x1b[C\x1b[D\rxq")
	if got := readerShown(r, 3); !slices.Equal(got, []int{1, 2, 3}) {
		t.Errorf("messages shown = %v, want [1 2 3]", got)
	}
	if r.next != "QUIT_NEWSCAN" {
		t.Errorf("next = %q, want QUIT_NEWSCAN from the bar's Quit", r.next)
	}
}

// TestMessageReaderScrollsLongBody checks the body scrolls: a line past the
// first screen only appears after Down/PageDown, and Enter still means Next.
func TestMessageReaderScrollsLongBody(t *testing.T) {
	env := newMsgEnv(t)
	var body strings.Builder
	for i := 1; i <= 60; i++ {
		fmt.Fprintf(&body, "line-%02d\n", i)
	}
	if _, err := env.e.MessageMgr.AddMessage(generalAreaID, "Sysop", "All", "long", body.String(), ""); err != nil {
		t.Fatal(err)
	}
	env.generalMsgs(1)
	env.markRead(generalAreaID, "Caller", 2)

	r := env.runCmd("READMSGS", env.caller, "", "1\rQ")
	if r.has("line-60") {
		t.Fatal("line 60 visible without scrolling; the test needs a longer body")
	}

	// Down, PageDown x5, PageUp, Up, CTRL-X, CTRL-C, then Enter = Next.
	r = env.runCmd("READMSGS", env.caller, "", "1\r\x1b[B\x1b[6~\x1b[6~\x1b[6~\x1b[6~\x1b[6~\x1b[5~\x1b[A\x18\x03\rQ")
	if !r.has("line-60") {
		t.Errorf("scrolling never reached the last line; output:\n%s", r.text())
	}
	if !r.has("\"subj-1\"") {
		t.Errorf("Enter should move to the next message; output:\n%s", r.text())
	}
}

// TestMessageReaderListCommand checks L opens the area's message list from
// inside the reader and Q in the list returns to the reader.
func TestMessageReaderListCommand(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(2)
	env.markRead(generalAreaID, "Caller", 2)

	r := env.runCmd("READMSGS", env.caller, "", "1\rLqQ")
	if r.next != "QUIT_NEWSCAN" {
		t.Errorf("next = %q, want the reader's Q after leaving the list", r.next)
	}
	if n := strings.Count(r.text(), "subj-2"); n < 1 {
		t.Errorf("L should list the area's messages; output:\n%s", r.text())
	}
}

// TestMessageReaderCP437 reads in CP437: the header and body still render
// and the reader behaves the same.
func TestMessageReaderCP437(t *testing.T) {
	env := newMsgEnv(t)
	env.outputMode = ansi.OutputModeCP437
	env.generalMsgs(2)

	r := env.runCmd("READMSGS", env.caller, "", "NQ")
	if got := readerShown(r, 2); !slices.Equal(got, []int{1, 2}) {
		t.Errorf("messages shown = %v, want [1 2]", got)
	}
	if !r.has("subj-2-body") {
		t.Errorf("body missing in CP437 output:\n%s", r.text())
	}
}
