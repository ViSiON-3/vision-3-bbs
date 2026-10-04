package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestListMessageAreasHidesUnreadableAreas pins LISTMSGAR's ACS filter: in the
// shipped Local conference a level-10 caller sees General Discussion (s10) but
// not Private Mail (s25), while the sysop sees both.
func TestListMessageAreasHidesUnreadableAreas(t *testing.T) {
	env := newMenuEnv(t)
	env.caller.CurrentMsgConferenceID = 1
	env.sysop.CurrentMsgConferenceID = 1

	r := env.runCmd("LISTMSGAR", env.caller, "", "\r")
	if r.err != nil || r.next != "" {
		t.Fatalf("caller LISTMSGAR = (%q, %v), want (\"\", nil)", r.next, r.err)
	}
	if !r.has("Local Areas", "General Discussion") {
		t.Errorf("caller list missing the readable area:\n%s", r.text())
	}
	if r.has("Private Mail") {
		t.Errorf("caller list shows Private Mail, which needs s25:\n%s", r.text())
	}

	r = env.runCmd("LISTMSGAR", env.sysop, "", "\r")
	if !r.has("General Discussion", "Private Mail") {
		t.Errorf("sysop list should show both shipped areas:\n%s", r.text())
	}
}

// TestListMessageAreasFiltersToCurrentConference pins that LISTMSGAR lists
// only the caller's current conference: FelonyNet holds no shipped areas, so
// a sysop joined there is told no areas are available.
func TestListMessageAreasFiltersToCurrentConference(t *testing.T) {
	env := newMenuEnv(t)
	env.sysop.CurrentMsgConferenceID = 2

	r := env.runCmd("LISTMSGAR", env.sysop, "", "\r")
	if r.has("General Discussion") || r.has("Private Mail") {
		t.Errorf("FelonyNet list shows Local areas:\n%s", r.text())
	}
	if !r.has("FelonyNet", "No accessible message areas found.") {
		t.Errorf("want FelonyNet header and the empty-list notice:\n%s", r.text())
	}
}

// TestListMessageAreasWaitsForEnter pins that LISTMSGAR ignores keys other
// than Enter at its pause, and reports a disconnect there as LOGOFF.
func TestListMessageAreasWaitsForEnter(t *testing.T) {
	env := newMenuEnv(t)
	env.sysop.CurrentMsgConferenceID = 1

	r := env.runCmd("LISTMSGAR", env.sysop, "", "xyz")
	if r.next != "LOGOFF" {
		t.Errorf("next = %q, want LOGOFF when input ends at the pause", r.next)
	}
	r = env.runCmd("LISTMSGAR", env.sysop, "", "xy\n")
	if r.next != "" || r.err != nil {
		t.Errorf("LF should end the pause: next=%q err=%v", r.next, r.err)
	}
}

// TestSelectMessageAreaLightbarQuitKeepsArea pins that quitting the
// SELECTMSGAREA picker leaves the caller's area alone, and that the picker
// only offers areas the caller can read.
func TestSelectMessageAreaLightbarQuitKeepsArea(t *testing.T) {
	env := newMenuEnv(t)
	env.caller.CurrentMsgConferenceID = 1

	r := env.runCmd("SELECTMSGAREA", env.caller, "", "\x1b[Bq")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if r.user != env.caller {
		t.Errorf("quit should hand back the caller")
	}
	if env.caller.CurrentMessageAreaID != 0 {
		t.Errorf("area = %d after quit, want unchanged 0", env.caller.CurrentMessageAreaID)
	}
	if !r.has("General Discussion") || r.has("Private Mail") {
		t.Errorf("picker should list only General Discussion for the caller:\n%s", r.text())
	}
}

// TestSelectMessageAreaLightbarConferenceSwitch pins the picker's side
// navigation: Right moves to FelonyNet (no areas, so Enter does nothing),
// Left returns to Local, and neither changes the saved area.
func TestSelectMessageAreaLightbarConferenceSwitch(t *testing.T) {
	env := newMenuEnv(t)
	env.sysop.CurrentMsgConferenceID = 1

	r := env.runCmd("SELECTMSGAREA", env.sysop, "", "\x1b[C\r\x1b[D\x1b")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("FelonyNet", "Local Areas") {
		t.Errorf("header should show both conferences as the filter moves:\n%s", r.text())
	}
	if env.sysop.CurrentMessageAreaID != 0 || env.sysop.CurrentMsgConferenceID != 1 {
		t.Errorf("side navigation changed the selection: area=%d conf=%d",
			env.sysop.CurrentMessageAreaID, env.sysop.CurrentMsgConferenceID)
	}
}

func TestChangeMessageConferenceCommandRendersLightbar(t *testing.T) {
	env := newMenuEnv(t)
	before := env.sysop.CurrentMsgConferenceID
	r := env.runCmd("CHANGEMSGCONF", env.sysop, "", "q")
	if r.err != nil {
		t.Fatalf("CHANGEMSGCONF: %v", r.err)
	}
	if !r.has("Local Areas", "FelonyNet", "Select", "Quit") {
		t.Fatalf("conference command did not render its picker:\n%s", r.text())
	}
	if env.sysop.CurrentMsgConferenceID != before {
		t.Fatalf("quitting the picker changed conference from %d to %d", before, env.sysop.CurrentMsgConferenceID)
	}
}

// TestChangeMessageConferenceCommandJoinsSelectedConference checks the
// shipped lightbar action's outcome: selecting FelonyNet updates the active
// conference for both message and file areas and persists the choice.
func TestChangeMessageConferenceCommandJoinsSelectedConference(t *testing.T) {
	env := newMenuEnv(t)
	env.sysop.CurrentMsgConferenceID = 1
	env.sysop.CurrentFileConferenceID = 1

	r := env.runCmd("CHANGEMSGCONF", env.sysop, "", "\x1b[B\r")
	if r.err != nil || r.user != env.sysop {
		t.Fatalf("result = (user %v, err %v), want the sysop and no error", r.user, r.err)
	}
	if !r.has("FelonyNet", "Conference Joined!") {
		t.Errorf("selected conference confirmation missing:\n%s", r.text())
	}
	saved := env.mustDiskUser(env.sysop.ID)
	if saved.CurrentMsgConferenceID != 2 || saved.CurrentFileConferenceID != 2 {
		t.Errorf("saved message/file conferences = %d/%d, want 2/2", saved.CurrentMsgConferenceID, saved.CurrentFileConferenceID)
	}
	if saved.CurrentMessageAreaID != 0 || saved.CurrentFileAreaID != 0 {
		t.Errorf("conference without areas retained selections %d/%d, want 0/0", saved.CurrentMessageAreaID, saved.CurrentFileAreaID)
	}
}

func TestChangeMessageConferenceCommandRollsBackIfSaveFails(t *testing.T) {
	env := newMenuEnv(t)
	usersPath := filepath.Join(env.dataDir(), "users.json")
	original, err := os.ReadFile(usersPath)
	if err != nil {
		t.Fatal(err)
	}
	restoreUsersFile := func() {
		if err := os.RemoveAll(usersPath); err != nil {
			t.Errorf("remove save-failure fixture: %v", err)
			return
		}
		if err := os.WriteFile(usersPath, original, 0o644); err != nil {
			t.Errorf("restore users.json: %v", err)
		}
	}
	t.Cleanup(restoreUsersFile)
	if err := os.Remove(usersPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(usersPath, 0o755); err != nil {
		t.Fatal(err)
	}

	before := snapshotConfSelection(env.sysop)
	r := env.runCmd("CHANGEMSGCONF", env.sysop, "", "\x1b[B\r")
	if r.err != nil || r.user != env.sysop {
		t.Fatalf("result = (user %v, err %v), want the sysop and no error", r.user, r.err)
	}
	if !r.has("Could not save the conference change.") {
		t.Errorf("save-failure message missing:\n%s", r.text())
	}
	if got := snapshotConfSelection(env.sysop); got != before {
		t.Errorf("session selection after save failure = %+v, want prior %+v", got, before)
	}
	if got, ok := env.um.GetUser("Sysop"); !ok || snapshotConfSelection(got) != before {
		t.Errorf("cached selection after save failure = %+v (found %v), want prior %+v", snapshotConfSelection(got), ok, before)
	}

	restoreUsersFile()
	if saved := env.mustDiskUser(env.sysop.ID); snapshotConfSelection(saved) != before {
		t.Errorf("disk selection after save failure = %+v, want prior %+v", snapshotConfSelection(saved), before)
	}
}

// TestSelectMessageAreaLightbarJoinsSelectedArea pins that Enter on a row
// joins that area and saves it: the sysop moves down to Private Mail and the
// choice survives a reload of users.json.
func TestSelectMessageAreaLightbarJoinsSelectedArea(t *testing.T) {
	env := newMenuEnv(t)
	env.sysop.CurrentMsgConferenceID = 1

	r := env.runCmd("SELECTMSGAREA", env.sysop, "", "\x1b[B\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if r.user == nil || r.user.CurrentMessageAreaTag != "PRIVMAIL" {
		t.Fatalf("returned user area = %+v, want PRIVMAIL", r.user)
	}
	if !r.has("Private Mail", "Area Joined!") {
		t.Errorf("missing join confirmation:\n%s", r.text())
	}
	saved := env.mustDiskUser(env.sysop.ID)
	if saved.CurrentMessageAreaID != 2 || saved.CurrentMsgConferenceID != 1 || saved.CurrentMsgConferenceTag != "LOCAL" {
		t.Errorf("saved area/conf = %d/%d/%q, want 2/1/LOCAL",
			saved.CurrentMessageAreaID, saved.CurrentMsgConferenceID, saved.CurrentMsgConferenceTag)
	}
}

// TestSelectMessageAreaClassicPromptNavigation drives the text-mode selector
// through its non-selecting commands: blank reprompts, ? relists, ] and [
// move the conference filter (wrapping), and Q quits without a change.
func TestSelectMessageAreaClassicPromptNavigation(t *testing.T) {
	env := newMenuEnv(t)
	env.sysop.CurrentMsgConferenceID = 1

	r := env.run(runSelectMessageArea, env.sysop, "", "\r?\r]\r]\r[\rq\r")
	if r.err != nil || r.user != env.sysop {
		t.Fatalf("result = (%v, %v), want the sysop and nil", r.user, r.err)
	}
	if env.sysop.CurrentMessageAreaID != 0 {
		t.Errorf("area changed to %d without a selection", env.sysop.CurrentMessageAreaID)
	}
	txt := r.text()
	// Initial list, ? relist, ] back to Local after wrapping, [ to FelonyNet.
	if n := strings.Count(txt, "General Discussion"); n < 3 {
		t.Errorf("General Discussion listed %d times, want at least 3 (initial, ?, wrap):\n%s", n, txt)
	}
	if !strings.Contains(txt, "Current Conf: FelonyNet") {
		t.Errorf("] should switch the list to FelonyNet:\n%s", txt)
	}
}

// TestSelectMessageAreaClassicRefusesThenSelects pins the text-mode
// selector's access check and its save: the caller naming PRIVMAIL by tag is
// refused, then picking list entry 1 joins General Discussion and persists.
func TestSelectMessageAreaClassicRefusesThenSelects(t *testing.T) {
	env := newMenuEnv(t)
	env.caller.CurrentMsgConferenceID = 1

	r := env.run(runSelectMessageArea, env.caller, "", "privmail\r1\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("Access denied to 'PRIVMAIL'.") {
		t.Errorf("PRIVMAIL should be refused for a level-10 caller:\n%s", r.text())
	}
	if !r.has("Current message area set to: General Discussion") {
		t.Errorf("missing selection confirmation:\n%s", r.text())
	}
	saved := env.mustDiskUser(env.caller.ID)
	if saved.CurrentMessageAreaTag != "GENERAL" || saved.CurrentMessageAreaID != 1 {
		t.Errorf("saved area = %d/%q, want 1/GENERAL", saved.CurrentMessageAreaID, saved.CurrentMessageAreaTag)
	}
}

// TestSelectMessageAreaClassicDisconnect pins that input ending at the
// text-mode prompt logs the caller off.
func TestSelectMessageAreaClassicDisconnect(t *testing.T) {
	env := newMenuEnv(t)
	env.sysop.CurrentMsgConferenceID = 1
	r := env.run(runSelectMessageArea, env.sysop, "", "")
	if r.next != "LOGOFF" {
		t.Errorf("next = %q, want LOGOFF", r.next)
	}
}

// TestNextPrevMsgAreaWrapsWithinConference pins NEXTMSGAREA/PREVMSGAREA: from
// no area the sysop lands on the first, steps to the second, wraps to the
// first, and PREV wraps back to the last; each step is saved.
func TestNextPrevMsgAreaWrapsWithinConference(t *testing.T) {
	env := newMenuEnv(t)
	env.sysop.CurrentMsgConferenceID = 1

	steps := []struct {
		cmd  string
		want string
	}{
		{"NEXTMSGAREA", "GENERAL"},
		{"NEXTMSGAREA", "PRIVMAIL"},
		{"NEXTMSGAREA", "GENERAL"},
		{"PREVMSGAREA", "PRIVMAIL"},
	}
	for i, st := range steps {
		r := env.runCmd(st.cmd, env.sysop, "", "")
		if r.err != nil {
			t.Fatalf("step %d %s: err = %v", i, st.cmd, r.err)
		}
		if env.sysop.CurrentMessageAreaTag != st.want {
			t.Fatalf("step %d %s: area = %q, want %q", i, st.cmd, env.sysop.CurrentMessageAreaTag, st.want)
		}
		if !r.has("Current area:", "("+st.want+")") {
			t.Errorf("step %d: missing current-area notice:\n%s", i, r.text())
		}
		if saved := env.mustDiskUser(env.sysop.ID); saved.CurrentMessageAreaTag != st.want {
			t.Errorf("step %d: saved area = %q, want %q", i, saved.CurrentMessageAreaTag, st.want)
		}
	}
}

// TestNextMsgAreaSkipsUnreadableAreas pins that NEXTMSGAREA only cycles
// through areas the caller can read: with Private Mail hidden, the caller
// stays on General Discussion.
func TestNextMsgAreaSkipsUnreadableAreas(t *testing.T) {
	env := newMenuEnv(t)
	env.caller.CurrentMsgConferenceID = 1
	env.caller.CurrentMessageAreaID = 1
	env.caller.CurrentMessageAreaTag = "GENERAL"

	for _, cmd := range []string{"NEXTMSGAREA", "PREVMSGAREA"} {
		env.runCmd(cmd, env.caller, "", "")
		if env.caller.CurrentMessageAreaTag != "GENERAL" {
			t.Errorf("%s moved the caller to %q, which it cannot read", cmd, env.caller.CurrentMessageAreaTag)
		}
	}
}

// TestNextPrevMsgConfJoinsBothMenus pins NEXTMSGCONF/PREVMSGCONF: moving to
// FelonyNet (no areas) clears both current areas, and moving back to Local
// joins its first message and file areas; the join is saved.
func TestNextPrevMsgConfJoinsBothMenus(t *testing.T) {
	env := newMenuEnv(t)
	u := env.sysop
	u.CurrentMsgConferenceID = 1
	u.CurrentMessageAreaID, u.CurrentMessageAreaTag = 2, "PRIVMAIL"
	u.CurrentFileConferenceID = 1
	u.CurrentFileAreaID, u.CurrentFileAreaTag = 2, "UPLOADS"

	r := env.runCmd("NEXTMSGCONF", u, "", "")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if u.CurrentMsgConferenceTag != "FELONYNET" || u.CurrentFileConferenceID != 2 {
		t.Fatalf("after NEXT: msg conf %q, file conf %d; want FELONYNET/2", u.CurrentMsgConferenceTag, u.CurrentFileConferenceID)
	}
	if u.CurrentMessageAreaID != 0 || u.CurrentFileAreaID != 0 {
		t.Errorf("FelonyNet has no areas; want both cleared, got msg %d file %d", u.CurrentMessageAreaID, u.CurrentFileAreaID)
	}
	if !r.has("FelonyNet", "FELONYNET") {
		t.Errorf("missing conference notice:\n%s", r.text())
	}

	env.runCmd("PREVMSGCONF", u, "", "")
	saved := env.mustDiskUser(u.ID)
	if saved.CurrentMsgConferenceID != 1 || saved.CurrentMessageAreaTag != "GENERAL" || saved.CurrentFileAreaTag != "GENERAL" {
		t.Errorf("saved after PREV = conf %d, msg %q, file %q; want 1/GENERAL/GENERAL",
			saved.CurrentMsgConferenceID, saved.CurrentMessageAreaTag, saved.CurrentFileAreaTag)
	}
}
