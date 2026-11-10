package menu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// privmailSubjects is the PRIVMAIL area's subjects in order.
func privmailSubjects(env *menuEnv) string {
	var s []string
	for i := 1; i <= env.msgCount(privmailAreaID); i++ {
		s = append(s, env.mustMsg(privmailAreaID, i).Subject)
	}
	return strings.Join(s, ",")
}

// readThenDelete is READPRIVMAIL followed by DELETEPRIVMAIL in the same
// call, as a caller reads a mail, leaves the reader and presses D on the
// mail menu. Quitting the reader leaves the caller on the menu (any next
// action but LOGOFF or a GOTO).
func readThenDelete(c *cmdCtx, args string) (*user.User, string, error) {
	if u, next, err := runReadPrivateMail(c, ""); err != nil || next == "LOGOFF" || strings.HasPrefix(next, "GOTO:") {
		return u, next, err
	}
	return runDeletePrivateMail(c, "")
}

// TestDeletePrivateMailCurrent: the mail the caller last read is their
// current mail, and D deletes it after a Y, without asking for a number.
func TestDeletePrivateMailCurrent(t *testing.T) {
	env := newMsgEnv(t)
	seedPrivmail(env)

	r := env.run(readThenDelete, env.caller, "", "QY")
	if r.has("Mail # to delete") {
		t.Errorf("asked for a number with a current mail; output:\n%s", r.text())
	}
	if !r.has("for-caller-1", "Delete this mail?", "has been deleted") {
		t.Errorf("current mail not offered and deleted; output:\n%s", r.text())
	}
	if got := privmailSubjects(env); got != "for-sysop,for-caller-2,public-note" {
		t.Errorf("PRIVMAIL after delete = %s", got)
	}
}

// TestDeletePrivateMailByNumber: with no current mail, D asks which mail by
// the number LISTPRIVMAIL shows; ? lists only the caller's own mail.
func TestDeletePrivateMailByNumber(t *testing.T) {
	env := newMsgEnv(t)
	seedPrivmail(env)

	r := env.runCmd("DELETEPRIVMAIL", env.caller, "", "?\r3\rY")
	if !r.has("Mail # to delete", "for-caller-1", "for-caller-2") {
		t.Errorf("the caller's mail wasn't listed; output:\n%s", r.text())
	}
	if r.has("for-sysop") || r.has("public-note") {
		t.Errorf("? listed mail that isn't the caller's; output:\n%s", r.text())
	}
	if got := privmailSubjects(env); got != "for-caller-1,for-sysop,public-note" {
		t.Errorf("PRIVMAIL after delete = %s", got)
	}
}

// TestDeletePrivateMailNotTheirs: a number that isn't the caller's mail (the
// sysop's, or a public message) is refused, and Enter leaves.
func TestDeletePrivateMailNotTheirs(t *testing.T) {
	env := newMsgEnv(t)
	seedPrivmail(env)

	r := env.runCmd("DELETEPRIVMAIL", env.caller, "", "2\r4\r\r")
	if n := strings.Count(r.text(), "isn't one of your mails"); n != 2 {
		t.Errorf("refusals = %d, want 2; output:\n%s", n, r.text())
	}
	if r.has("Delete this mail?") {
		t.Errorf("offered to delete someone else's mail; output:\n%s", r.text())
	}
	if n := env.msgCount(privmailAreaID); n != 4 {
		t.Errorf("PRIVMAIL has %d messages, want 4", n)
	}
}

// TestDeletePrivateMailRace: while the caller is at the Y/N, another session
// deletes their mail and packs, moving the sysop's into its number. Y must
// not delete that.
func TestDeletePrivateMailRace(t *testing.T) {
	env := newMsgEnv(t)
	seedPrivmail(env)
	env.whenOutput("Delete this mail?", func() {
		_ = env.e.MessageMgr.DeleteMessage(privmailAreaID, 1)
		_ = env.e.MessageMgr.PackAndLinkArea(privmailAreaID)
	})

	r := env.runCmd("DELETEPRIVMAIL", env.caller, "", "1\rY")
	if !r.has("Nothing was deleted") {
		t.Errorf("the caller wasn't told; output:\n%s", r.text())
	}
	if got := privmailSubjects(env); got != "for-sysop,for-caller-2,public-note" {
		t.Errorf("PRIVMAIL = %s, want the sysop's mail kept", got)
	}
}

// TestDeletePrivateMailNone: a caller with no mail is told so.
func TestDeletePrivateMailNone(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(privmailAreaID, testMsg{from: "Caller", to: "Sysop", subject: "for-sysop", private: true})

	r := env.runCmd("DELETEPRIVMAIL", env.caller, "", "")
	if !r.has("You have no mail.") {
		t.Errorf("output:\n%s", r.text())
	}
	if n := env.msgCount(privmailAreaID); n != 1 {
		t.Errorf("PRIVMAIL has %d messages, want 1", n)
	}
}

// The stock mail menu binds D, the art's "Delete Current Mail", to
// DELETEPRIVMAIL.
func TestStockEmailMenuBindsDelete(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "menus", "v3", "cfg", "EMAILM.CFG"))
	if err != nil {
		t.Fatal(err)
	}
	var items []struct{ KEYS, CMD string }
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.KEYS == "D" {
			if it.CMD != "RUN:DELETEPRIVMAIL" {
				t.Errorf("D runs %q, want RUN:DELETEPRIVMAIL", it.CMD)
			}
			return
		}
	}
	t.Error("EMAILM.CFG has no D")
}
