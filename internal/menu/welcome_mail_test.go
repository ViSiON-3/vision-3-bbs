package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// welcomeEnv is a message env with sendWelcomeMail set as given and the
// caller owed a welcome letter.
func welcomeEnv(t *testing.T, send bool) *menuEnv {
	t.Helper()
	env := newMsgEnv(t)
	cfg := env.e.GetServerConfig()
	cfg.SendWelcomeMail = send
	cfg.BoardName = "Test Board"
	env.e.SetServerConfig(cfg)
	env.caller.WelcomePending = true
	if err := env.um.UpdateUser(env.caller); err != nil {
		t.Fatal(err)
	}
	return env
}

// A validated account that is owed the letter gets the stock WELCOME.MSG as
// private mail from the SysOp, once.
func TestWelcomeMailSentOnceValidated(t *testing.T) {
	env := welcomeEnv(t, true)
	env.e.deliverWelcomeMail(env.um, env.caller, 1)

	if n := env.msgCount(privmailAreaID); n != 1 {
		t.Fatalf("PRIVMAIL has %d messages, want 1", n)
	}
	m := env.mustMsg(privmailAreaID, 1)
	if m.From != "Sysop" || m.To != "Caller" || m.Subject != "Welcome to Test Board" || !m.IsPrivate {
		t.Errorf("letter: from %q to %q subject %q private %v", m.From, m.To, m.Subject, m.IsPrivate)
	}
	for _, want := range []string{"|15Hello |14Caller|15, and welcome to |14Test Board|15!", "|07Sysop"} {
		if !strings.Contains(m.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, m.Body)
		}
	}
	if strings.Contains(m.Body, "Subject:") || strings.Contains(m.Body, "|UH") {
		t.Errorf("body keeps the subject line or a code:\n%s", m.Body)
	}
	if !m.VisibleTo("Caller", 2) {
		t.Error("the caller can't see their own welcome letter")
	}
	if env.caller.WelcomePending || env.mustDiskUser(2).WelcomePending {
		t.Error("WelcomePending is still set")
	}

	env.e.deliverWelcomeMail(env.um, env.caller, 1)
	if n := env.msgCount(privmailAreaID); n != 1 {
		t.Errorf("a second login sent another: %d messages", n)
	}
}

// The login sequence delivers it before its steps run, so NMAILSCAN counts it.
func TestWelcomeMailComesBeforeTheLoginSteps(t *testing.T) {
	env := welcomeEnv(t, true)
	env.run(runFullLoginSequence, env.caller, "", strings.Repeat("\r", 40))
	if got := privmailSubjects(env); got != "Welcome to Test Board" {
		t.Errorf("PRIVMAIL = %q", got)
	}
}

// Nothing is sent to an account still awaiting validation, which stays owed
// the letter; nor with the setting off, nor to the SysOp, and those two are
// no longer owed one.
func TestWelcomeMailNotSent(t *testing.T) {
	env := welcomeEnv(t, true)
	env.caller.Validated = false
	env.e.deliverWelcomeMail(env.um, env.caller, 1)
	if env.msgCount(privmailAreaID) != 0 || !env.caller.WelcomePending {
		t.Errorf("unvalidated: %d messages, pending %v", env.msgCount(privmailAreaID), env.caller.WelcomePending)
	}

	env.sysop.WelcomePending = true
	env.e.deliverWelcomeMail(env.um, env.sysop, 1)
	if env.msgCount(privmailAreaID) != 0 || env.sysop.WelcomePending {
		t.Errorf("sysop: %d messages, pending %v", env.msgCount(privmailAreaID), env.sysop.WelcomePending)
	}

	// An account that isn't owed one: every caller from before the setting.
	env.caller.Validated, env.caller.WelcomePending = true, false
	env.e.deliverWelcomeMail(env.um, env.caller, 1)
	if env.msgCount(privmailAreaID) != 0 {
		t.Error("an account that wasn't owed a letter got one")
	}

	off := welcomeEnv(t, false)
	off.e.deliverWelcomeMail(off.um, off.caller, 1)
	if off.msgCount(privmailAreaID) != 0 || off.mustDiskUser(2).WelcomePending {
		t.Errorf("setting off: %d messages, pending %v", off.msgCount(privmailAreaID), off.mustDiskUser(2).WelcomePending)
	}
}

// A missing or empty template sends nothing and doesn't ask again.
func TestWelcomeMailWithoutATemplate(t *testing.T) {
	env := welcomeEnv(t, true)
	env.e.MenuSetPath = t.TempDir()
	env.e.deliverWelcomeMail(env.um, env.caller, 1)
	if env.msgCount(privmailAreaID) != 0 || env.caller.WelcomePending {
		t.Errorf("missing: %d messages, pending %v", env.msgCount(privmailAreaID), env.caller.WelcomePending)
	}

	env.caller.WelcomePending = true
	dir := filepath.Join(env.e.MenuSetPath, "templates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, welcomeMailTemplate), []byte("Subject: hi\r\n\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env.e.deliverWelcomeMail(env.um, env.caller, 1)
	if env.msgCount(privmailAreaID) != 0 {
		t.Error("an empty template was sent")
	}
}

func TestWelcomeMailFromTemplate(t *testing.T) {
	cfg := config.ServerConfig{BoardName: "The Board"}
	u := &user.User{ID: 7, Handle: "New|Guy", RealName: "Ned New", AccessLevel: 10, PrivateNote: "seems dodgy"}
	sysop := &user.User{ID: 1, Handle: "Boss"}

	// CRLF, a subject line, an ANSI escape, a SAUCE-less EOF mark, colour
	// codes left alone, and |UN, which is not a code here.
	raw := "subject:  Hi |UH \r\n\r\n\x1b[1;31m|12Dear |NAME (|HANDLE), level |LEVEL at |BN.\r\n|UN -- |SYSOP\r\n\r\n\x1aignored"
	subject, body := welcomeMailFromTemplate([]byte(raw), cfg, u, sysop)
	if subject != "Hi New||Guy" {
		t.Errorf("subject = %q", subject)
	}
	if want := "\x1b[1;31m|12Dear Ned New (New||Guy), level 10 at The Board.\n|UN -- Boss\x1b[0m"; body != want {
		t.Errorf("body = %q\nwant   %q", body, want)
	}

	// No subject line: the default, and the first line stays in the body.
	subject, body = welcomeMailFromTemplate([]byte("Hello |UH\n"), cfg, u, sysop)
	if subject != "Welcome to The Board" || body != "Hello New||Guy" {
		t.Errorf("no subject line: %q, %q", subject, body)
	}
	if s, b := welcomeMailFromTemplate([]byte("Subject: x\n \n"), cfg, u, sysop); s != "" || b != "" {
		t.Errorf("empty template: %q, %q", s, b)
	}
}

// The stock template fits an 80-column reader.
func TestStockWelcomeTemplate(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "menus", "v3", "templates", welcomeMailTemplate))
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(raw), "\n") {
		if n := len(pipeColourRe.ReplaceAllString(line, "")); n > 79 {
			t.Errorf("line %d is %d columns wide", i+1, n)
		}
	}
}
