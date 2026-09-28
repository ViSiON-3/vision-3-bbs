package menu

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// addScanArea adds a local area readable at s10 to conference confID and
// posts n messages to it with subjects <tag>-1..<tag>-n.
func addScanArea(env *menuEnv, tag string, confID, n int) int {
	env.t.Helper()
	id, err := env.e.MessageMgr.AddArea(message.MessageArea{
		Tag: tag, Name: tag + " Area", AreaType: "local", ConferenceID: confID,
		ACSRead: "s10", ACSWrite: "s10",
	})
	if err != nil {
		env.t.Fatalf("AddArea %s: %v", tag, err)
	}
	for i := 1; i <= n; i++ {
		env.postMsgs(id, testMsg{from: "Sysop", to: "All", subject: fmt.Sprintf("%s-%d", tag, i)})
	}
	return id
}

// tagAreas saves tags as the caller's newscan areas.
func tagAreas(env *menuEnv, tags ...string) {
	env.t.Helper()
	env.caller.TaggedMessageAreaTags = tags
	if err := env.um.UpdateUser(env.caller); err != nil {
		env.t.Fatal(err)
	}
}

// scanShown lists which <tag>-1..<tag>-n message bodies the output showed.
func scanShown(r runResult, tag string, n int) []int {
	var shown []int
	for i := 1; i <= n; i++ {
		if r.has(fmt.Sprintf("%s-%d-body", tag, i)) {
			shown = append(shown, i)
		}
	}
	return shown
}

// withScanNoticePause turns the scan notices' pause off for the test.
func withScanNoticePause(t *testing.T) {
	t.Helper()
	prev := scanNoticePause
	scanNoticePause = 0
	t.Cleanup(func() { scanNoticePause = prev })
}

// TestNewscanTaggedAreasReadSkipQuit scans the tagged areas: the per-area
// bar's S skips an area (leaving its pointer), R reads the next, Q in the
// reader ends the whole scan, and pointers of areas read are saved.
func TestNewscanTaggedAreasReadSkipQuit(t *testing.T) {
	withScanNoticePause(t)
	env := newMsgEnv(t)
	env.generalMsgs(2)
	addScanArea(env, "ALPHA", 1, 2)
	betaID := addScanArea(env, "BETA", 1, 2)
	gammaID := addScanArea(env, "GAMMA", 1, 2)
	tagAreas(env, "GENERAL", "BETA", "GAMMA") // ALPHA untagged

	// Enter = scan tagged areas. GENERAL: S skip. BETA: R, read 1, Q.
	r := env.runCmd("NEWSCAN", env.caller, "", "\rSRQ")
	if r.err != nil {
		t.Fatalf("NEWSCAN: %v", r.err)
	}
	if got := readerShown(r, 2); len(got) != 0 {
		t.Errorf("skipped GENERAL but read %v", got)
	}
	if got := scanShown(r, "ALPHA", 2); len(got) != 0 {
		t.Errorf("untagged ALPHA was read: %v", got)
	}
	if got := scanShown(r, "BETA", 2); !slices.Equal(got, []int{1}) {
		t.Errorf("BETA shown %v, want [1]", got)
	}
	if got := scanShown(r, "GAMMA", 2); len(got) != 0 {
		t.Errorf("GAMMA read after Q ended the scan: %v", got)
	}
	if !r.has("Newscan complete!") {
		t.Errorf("scan end not reported; output:\n%s", r.text())
	}
	if lr := env.diskLastRead(generalAreaID, "Caller"); lr != 0 {
		t.Errorf("skipped GENERAL's lastread = %d, want 0", lr)
	}
	if lr := env.diskLastRead(betaID, "Caller"); lr != 1 {
		t.Errorf("BETA lastread = %d, want 1", lr)
	}
	if lr := env.diskLastRead(gammaID, "Caller"); lr != 0 {
		t.Errorf("GAMMA lastread = %d, want 0", lr)
	}
	saved := env.mustDiskUser(2)
	if saved.CurrentMessageAreaID != generalAreaID || saved.CurrentMessageAreaTag != "GENERAL" {
		t.Errorf("saved area after scan = %d/%q, want GENERAL", saved.CurrentMessageAreaID, saved.CurrentMessageAreaTag)
	}
}

// TestNewscanBarQuitAndNonStop checks the bar's Q stops before reading
// anything, and its N (NonStop) reads every tagged area without asking
// again.
func TestNewscanBarQuitAndNonStop(t *testing.T) {
	withScanNoticePause(t)
	env := newMsgEnv(t)
	env.generalMsgs(1)
	betaID := addScanArea(env, "BETA", 1, 1)
	tagAreas(env, "GENERAL", "BETA")

	r := env.runCmd("NEWSCAN", env.caller, "", "\rQ")
	if len(readerShown(r, 1)) != 0 || len(scanShown(r, "BETA", 1)) != 0 {
		t.Errorf("bar Q still read messages; output:\n%s", r.text())
	}
	if lr := env.diskLastRead(generalAreaID, "Caller"); lr != 0 {
		t.Errorf("GENERAL lastread = %d after quitting, want 0", lr)
	}

	// NonStop, then N past GENERAL's message, Q on BETA's.
	r = env.runCmd("NEWSCAN", env.caller, "", "\rNNQ")
	if !slices.Equal(readerShown(r, 1), []int{1}) || !slices.Equal(scanShown(r, "BETA", 1), []int{1}) {
		t.Errorf("NonStop did not read both areas; output:\n%s", r.text())
	}
	if lr := env.diskLastRead(betaID, "Caller"); lr != 1 {
		t.Errorf("BETA lastread = %d, want 1", lr)
	}
}

// TestNewscanBarJumpMarksEarlierRead checks the bar's J: reading starts at
// the typed message and everything before it is marked read.
func TestNewscanBarJumpMarksEarlierRead(t *testing.T) {
	withScanNoticePause(t)
	env := newMsgEnv(t)
	env.generalMsgs(4)
	tagAreas(env, "GENERAL")

	r := env.runCmd("NEWSCAN", env.caller, "", "\rJ3\rQ")
	if got := readerShown(r, 4); !slices.Equal(got, []int{3}) {
		t.Errorf("messages shown = %v, want [3]", got)
	}
	if lr := env.diskLastRead(generalAreaID, "Caller"); lr != 3 {
		t.Errorf("lastread = %d, want 3", lr)
	}
}

// TestNewscanBarPostThenNextArea checks the bar's P posts to the area being
// scanned and the scan moves on to the next area.
func TestNewscanBarPostThenNextArea(t *testing.T) {
	withScanNoticePause(t)
	env := newMsgEnv(t)
	env.generalMsgs(1)
	addScanArea(env, "BETA", 1, 1)
	tagAreas(env, "GENERAL", "BETA")

	r := env.runCmd("NEWSCAN", env.caller, "", "\rPscan post\r\rfrom the bar\x1aRQ")
	if n := env.msgCount(generalAreaID); n != 2 {
		t.Fatalf("GENERAL has %d messages, want 2", n)
	}
	if m := env.mustMsg(generalAreaID, 2); m.Subject != "scan post" || m.From != "Caller" {
		t.Errorf("posted %q from %q", m.Subject, m.From)
	}
	if !slices.Equal(scanShown(r, "BETA", 1), []int{1}) {
		t.Errorf("scan did not continue to BETA; output:\n%s", r.text())
	}
}

// TestNewscanUpdatePointersOffRestores checks a multi-area scan with Update
// Pointers off leaves every read area's pointer where it was.
func TestNewscanUpdatePointersOffRestores(t *testing.T) {
	withScanNoticePause(t)
	env := newMsgEnv(t)
	env.generalMsgs(2)
	betaID := addScanArea(env, "BETA", 1, 2)
	tagAreas(env, "GENERAL", "BETA")

	// NonStop: GENERAL 1, 2, past the end; BETA 1, 2, Q.
	r := env.runCmd("NEWSCAN", env.caller, "", "U\rNNNNQ")
	if !slices.Equal(readerShown(r, 2), []int{1, 2}) || !slices.Equal(scanShown(r, "BETA", 2), []int{1, 2}) {
		t.Fatalf("scan did not read both areas; output:\n%s", r.text())
	}
	for _, a := range []struct {
		name string
		id   int
	}{{"GENERAL", generalAreaID}, {"BETA", betaID}} {
		if lr := env.diskLastRead(a.id, "Caller"); lr != 0 {
			t.Errorf("%s lastread = %d with Update Pointers off, want 0", a.name, lr)
		}
	}
}

// TestNewscanConferenceScope scans all areas in the current conference:
// untagged areas in it are read, areas in other conferences are not.
func TestNewscanConferenceScope(t *testing.T) {
	withScanNoticePause(t)
	env := newMsgEnv(t)
	addScanArea(env, "ALPHA", 1, 1)
	addScanArea(env, "FAR", 2, 1)
	tagAreas(env) // nothing tagged

	// S then A: all areas in the conference. ALPHA: R, then Q.
	r := env.runCmd("NEWSCAN", env.caller, "", "SA\rRQ")
	if !slices.Equal(scanShown(r, "ALPHA", 1), []int{1}) {
		t.Errorf("ALPHA in the current conference not read; output:\n%s", r.text())
	}
	if len(scanShown(r, "FAR", 1)) != 0 {
		t.Errorf("FAR in another conference was read")
	}
}

// TestNewscanNothingTagged checks a tagged-areas scan with no tags says so,
// and a scan with nothing new still completes.
func TestNewscanNothingTagged(t *testing.T) {
	withScanNoticePause(t)
	env := newMsgEnv(t)
	tagAreas(env)

	if r := env.runCmd("NEWSCAN", env.caller, "", "\r"); !r.has("No message areas tagged for newscan.") {
		t.Errorf("no-tags scan output:\n%s", r.text())
	}
	tagAreas(env, "GENERAL")
	if r := env.runCmd("NEWSCAN", env.caller, "", "\r"); !r.has("Newscan complete!") {
		t.Errorf("empty scan output:\n%s", r.text())
	}
	// ESC at the setup menu aborts without scanning.
	if r := env.runCmd("NEWSCAN", env.caller, "", "\x1b"); r.has("Newscan complete!") {
		t.Errorf("aborted scan still ran; output:\n%s", r.text())
	}
}

// TestUpdateNewscanPointersMarkAllReadAndAllNew checks N marks every
// readable area read and A makes every message new again, both saved to
// the bases, across all conferences.
func TestUpdateNewscanPointersMarkAllReadAndAllNew(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(3)
	farID := addScanArea(env, "FAR", 2, 2)

	r := env.runCmd("UPDATENEWSCAN", env.caller, "", "N\rA")
	if !r.has("NewScan pointers updated for 3 area(s).") {
		t.Errorf("success notice missing; output:\n%s", r.text())
	}
	if lr := env.diskLastRead(generalAreaID, "Caller"); lr != 3 {
		t.Errorf("GENERAL lastread = %d, want 3", lr)
	}
	if lr := env.diskLastRead(farID, "Caller"); lr != 2 {
		t.Errorf("FAR lastread = %d, want 2", lr)
	}

	env.runCmd("UPDATENEWSCAN", env.caller, "", "all\ra")
	if lr := env.diskLastRead(generalAreaID, "Caller"); lr != 0 {
		t.Errorf("GENERAL lastread after all-new = %d, want 0", lr)
	}
	if lr := env.diskLastRead(farID, "Caller"); lr != 0 {
		t.Errorf("FAR lastread after all-new = %d, want 0", lr)
	}
}

// TestUpdateNewscanPointersCurrentConference checks the default scope (Enter)
// updates only the caller's current conference.
func TestUpdateNewscanPointersCurrentConference(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(2)
	farID := addScanArea(env, "FAR", 2, 2)

	r := env.runCmd("UPDATENEWSCAN", env.caller, "", "N\r\r")
	if !r.has("NewScan pointers updated for 2 area(s).") {
		t.Errorf("want GENERAL and PRIVMAIL updated; output:\n%s", r.text())
	}
	if lr := env.diskLastRead(generalAreaID, "Caller"); lr != 2 {
		t.Errorf("GENERAL lastread = %d, want 2", lr)
	}
	if lr := env.diskLastRead(farID, "Caller"); lr != 0 {
		t.Errorf("FAR (other conference) lastread = %d, want 0", lr)
	}
}

// TestUpdateNewscanPointersByDate sets the pointers to a date: messages
// from that day on are new and earlier ones read.
func TestUpdateNewscanPointersByDate(t *testing.T) {
	env := newMsgEnv(t)
	day := time.Now().AddDate(0, 0, -10)
	for i := 0; i < 4; i++ {
		if _, err := env.e.MessageMgr.AddMessageWithDate(generalAreaID, "Sysop", "All",
			fmt.Sprintf("d%d", i), "b", "", day.AddDate(0, 0, 2*i)); err != nil {
			t.Fatal(err)
		}
	}
	// Messages fall on day, +2, +4, +6; pick +3, so the first two are read.
	target := day.AddDate(0, 0, 3).Format("01/02/06")
	env.runCmd("UPDATENEWSCAN", env.caller, "", target+"\rC")
	if lr := env.diskLastRead(generalAreaID, "Caller"); lr != 2 {
		t.Errorf("lastread after date %s = %d, want 2", target, lr)
	}
}

// TestUpdateNewscanPointersCancelled checks a blank date, a bad date, ESC at
// the date and ESC at the scope all cancel without touching a pointer.
func TestUpdateNewscanPointersCancelled(t *testing.T) {
	env := newMsgEnv(t)
	env.generalMsgs(2)
	env.markRead(generalAreaID, "Caller", 1)

	for _, tc := range []struct{ name, input, extra string }{
		{"blank date", "\r", ""},
		{"bad date", "xyz\r", "Invalid date."},
		{"esc at date", "\x1b", ""},
		{"esc at scope", "N\r\x1b", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := env.sub(t).runCmd("UPDATENEWSCAN", env.caller, "", tc.input)
			if !r.has("NewScan pointer update cancelled.") || !r.has(tc.extra) {
				t.Errorf("output:\n%s", r.text())
			}
			if lr := env.diskLastRead(generalAreaID, "Caller"); lr != 1 {
				t.Errorf("lastread = %d, want 1 untouched", lr)
			}
		})
	}

	if r := env.runCmd("UPDATENEWSCAN", nil, "", "N\rA"); !r.has("You must be logged in to update newscan pointers.") {
		t.Errorf("anonymous caller output:\n%s", r.text())
	}
}
