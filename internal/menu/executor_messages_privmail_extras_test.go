package menu

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// readThen is READPRIVMAIL followed by fn in the same call, as a caller
// reads a mail, leaves the reader and presses a key on the mail menu.
func readThen(fn RunnableFunc) RunnableFunc {
	return func(c *cmdCtx, args string) (*user.User, string, error) {
		if u, next, err := runReadPrivateMail(c, ""); err != nil || next == "LOGOFF" || strings.HasPrefix(next, "GOTO:") {
			return u, next, err
		}
		return fn(c, "")
	}
}

// privmailMsg is the PRIVMAIL message with subject, failing if there is none.
func privmailMsg(env *menuEnv, subject string) *message.DisplayMessage {
	env.t.Helper()
	for i := 1; i <= env.msgCount(privmailAreaID); i++ {
		if m := env.mustMsg(privmailAreaID, i); m.Subject == subject {
			return m
		}
	}
	env.t.Fatalf("no PRIVMAIL message %q; have %s", subject, privmailSubjects(env))
	return nil
}

func lastReadOf(env *menuEnv, handle string) int {
	env.t.Helper()
	n, err := env.e.MessageMgr.GetLastRead(privmailAreaID, handle)
	if err != nil {
		env.t.Fatal(err)
	}
	return n
}

// V shows only the mail the caller sent, and reading it leaves their own
// lastread alone, so their unread mail stays unread.
func TestSentPrivateMailShowsOnlySent(t *testing.T) {
	env := newMsgEnv(t)
	seedPrivmail(env)

	r := env.run(runSentPrivateMail, env.caller, "", "NQ")
	if !r.has("for-sysop") || r.has("for-caller-1") || r.has("for-caller-2") || r.has("public-note") {
		t.Errorf("output:\n%s", r.text())
	}
	if n := lastReadOf(env, "Caller"); n != 0 {
		t.Errorf("caller's lastread = %d after reading sent mail, want 0", n)
	}

	empty := newMsgEnv(t)
	if r := empty.run(runSentPrivateMail, empty.caller, "", "Q"); !r.has("You haven't sent any mail.") {
		t.Errorf("no sent mail: output:\n%s", r.text())
	}
}

// A letter its recipient hasn't read can be unsent with D; once they have,
// D does nothing.
func TestSentPrivateMailUnsend(t *testing.T) {
	env := newMsgEnv(t)
	seedPrivmail(env)
	r := env.run(runSentPrivateMail, env.caller, "", "DYQ")
	if !r.has("Delete this message?") {
		t.Errorf("output:\n%s", r.text())
	}
	if got := privmailSubjects(env); strings.Contains(got, "for-sysop") {
		t.Errorf("PRIVMAIL after unsend = %s", got)
	}

	env = newMsgEnv(t)
	seedPrivmail(env)
	env.markRead(privmailAreaID, "Sysop", 2)
	r = env.run(runSentPrivateMail, env.caller, "", "DYQ")
	if r.has("Delete this message?") {
		t.Errorf("a read letter was offered to unsend; output:\n%s", r.text())
	}
	if got := privmailSubjects(env); !strings.Contains(got, "for-sysop") {
		t.Errorf("PRIVMAIL = %s, want for-sysop kept", got)
	}
}

// Unsending is the mailbox's: a private letter the caller left in a public
// area isn't theirs to delete there, read or not.
func TestUnsendOnlyInMailbox(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(generalAreaID, testMsg{from: "Caller", to: "Sysop", subject: "private-in-general", private: true})
	r := env.runCmd("READMSGS", env.caller, "", "DYQ")
	if r.has("Delete this message?") || env.msgCount(generalAreaID) != 1 {
		t.Errorf("unsent outside the mailbox; output:\n%s", r.text())
	}
}

// If the recipient reads the letter while the unsend is being confirmed,
// nothing is deleted and the caller is told why.
func TestSentPrivateMailReadMeanwhile(t *testing.T) {
	env := newMsgEnv(t)
	seedPrivmail(env)
	env.whenOutput("Delete this message?", func() { env.markRead(privmailAreaID, "Sysop", 2) })
	r := env.run(runSentPrivateMail, env.caller, "", "DYQ")
	if !r.has("That mail has been read, so it can't be unsent now.") {
		t.Errorf("output:\n%s", r.text())
	}
	if got := privmailSubjects(env); !strings.Contains(got, "for-sysop") {
		t.Errorf("PRIVMAIL = %s, want for-sysop kept", got)
	}
}

// C copies the current mail to another user as "Fwd: subject", with a
// header naming the original sender and recipient, then its text; the
// caller's own is kept.
func TestCopyPrivateMail(t *testing.T) {
	env := newMsgEnv(t)
	seedPrivmail(env)

	r := env.run(readThen(runCopyPrivateMail), env.caller, "", "Qsysop\rN")
	if !r.has("Mail copied to Sysop.") {
		t.Fatalf("output:\n%s", r.text())
	}
	m := privmailMsg(env, "Fwd: for-caller-1")
	if !m.IsPrivate || m.From != "Caller" || m.To != "Sysop" {
		t.Errorf("copy from %q to %q private %v", m.From, m.To, m.IsPrivate)
	}
	if !strings.HasPrefix(m.Body, "--- Forwarded mail ---\nFrom: Sysop\nTo: Caller\nDate: ") ||
		!strings.Contains(m.Body, "\nSubject: for-caller-1\n\nfor-caller-1-body") {
		t.Errorf("copy body = %q", m.Body)
	}
	privmailMsg(env, "for-caller-1") // still there
}

// W forwards: a note of the caller's goes on top, and their own copy is
// deleted. Without a current mail, it asks which, by number.
func TestForwardPrivateMail(t *testing.T) {
	env := newMsgEnv(t)
	seedPrivmail(env)

	r := env.run(runForwardPrivateMail, env.caller, "", "3\rSysop\rYsee below\x1a")
	if !r.has("Mail # to forward", "Mail forwarded to Sysop.") {
		t.Fatalf("output:\n%s", r.text())
	}
	m := privmailMsg(env, "Fwd: for-caller-2")
	if !strings.HasPrefix(m.Body, "see below\n\n--- Forwarded mail ---\n") || !strings.HasSuffix(m.Body, "for-caller-2-body") {
		t.Errorf("forward body = %q", m.Body)
	}
	if got := privmailSubjects(env); strings.Contains(got, "for-caller-2,") || strings.HasSuffix(got, ",for-caller-2") {
		t.Errorf("PRIVMAIL after forward = %s, want for-caller-2 gone", got)
	}
}

// A forward to nobody sends nothing and keeps the mail; one whose original
// is deleted elsewhere meanwhile still sends, and says the original was
// kept as it was changed.
func TestForwardPrivateMailEdgeCases(t *testing.T) {
	env := newMsgEnv(t)
	seedPrivmail(env)
	before := privmailSubjects(env)
	r := env.run(readThen(runForwardPrivateMail), env.caller, "", "Qnobody\r")
	if !r.has("User 'nobody' not found.") || privmailSubjects(env) != before {
		t.Errorf("subjects %s; output:\n%s", privmailSubjects(env), r.text())
	}

	// A deleted account isn't sent to either.
	env = newMsgEnv(t)
	seedPrivmail(env)
	sysop, _ := env.um.GetUser("Sysop")
	sysop.DeletedUser = true
	if err := env.um.UpdateUser(sysop); err != nil {
		t.Fatal(err)
	}
	before = privmailSubjects(env)
	r = env.run(readThen(runCopyPrivateMail), env.caller, "", "QSysop\r")
	if !r.has("User 'Sysop' not found.") || privmailSubjects(env) != before {
		t.Errorf("deleted recipient: subjects %s; output:\n%s", privmailSubjects(env), r.text())
	}

	env = newMsgEnv(t)
	seedPrivmail(env)
	env.whenOutput("Add a note", func() {
		if err := env.e.MessageMgr.DeleteMessage(privmailAreaID, 1); err != nil {
			t.Error(err)
		}
	})
	r = env.run(readThen(runForwardPrivateMail), env.caller, "", "QSysop\rN")
	if !r.has("but yours was changed elsewhere, so it was kept") {
		t.Errorf("output:\n%s", r.text())
	}
	privmailMsg(env, "Fwd: for-caller-1")

	env = newMsgEnv(t)
	seedPrivmail(env)
	before = privmailSubjects(env)
	r = env.run(readThen(runCopyPrivateMail), env.caller, "", "QSysop\rYdraft\x01Y")
	if !r.has("Nothing was copied.") || privmailSubjects(env) != before {
		t.Errorf("abandoned note: subjects %s; output:\n%s", privmailSubjects(env), r.text())
	}
}

func TestForwardedSubject(t *testing.T) {
	for in, want := range map[string]string{"hello": "Fwd: hello", "Fwd: hello": "Fwd: hello", "FWD:x": "FWD:x", "": "Fwd: "} {
		if got := forwardedSubject(in); got != want {
			t.Errorf("forwardedSubject(%q) = %q, want %q", in, got, want)
		}
	}
}

// The stock mail menu binds, visibly, every key its art shows, and the art
// shows no key the menu doesn't bind (#325).
func TestStockMailMenuBindsExtras(t *testing.T) {
	stock := filepath.Join("..", "..", "menus", "v3")
	data, err := os.ReadFile(filepath.Join(stock, "cfg", "EMAILM.CFG"))
	if err != nil {
		t.Fatal(err)
	}
	var items []struct {
		KEYS, CMD string
		HIDDEN    bool
	}
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, it := range items {
		got[it.KEYS] = it.CMD
		if it.HIDDEN {
			t.Errorf("%s is hidden", it.KEYS)
		}
	}
	want := map[string]string{
		"C": "RUN:COPYPRIVMAIL", "D": "RUN:DELETEPRIVMAIL", "W": "RUN:FORWARDPRIVMAIL",
		"L": "RUN:LISTPRIVMAIL", "N": "RUN:NMAILSCAN", "R": "RUN:READPRIVMAIL",
		"S": "RUN:SENDPRIVMAIL", "V": "RUN:SENTPRIVMAIL", "Q": "GOTO:MAIN",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("EMAILM.CFG binds %v, want %v", got, want)
	}

	// The art's options read "[K] Label" once its colour codes are dropped.
	art, err := os.ReadFile(filepath.Join(stock, "ansi", "EMAILM.ANS"))
	if err != nil {
		t.Fatal(err)
	}
	// The file keeps the length ViSiON/2's art had, so its SAUCE record,
	// whose FileSize (offset 90) counts the bytes before the EOF mark, holds.
	const size, sauceLen = 2066, 128
	eof := bytes.IndexByte(art, 0x1a)
	if _, ok := ansi.ReadSAUCE(art); !ok || len(art) != size || eof != size-sauceLen-1 ||
		int(binary.LittleEndian.Uint32(art[size-sauceLen+90:])) != eof {
		t.Fatalf("EMAILM.ANS: %d bytes, EOF mark at %d, SAUCE found %v; want %d bytes and a FileSize of %d", len(art), eof, ok, size, size-sauceLen-1)
	}
	plain := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAll(ansi.StripSAUCE(art), nil)
	shown := map[string]string{}
	for _, m := range regexp.MustCompile(`\[(.)\] ([A-Za-z ]+[a-z])`).FindAllSubmatch(plain, -1) {
		shown[string(m[1])] = string(m[2])
	}
	labels := map[string]string{
		"C": "Copy Mail to User", "D": "Delete Current Mail", "W": "Forward Mail",
		"L": "List All Mail", "N": "Newscan Your Mail", "R": "Read Your Mail",
		"S": "Send Mail", "V": "View Outgoing Mail", "Q": "Quit to Previous",
	}
	if !reflect.DeepEqual(shown, labels) {
		t.Errorf("EMAILM.ANS shows %v, want %v", shown, labels)
	}
}

// A letter its recipient opens between the unsend pre-check and the locked
// delete is kept, and the sender is told it was read (#1031): "still
// unread" is decided under the same lock as the delete.
func TestUnsendReadBeforeTheLock(t *testing.T) {
	env := newMsgEnv(t)
	seedPrivmail(env)
	beforeLockedDelete = func() {
		if err := env.e.MessageMgr.AdvanceLastRead(privmailAreaID, "Sysop", 2); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { beforeLockedDelete = nil })
	r := env.run(runSentPrivateMail, env.caller, "", "DYQ")
	if !r.has("Delete this message?", "That mail has been read, so it can't be unsent now. Nothing was deleted.") {
		t.Errorf("output:\n%s", r.text())
	}
	if got := privmailSubjects(env); !strings.Contains(got, "for-sysop") {
		t.Errorf("PRIVMAIL = %s, want for-sysop kept", got)
	}
}
