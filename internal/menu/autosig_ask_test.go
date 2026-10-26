package menu

import (
	"strings"
	"testing"
)

// TestAutoSigAskCompose pins "ask each post": the caller is asked after
// saving, N posts the body alone and Y adds the signature.
func TestAutoSigAskCompose(t *testing.T) {
	env := newMsgEnv(t)
	env.caller.AutoSignature = "-- Carl"
	env.caller.AutoSigAsk = true

	r := env.runCmd("COMPOSEMSG", env.caller, "", "One\r\rfirst\x1aN")
	if !r.has("Add your Auto-Signature?") {
		t.Fatalf("not asked about the signature; output:\n%s", r.text())
	}
	if body := env.mustMsg(generalAreaID, 1).Body; strings.Contains(body, "-- Carl") {
		t.Errorf("declined signature was added: %q", body)
	}

	env.runCmd("COMPOSEMSG", env.caller, "", "Two\r\rsecond\x1aY")
	if body := env.mustMsg(generalAreaID, 2).Body; !strings.HasSuffix(strings.TrimSpace(body), "-- Carl") {
		t.Errorf("accepted signature missing: %q", body)
	}
}

// TestAutoSigNotAskedByDefault pins that a caller who hasn't asked to be
// asked is signed without a prompt, as before.
func TestAutoSigNotAskedByDefault(t *testing.T) {
	env := newMsgEnv(t)
	env.caller.AutoSignature = "-- Carl"

	r := env.runCmd("COMPOSEMSG", env.caller, "", "One\r\rfirst\x1a")
	if r.has("Add your Auto-Signature?") {
		t.Errorf("asked although AutoSigAsk is off; output:\n%s", r.text())
	}
	if body := env.mustMsg(generalAreaID, 1).Body; !strings.HasSuffix(strings.TrimSpace(body), "-- Carl") {
		t.Errorf("signature missing: %q", body)
	}
}

// TestAutoSigAskNoSigNoPrompt pins that asking is skipped when there is no
// signature to add.
func TestAutoSigAskNoSigNoPrompt(t *testing.T) {
	env := newMsgEnv(t)
	env.caller.AutoSigAsk = true

	r := env.runCmd("COMPOSEMSG", env.caller, "", "One\r\rfirst\x1a")
	if r.has("Add your Auto-Signature?") {
		t.Errorf("asked with no signature set; output:\n%s", r.text())
	}
	if n := env.msgCount(generalAreaID); n != 1 {
		t.Errorf("GENERAL has %d messages, want 1", n)
	}
}

// TestAutoSigAskReply covers a reply from the message reader.
func TestAutoSigAskReply(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(generalAreaID, testMsg{from: "Sysop", to: "All", subject: "hello"})
	env.caller.AutoSignature = "-- Carl"
	env.caller.AutoSigAsk = true

	env.runCmd("READMSGS", env.caller, "", "Rhi\x1aNQ")
	if body := env.mustMsg(generalAreaID, 2).Body; strings.Contains(body, "-- Carl") || !strings.Contains(body, "hi") {
		t.Errorf("reply body = %q, want the text without the signature", body)
	}
}

// TestAutoSigAskPrivateMail covers SENDPRIVMAIL.
func TestAutoSigAskPrivateMail(t *testing.T) {
	env := newMsgEnv(t)
	env.caller.AutoSignature = "-- Carl"
	env.caller.AutoSigAsk = true

	env.runCmd("SENDPRIVMAIL", env.caller, "", "sysop\rLunch?\rNoon.\x1aN")
	if body := env.mustMsg(privmailAreaID, 1).Body; strings.Contains(body, "-- Carl") {
		t.Errorf("declined signature was added: %q", body)
	}
}

// TestKonfigAutoSigAskToggle pins A at the Auto-Signature choice: it switches
// asking on and off, and the field shows when it is on.
func TestKonfigAutoSigAskToggle(t *testing.T) {
	env := newMenuEnv(t)
	env.caller.AutoSignature = "-- Carl"

	r := env.runCmd("USERCONFIG", env.caller, "", "faq")
	if !env.mustDiskUser(env.caller.ID).AutoSigAsk {
		t.Fatalf("A didn't switch asking on\n%s", r.text())
	}
	if !r.has("You'll be asked before your Auto-Signature is added.", "1 line, ask") {
		t.Errorf("status or value missing:\n%s", r.text())
	}

	env.caller = env.mustDiskUser(env.caller.ID)
	r = env.runCmd("USERCONFIG", env.caller, "", "faq")
	if env.mustDiskUser(env.caller.ID).AutoSigAsk {
		t.Errorf("second A didn't switch asking off\n%s", r.text())
	}
	if !r.has("Your Auto-Signature is added to every message.") {
		t.Errorf("no off notice:\n%s", r.text())
	}
}

// TestCfgAutoSigAskToggle pins A in CFG_AUTOSIG: it switches asking on and
// off and the screen says when it is on.
func TestCfgAutoSigAskToggle(t *testing.T) {
	env := newMenuEnv(t)

	r := env.runCmd("CFG_AUTOSIG", env.caller, "", "A\rQ\r")
	if !r.has("You'll be asked before your Auto-Signature is added.", "You're asked before it is added to each message.") {
		t.Errorf("notices missing:\n%s", r.text())
	}
	if !env.mustDiskUser(2).AutoSigAsk {
		t.Fatal("A didn't switch asking on")
	}

	r = env.runCmd("CFG_AUTOSIG", env.caller, "", "A\rQ\r")
	if !r.has("Your Auto-Signature is added to every message.") || env.mustDiskUser(2).AutoSigAsk {
		t.Errorf("second A didn't switch asking off:\n%s", r.text())
	}
}
