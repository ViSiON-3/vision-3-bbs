package menu

import (
	"os"
	"path/filepath"
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
	mm := &message.MessageManager{IsV3NetArea: func(id int) bool { return id == 9 }}
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
		{&message.MessageArea{ID: 9, Tag: "BOUND", AreaType: "local"}, false},
		{nil, false},
	} {
		if got := localPublicArea(mm, tc.area); got != tc.want {
			t.Errorf("localPublicArea(%+v) = %v, want %v", tc.area, got, tc.want)
		}
	}
}

// A local area a V3Net leaf binds by tag shares its messages with the
// network: nothing moves into it, and nothing out of it.
func TestReaderMoveNotV3NetBound(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(1)
	addMoveArea(env, "TECH", "local", "")
	bound := addMoveArea(env, "BOUND", "local", "")
	env.e.MessageMgr.IsV3NetArea = func(id int) bool { return id == bound }
	r := env.runCmd("READMSGS", env.sysop, "", "Vbound\rQ")
	if !r.has("TECH Area", "That isn't one of the areas listed.") || r.has("BOUND Area") {
		t.Errorf("into a bound area; output:\n%s", r.text())
	}
	if env.msgCount(generalAreaID) != 1 || env.msgCount(bound) != 0 {
		t.Error("the message moved")
	}

	env = newMsgEnv(t)
	env.generalMsgs(1)
	tech := addMoveArea(env, "TECH", "local", "")
	env.e.MessageMgr.IsV3NetArea = func(id int) bool { return id == generalAreaID }
	if r := env.runCmd("READMSGS", env.sysop, "", "?VtechQ"); r.has("Move to Another Area") || r.has("Move this message to") {
		t.Errorf("move offered out of a bound area; output:\n%s", r.text())
	}
	if env.msgCount(generalAreaID) != 1 || env.msgCount(tech) != 0 {
		t.Error("the message moved")
	}
}

// A message stored in another character set than UTF-8 isn't offered a
// move: its copy would lose the CHRS kludge that says how to read it.
func TestReaderMoveNotOtherCharset(t *testing.T) {
	env := newMsgEnv(t)
	tech := addMoveArea(env, "TECH", "local", "")
	if _, err := env.e.MessageMgr.Post(generalAreaID, "Caller", "All", "dos", "caf\x82", message.PostOptions{Kludges: []string{"CHRS: CP437 2"}}); err != nil {
		t.Fatal(err)
	}
	if r := env.runCmd("READMSGS", env.sysop, "", "?VtechQ"); r.has("Move to Another Area") || r.has("Move this message to") {
		t.Errorf("move offered on a CP437 message; output:\n%s", r.text())
	}
	if env.msgCount(generalAreaID) != 1 || env.msgCount(tech) != 0 {
		t.Error("the message moved")
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

// An area that stops being one the caller may move into while they confirm
// (here, a V3Net leaf binds it) takes nothing.
func TestReaderMoveTargetChangedMeanwhile(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(1)
	tech := addMoveArea(env, "TECH", "local", "")
	env.whenOutput("[Y/N]", func() {
		env.e.MessageMgr.IsV3NetArea = func(id int) bool { return id == tech }
	})
	r := env.runCmd("READMSGS", env.sysop, "", "Vtech\rYQ")
	if !r.has("That area can no longer take it. Nothing was moved.") {
		t.Errorf("output:\n%s", r.text())
	}
	if env.msgCount(generalAreaID) != 1 || env.msgCount(tech) != 0 {
		t.Errorf("GENERAL %d, TECH %d, want 1 and 0", env.msgCount(generalAreaID), env.msgCount(tech))
	}
}

// A sponsor the sysop removes while the copy is being written moves
// nothing: the right to move is asked again as the original is deleted.
func TestReaderMoveSponsorRemovedDuringCopy(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(1)
	tech := addMoveArea(env, "TECH", "local", "")
	sponsorSetSponsor(env, generalAreaID, "caller")
	env.e.MessageMgr.OnMessagePosted = func(a *message.MessageArea, _ int, _, _, _, _ string) {
		if a.ID == tech {
			sponsorSetSponsor(env, generalAreaID, "")
		}
	}
	env.runCmd("READMSGS", env.caller, "", "V1\rYQ")
	if env.msgCount(generalAreaID) != 1 || env.msgCount(tech) != 0 {
		t.Errorf("GENERAL %d, TECH %d, want 1 and 0", env.msgCount(generalAreaID), env.msgCount(tech))
	}
}

// When the message is deleted elsewhere and the area packed while the
// caller confirms, the reader shows what is in its place now, as after a
// refused delete.
func TestReaderMoveConflictReloads(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(2)
	addMoveArea(env, "TECH", "local", "")
	env.whenOutput("[Y/N]", func() {
		if err := env.e.MessageMgr.DeleteMessage(generalAreaID, 1); err != nil {
			t.Error(err)
		}
		if err := env.e.MessageMgr.PackAndLinkArea(generalAreaID); err != nil {
			t.Error(err)
		}
	})
	r := env.runCmd("READMSGS", env.sysop, "", "Vtech\rYQ")
	out := r.text()
	i := strings.Index(out, "Nothing was moved.")
	if i < 0 {
		t.Fatalf("output:\n%s", out)
	}
	if after := out[i:]; !strings.Contains(after, "subj-2") || strings.Contains(after, "subj-1") {
		t.Errorf("after the conflict, want subj-2 shown, not subj-1:\n%s", after)
	}
}

// A move that fails and can't take its copy back says a copy was left, and
// the reader shows what is in the message's place now.
func TestReaderMoveIncomplete(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(2)
	tech := addMoveArea(env, "TECH", "local", "")
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	env.e.MessageMgr.OnMessagePosted = func(a *message.MessageArea, _ int, _, _, _, _ string) {
		if a.ID != tech {
			return
		}
		// Elsewhere: the original is deleted and GENERAL packed, so the
		// move is refused, and TECH's base can't be opened to take the
		// copy back.
		if err := env.e.MessageMgr.DeleteMessage(generalAreaID, 1); err != nil {
			t.Error(err)
		}
		if err := env.e.MessageMgr.PackAndLinkArea(generalAreaID); err != nil {
			t.Error(err)
		}
		bad := *a
		bad.BasePath = filepath.Join(blocker, "tech")
		if err := env.e.MessageMgr.UpdateAreaByID(tech, bad); err != nil {
			t.Error(err)
		}
	}
	r := env.runCmd("READMSGS", env.sysop, "", "Vtech\rYQ")
	out := r.text()
	i := strings.Index(out, "a copy was left in")
	if i < 0 {
		t.Fatalf("output:\n%s", out)
	}
	if after := out[i:]; !strings.Contains(after, "subj-2") || strings.Contains(after, "subj-1") {
		t.Errorf("after the failed move, want subj-2 shown, not subj-1:\n%s", after)
	}
}
