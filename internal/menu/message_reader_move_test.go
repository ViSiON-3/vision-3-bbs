package menu

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// addMoveArea adds an area of the given type to move messages into; acs is
// its read and write ACS.
func addMoveArea(env *menuEnv, tag, areaType, acs string) int {
	env.t.Helper()
	return addMoveAreaACS(env, tag, areaType, acs, acs)
}

// addMoveAreaACS is addMoveArea with separate read and write ACS.
func addMoveAreaACS(env *menuEnv, tag, areaType, read, write string) int {
	env.t.Helper()
	id, err := env.e.MessageMgr.AddArea(message.MessageArea{
		Tag: tag, Name: tag + " Area", AreaType: areaType, ACSRead: read, ACSWrite: write,
	})
	if err != nil {
		env.t.Fatalf("AddArea %s: %v", tag, err)
	}
	return id
}

// generalName is GENERAL's name as a moved line gives it.
func generalName(env *menuEnv) string {
	a, _ := env.e.MessageMgr.GetAreaByID(generalAreaID)
	return areaPlainName(a)
}

// A sysop moves a message with V, picking the area by tag: it leaves
// GENERAL, which is packed, and arrives in TECH ending with a line saying
// where it came from and who moved it.
func TestReaderSysopMovesMessage(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(2)
	tech := addMoveArea(env, "TECH", "local", "")

	r := env.runCmd("READMSGS", env.sysop, "", "?Vtech\rYQ")
	if !r.has("V Move to Another Area", "TECH Area", "Message moved to TECH Area.") {
		t.Errorf("output:\n%s", r.text())
	}
	if n := env.msgCount(generalAreaID); n != 1 || env.mustMsg(generalAreaID, 1).Subject != "subj-2" {
		t.Errorf("GENERAL has %d messages, want just subj-2", n)
	}
	if n := env.msgCount(tech); n != 1 {
		t.Fatalf("TECH has %d messages, want 1", n)
	}
	m := env.mustMsg(tech, 1)
	if m.Subject != "subj-1" || m.From != "Sysop" || !strings.HasPrefix(m.Body, "subj-1-body\n\n") ||
		!strings.Contains(m.Body, " * Moved from "+generalName(env)+" by Sysop on ") {
		t.Errorf("moved message = %q from %q body %q", m.Subject, m.From, m.Body)
	}
}

// An area's sponsor moves a message out of it, picking the area by number;
// a caller who isn't its sponsor gets no Move, by key or in the help.
func TestReaderSponsorMovesMessage(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(1)
	addMoveArea(env, "TECH", "local", "")

	r := env.runCmd("READMSGS", env.caller, "", "?VQ")
	if r.has("Move to Another Area") || r.has("Move this message to") {
		t.Errorf("a caller who isn't the sponsor was offered the move; output:\n%s", r.text())
	}

	env = newMsgEnv(t)
	env.generalMsgs(1)
	tech := addMoveArea(env, "TECH", "local", "")
	sponsorSetSponsor(env, generalAreaID, "caller")
	r = env.runCmd("READMSGS", env.caller, "", "V1\rY")
	if !r.has("Message moved to TECH Area.") {
		t.Errorf("output:\n%s", r.text())
	}
	if env.msgCount(generalAreaID) != 0 || env.msgCount(tech) != 1 {
		t.Errorf("GENERAL %d, TECH %d, want 0 and 1", env.msgCount(generalAreaID), env.msgCount(tech))
	}
	if b := env.mustMsg(tech, 1).Body; !strings.Contains(b, " by Caller on ") {
		t.Errorf("body = %q, want the sponsor named", b)
	}
}

// Only local areas the caller may read and post in are offered; one they
// name that isn't listed is refused.
func TestReaderMoveTargets(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(1)
	addMoveArea(env, "TECH", "local", "")
	addMoveArea(env, "LOCKED", "local", "s200")
	addMoveAreaACS(env, "NOPOST", "local", "", "s200")
	addMoveAreaACS(env, "NOREAD", "local", "s200", "")
	addMoveArea(env, "FSX", "echomail", "")
	sponsorSetSponsor(env, generalAreaID, "caller")

	r := env.runCmd("READMSGS", env.caller, "", "Vlocked\rQ")
	if !r.has("TECH Area", "That isn't one of the areas listed.") || r.has("LOCKED Area") || r.has("NOPOST Area") ||
		r.has("NOREAD Area") || r.has("FSX Area") || r.has("PRIVMAIL") {
		t.Errorf("output:\n%s", r.text())
	}
	if env.msgCount(generalAreaID) != 1 {
		t.Error("the message moved")
	}
}

// Nothing moves from private mail, on a blank answer or a No, or when there
// is nowhere to move it.
func TestReaderMoveRefusals(t *testing.T) {
	env := newMsgEnv(t)
	env.postMsgs(privmailAreaID, testMsg{from: "Sysop", to: "Sysop", subject: "note", private: true})
	addMoveArea(env, "TECH", "local", "")
	env.sysop.CurrentMessageAreaID, env.sysop.CurrentMessageAreaTag = privmailAreaID, "PRIVMAIL"
	if r := env.runCmd("READMSGS", env.sysop, "", "VtechQ"); r.has("Move this message to") {
		t.Errorf("move offered in PRIVMAIL; output:\n%s", r.text())
	}

	for _, in := range []string{"V\rQ", "Vtech\rNQ"} {
		env := newMsgEnv(t)
		env.generalMsgs(1)
		tech := addMoveArea(env, "TECH", "local", "")
		env.runCmd("READMSGS", env.sysop, "", in)
		if env.msgCount(generalAreaID) != 1 || env.msgCount(tech) != 0 {
			t.Errorf("%q: the message moved", in)
		}
	}

	env = newMsgEnv(t)
	env.generalMsgs(1)
	if r := env.runCmd("READMSGS", env.sysop, "", "VQ"); !r.has("There's no other area to move it to.") {
		t.Errorf("output:\n%s", r.text())
	}
}

// A sponsor the sysop removes while the move is being confirmed moves
// nothing, and is told why.
func TestReaderMoveSponsorRemoved(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(1)
	tech := addMoveArea(env, "TECH", "local", "")
	sponsorSetSponsor(env, generalAreaID, "caller")
	env.whenOutput("[Y/N]", func() { sponsorSetSponsor(env, generalAreaID, "") })

	r := env.runCmd("READMSGS", env.caller, "", "V1\rYQ")
	if !r.has("You can no longer move this message. Nothing was moved.") {
		t.Errorf("output:\n%s", r.text())
	}
	if env.msgCount(generalAreaID) != 1 || env.msgCount(tech) != 0 {
		t.Errorf("GENERAL %d, TECH %d, want 1 and 0", env.msgCount(generalAreaID), env.msgCount(tech))
	}
}

func TestLocalPublicArea(t *testing.T) {
	for _, tc := range []struct {
		area *message.MessageArea
		want bool
	}{
		{&message.MessageArea{Tag: "GENERAL", AreaType: "local"}, true},
		{&message.MessageArea{Tag: "GENERAL"}, true},
		{&message.MessageArea{Tag: "privmail", AreaType: "local"}, false},
		{&message.MessageArea{Tag: "FSX", AreaType: "echomail"}, false},
		{&message.MessageArea{Tag: "NET", AreaType: "netmail"}, false},
		{&message.MessageArea{Tag: "DOVE", AreaType: "qwknet"}, false},
		{&message.MessageArea{Tag: "FELONY", AreaType: "v3net"}, false},
		{nil, false},
	} {
		if got := localPublicArea(tc.area); got != tc.want {
			t.Errorf("localPublicArea(%+v) = %v, want %v", tc.area, got, tc.want)
		}
	}
}

func TestMovedLine(t *testing.T) {
	when := time.Date(2026, 11, 12, 14, 10, 0, 0, time.UTC)
	if got := withMovedLine("hello\n\n", "General", "Sysop", when); got != "hello\n\n * Moved from General by Sysop on 11/12/26 at 2:10 pm" {
		t.Errorf("withMovedLine = %q", got)
	}
	if got := areaPlainName(&message.MessageArea{Tag: "GEN", Name: "|12Hot |07Topics"}); got != "Hot Topics" {
		t.Errorf("areaPlainName = %q, want the colour codes taken out", got)
	}
	if got := areaPlainName(&message.MessageArea{Tag: "GEN", Name: "|12"}); got != "GEN" {
		t.Errorf("areaPlainName of a name that's only codes = %q, want the tag", got)
	}
}
