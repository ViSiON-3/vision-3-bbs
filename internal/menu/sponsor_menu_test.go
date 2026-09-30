package menu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// sponsorHeaderMark is text only SPONSORM.ANS draws, so counting it counts
// how many times the sponsor menu header was displayed.
const sponsorHeaderMark = "SPONSOR MENU"

// sponsorNewEnv is a menuEnv with two areas added to the shipped GENERAL (1)
// and PRIVMAIL (2): THIRD (3) in the Local conference and FAR (4) in
// FelonyNet. The caller is the named sponsor of GENERAL and THIRD, and both
// users start in the Local conference on GENERAL.
func sponsorNewEnv(t *testing.T) *menuEnv {
	t.Helper()
	env := newMenuEnv(t)
	sponsorAddArea(env, message.MessageArea{Tag: "THIRD", Name: "Third Area", ConferenceID: 1, Sponsor: "Caller"})
	sponsorAddArea(env, message.MessageArea{Tag: "FAR", Name: "Far Away", ConferenceID: 2})
	sponsorSetSponsor(env, 1, "Caller")
	for _, u := range []*user.User{env.sysop, env.caller} {
		u.CurrentMsgConferenceID = 1
		u.CurrentMessageAreaID = 1
		u.CurrentMessageAreaTag = "GENERAL"
	}
	return env
}

// sponsorAddArea adds a local message area and returns its assigned ID.
func sponsorAddArea(env *menuEnv, a message.MessageArea) int {
	env.t.Helper()
	if a.AreaType == "" {
		a.AreaType = "local"
	}
	id, err := env.e.MessageMgr.AddArea(a)
	if err != nil {
		env.t.Fatalf("AddArea %s: %v", a.Tag, err)
	}
	return id
}

// sponsorSetSponsor makes handle the sponsor of area id and saves the areas.
func sponsorSetSponsor(env *menuEnv, id int, handle string) {
	env.t.Helper()
	a := sponsorArea(env, id)
	a.Sponsor = handle
	if err := env.e.MessageMgr.UpdateAreaByID(id, a); err != nil {
		env.t.Fatalf("UpdateAreaByID %d: %v", id, err)
	}
	if err := env.e.MessageMgr.SaveAreas(); err != nil {
		env.t.Fatalf("SaveAreas: %v", err)
	}
}

// sponsorArea returns a copy of area id as the live manager holds it.
func sponsorArea(env *menuEnv, id int) message.MessageArea {
	env.t.Helper()
	a, ok := env.e.MessageMgr.GetAreaByID(id)
	if !ok {
		env.t.Fatalf("area %d missing from the message manager", id)
	}
	return *a
}

// sponsorDiskAreas reads message_areas.json back, in file (position) order,
// so assertions see what a handler persisted rather than in-memory state.
func sponsorDiskAreas(env *menuEnv) []message.MessageArea {
	env.t.Helper()
	b, err := os.ReadFile(filepath.Join(env.cfgDir(), "message_areas.json"))
	if err != nil {
		env.t.Fatal(err)
	}
	var areas []message.MessageArea
	if err := json.Unmarshal(b, &areas); err != nil {
		env.t.Fatalf("parse message_areas.json: %v", err)
	}
	return areas
}

// sponsorDiskArea is the saved copy of area id.
func sponsorDiskArea(env *menuEnv, id int) message.MessageArea {
	env.t.Helper()
	for _, a := range sponsorDiskAreas(env) {
		if a.ID == id {
			return a
		}
	}
	env.t.Fatalf("area %d missing from message_areas.json", id)
	return message.MessageArea{}
}

// sponsorDiskOrder is the saved area tags of one conference in position order.
func sponsorDiskOrder(env *menuEnv, conferenceID int) string {
	env.t.Helper()
	var tags []string
	for _, a := range sponsorDiskAreas(env) {
		if a.ConferenceID == conferenceID {
			tags = append(tags, a.Tag)
		}
	}
	return strings.Join(tags, " ")
}

// sponsorReadOnlyDir takes write permission away from dir for the rest of the
// test, which is what makes an atomic (temp file + rename) save into it fail.
// It skips where a mode change cannot do that: on Windows, and as root.
func sponsorReadOnlyDir(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("cannot make a directory unwritable for this user")
	}
	// Restore write permission whatever happens, or t.TempDir cannot clean up.
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod %s: %v", dir, err)
	}
}

// TestSponsorMenuGuards pins who SPONSORMENU turns away before drawing
// anything: a caller who is not logged in, one with no (or an unknown)
// current area, and one who is neither co-sysop nor the area's sponsor.
func TestSponsorMenuGuards(t *testing.T) {
	env := sponsorNewEnv(t)

	r := env.sub(t).runCmd("SPONSORMENU", nil, "", "q\r")
	if r.user != nil || r.next != "" || r.err != nil || r.raw != "" {
		t.Errorf("no user: got (%v, %q, %v) output %q, want nothing at all", r.user, r.next, r.err, r.raw)
	}

	for _, id := range []int{0, 99} {
		env.caller.CurrentMessageAreaID = id
		r = env.runCmd("SPONSORMENU", env.caller, "", "q\r")
		if r.user != env.caller || !r.has("No message area selected.") || r.has(sponsorHeaderMark) {
			t.Errorf("area %d: want the no-area notice and no menu:\n%s", id, r.text())
		}
	}

	// PRIVMAIL has no sponsor, so the level-10 caller is refused silently.
	env.caller.CurrentMessageAreaID = 2
	r = env.runCmd("SPONSORMENU", env.caller, "", "q\r")
	if r.user != env.caller || r.next != "" || r.raw != "" {
		t.Errorf("non-sponsor: got next=%q output %q, want a silent refusal", r.next, r.raw)
	}

	// The same caller sponsors GENERAL and gets the menu there.
	env.caller.CurrentMessageAreaID = 1
	r = env.runCmd("SPONSORMENU", env.caller, "", "q\r")
	if !r.has(sponsorHeaderMark, "General Discussion") {
		t.Errorf("sponsor should see the menu for GENERAL:\n%s", r.text())
	}
}

// TestSponsorMenuCommandLoop pins the top-level keys: Q and a bare Enter
// leave, ? redraws the header, an unknown key is reported and redraws it, and
// running out of input is a LOGOFF.
func TestSponsorMenuCommandLoop(t *testing.T) {
	env := sponsorNewEnv(t)

	for _, quit := range []string{"q\r", "Q\r", "\r"} {
		r := env.runCmd("SPONSORMENU", env.sysop, "", quit)
		if r.user != env.sysop || r.next != "" || r.err != nil {
			t.Errorf("input %q: got (%v, %q, %v), want the sysop back", quit, r.user, r.next, r.err)
		}
		if n := strings.Count(r.text(), sponsorHeaderMark); n != 1 {
			t.Errorf("input %q: header drawn %d times, want 1", quit, n)
		}
	}

	r := env.runCmd("SPONSORMENU", env.sysop, "", "?\rq\r")
	if n := strings.Count(r.text(), sponsorHeaderMark); n != 2 || r.has("Invalid key.") {
		t.Errorf("? should redraw the header once more (drawn %d times):\n%s", n, r.text())
	}

	r = env.runCmd("SPONSORMENU", env.sysop, "", "x\rq\r")
	if n := strings.Count(r.text(), sponsorHeaderMark); n != 2 || !r.has("Invalid key.") {
		t.Errorf("unknown key should be reported and the header redrawn (drawn %d times):\n%s", n, r.text())
	}

	r = env.runCmd("SPONSORMENU", env.sysop, "", "")
	if r.user != nil || r.next != "LOGOFF" {
		t.Errorf("disconnect at the prompt: got (%v, %q), want (nil, LOGOFF)", r.user, r.next)
	}
}

// TestSponsorMenuFallbackPrompt pins the built-in prompt used when the menu
// set has no SPONSORM.MNU: it names the current area's tag and follows area
// navigation.
func TestSponsorMenuFallbackPrompt(t *testing.T) {
	env := sponsorNewEnv(t)
	env.e.MenuSetPath = t.TempDir()

	r := env.runCmd("SPONSORMENU", env.sysop, "", "]\rq\r")
	if r.err != nil || r.user != env.sysop {
		t.Fatalf("got (%v, %v), want the sysop and nil", r.user, r.err)
	}
	if !r.has("[GENERAL] Sponsor: E=Edit  P=Position  [/]=Prev/Next  Q=Quit: ", "[PRIVMAIL] Sponsor: ") {
		t.Errorf("want the fallback prompt for GENERAL, then PRIVMAIL:\n%s", r.text())
	}
}

// TestSponsorMenuAreaNavigation pins [ and ]: they walk the areas of the
// current conference the user may sponsor, in position order and wrapping,
// and save the new current area.
func TestSponsorMenuAreaNavigation(t *testing.T) {
	tests := []struct {
		name    string
		sysop   bool
		keys    string
		wantID  int
		wantTag string
	}{
		{"sysop next", true, "]\rq\r", 2, "PRIVMAIL"},
		{"sysop previous wraps to the last", true, "[\rq\r", 3, "THIRD"},
		{"sysop next three times wraps round", true, "]\r]\r]\rq\r", 1, "GENERAL"},
		// The caller does not sponsor PRIVMAIL, so it is stepped over.
		{"sponsor next skips unsponsored", false, "]\rq\r", 3, "THIRD"},
		{"sponsor next wraps", false, "]\r]\rq\r", 1, "GENERAL"},
		{"sponsor previous", false, "[\rq\r", 3, "THIRD"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := sponsorNewEnv(t)
			u := env.caller
			if tc.sysop {
				u = env.sysop
			}
			r := env.runCmd("SPONSORMENU", u, "", tc.keys)
			if r.err != nil || r.user != u {
				t.Fatalf("got (%v, %v), want the user and nil", r.user, r.err)
			}
			saved := env.mustDiskUser(u.ID)
			if saved.CurrentMessageAreaID != tc.wantID || saved.CurrentMessageAreaTag != tc.wantTag {
				t.Errorf("saved area = %d/%q, want %d/%q",
					saved.CurrentMessageAreaID, saved.CurrentMessageAreaTag, tc.wantID, tc.wantTag)
			}
		})
	}
}

// TestSponsorMenuNavigationEdgeCases pins the two cases where [ and ] have
// nowhere obvious to go: a sponsor of a single area stays put, and a user
// whose current area lies outside their current conference lands on the
// conference's first sponsorable area.
func TestSponsorMenuNavigationEdgeCases(t *testing.T) {
	env := sponsorNewEnv(t)
	sponsorSetSponsor(env, 3, "")

	r := env.sub(t).runCmd("SPONSORMENU", env.caller, "", "]\r[\rq\r")
	if env.caller.CurrentMessageAreaID != 1 {
		t.Errorf("sole sponsored area: moved to %d, want to stay on 1", env.caller.CurrentMessageAreaID)
	}
	if n := strings.Count(r.text(), sponsorHeaderMark); n != 1 {
		t.Errorf("header drawn %d times, want 1 (no navigation happened)", n)
	}

	// The sysop is on FAR (FelonyNet) while joined to the Local conference.
	env.sysop.CurrentMessageAreaID, env.sysop.CurrentMessageAreaTag = 4, "FAR"
	env.runCmd("SPONSORMENU", env.sysop, "", "]\rq\r")
	saved := env.mustDiskUser(env.sysop.ID)
	if saved.CurrentMessageAreaID != 1 || saved.CurrentMessageAreaTag != "GENERAL" {
		t.Errorf("saved area = %d/%q, want the conference's first area 1/GENERAL",
			saved.CurrentMessageAreaID, saved.CurrentMessageAreaTag)
	}
}

// TestSponsorMenuReposition pins the P sub-menu's moves. "Before N" counts in
// the list as displayed, E means the end, and each move is saved.
func TestSponsorMenuReposition(t *testing.T) {
	tests := []struct {
		name, keys, want string
	}{
		{"first to the end", "1\re\r", "PRIVMAIL THIRD GENERAL"},
		{"first before the third", "1\r3\r", "PRIVMAIL GENERAL THIRD"},
		{"last before the first", "3\r1\r", "THIRD GENERAL PRIVMAIL"},
		{"last before the second", "3\r2\r", "GENERAL THIRD PRIVMAIL"},
		{"two moves in a row", "1\re\r1\re\r", "THIRD GENERAL PRIVMAIL"},
		{"same position is a no-op", "2\r2\r", "GENERAL PRIVMAIL THIRD"},
		{"last to the end is a no-op", "3\re\r", "GENERAL PRIVMAIL THIRD"},
		{"blank destination cancels", "1\r\r", "GENERAL PRIVMAIL THIRD"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := sponsorNewEnv(t)
			r := env.runCmd("SPONSORMENU", env.sysop, "", "p\r"+tc.keys+"q\rq\r")
			if r.err != nil || r.user != env.sysop {
				t.Fatalf("got (%v, %v), want the sysop and nil", r.user, r.err)
			}
			if got := sponsorDiskOrder(env, 1); got != tc.want {
				t.Errorf("saved order = %q, want %q", got, tc.want)
			}
			if !r.has("-- Area Positions: Local Areas --", "Select area to move (1-3, Q=Quit): ") {
				t.Errorf("want the position list and its prompt:\n%s", r.text())
			}
			// Leaving the sub-menu returns to the sponsor menu.
			if n := strings.Count(r.text(), sponsorHeaderMark); n != 2 {
				t.Errorf("header drawn %d times, want 2 (entry and return)", n)
			}
			if r.has("Far Away") {
				t.Errorf("the list shows an area from another conference:\n%s", r.text())
			}
		})
	}
}

// TestSponsorMenuRepositionRejectsBadInput pins the sub-menu's validation:
// out-of-range and non-numeric entries are reported and nothing moves.
func TestSponsorMenuRepositionRejectsBadInput(t *testing.T) {
	env := sponsorNewEnv(t)

	r := env.runCmd("SPONSORMENU", env.sysop, "", "p\r9\rabc\r0\r1\r4\r1\rxyz\rq\rq\r")
	if n := strings.Count(r.text(), "Invalid selection."); n != 3 {
		t.Errorf("Invalid selection shown %d times, want 3 (9, abc, 0):\n%s", n, r.text())
	}
	if n := strings.Count(r.text(), "Invalid position."); n != 2 {
		t.Errorf("Invalid position shown %d times, want 2 (4, xyz):\n%s", n, r.text())
	}
	if !r.has("Place GENERAL before (1-3) or E=End: ") {
		t.Errorf("want the destination prompt naming the chosen area:\n%s", r.text())
	}
	if got := sponsorDiskOrder(env, 1); got != "GENERAL PRIVMAIL THIRD" {
		t.Errorf("order changed to %q by rejected input", got)
	}
}

// TestSponsorMenuRepositionNeedsTwoAreas pins that P refuses a conference
// with a single area, and that it labels conference 0 "Ungrouped".
func TestSponsorMenuRepositionNeedsTwoAreas(t *testing.T) {
	env := sponsorNewEnv(t)
	env.sysop.CurrentMsgConferenceID = 2
	env.sysop.CurrentMessageAreaID = 4

	r := env.runCmd("SPONSORMENU", env.sysop, "", "p\rq\r")
	if !r.has("Need at least 2 areas to reposition.") || r.has("Area Positions") {
		t.Errorf("want the too-few-areas notice and no list:\n%s", r.text())
	}

	loose1 := sponsorAddArea(env, message.MessageArea{Tag: "LOOSE1", Name: "Loose One"})
	sponsorAddArea(env, message.MessageArea{Tag: "LOOSE2", Name: "Loose Two"})
	env.sysop.CurrentMsgConferenceID = 0
	env.sysop.CurrentMessageAreaID = loose1

	r = env.runCmd("SPONSORMENU", env.sysop, "", "p\r2\r1\r\rq\r")
	if !r.has("-- Area Positions: Ungrouped --") {
		t.Errorf("want the Ungrouped heading:\n%s", r.text())
	}
	if got := sponsorDiskOrder(env, 0); got != "LOOSE2 LOOSE1" {
		t.Errorf("saved ungrouped order = %q, want \"LOOSE2 LOOSE1\"", got)
	}
}

// TestSponsorMenuRepositionPagesLongLists pins the pause that stops a list
// longer than the screen from scrolling away: with 21 areas on 24 rows the
// list holds for Enter once before its last lines.
func TestSponsorMenuRepositionPagesLongLists(t *testing.T) {
	env := sponsorNewEnv(t)
	for i := 0; i < 20; i++ {
		tag := "BULK" + string(rune('A'+i))
		sponsorAddArea(env, message.MessageArea{Tag: tag, Name: "Bulk " + tag, ConferenceID: 2})
	}
	env.sysop.CurrentMsgConferenceID = 2
	env.sysop.CurrentMessageAreaID = 4

	// Enter answers the pause; Q then leaves the sub-menu and the menu.
	r := env.runCmd("SPONSORMENU", env.sysop, "", "p\r\rq\rq\r")
	if r.err != nil || r.next != "" {
		t.Fatalf("got (%q, %v), want a clean return", r.next, r.err)
	}
	if n := strings.Count(r.text(), "SlAm eNtEr!"); n != 1 {
		t.Errorf("pause shown %d times, want 1:\n%s", n, r.text())
	}
	if !r.has("21) BULKT", "Select area to move (1-21, Q=Quit): ") {
		t.Errorf("want all 21 areas listed after the pause:\n%s", r.text())
	}
}

// TestSponsorMenuRepositionDisconnect pins that losing the caller at either
// reposition prompt logs them off without moving anything.
func TestSponsorMenuRepositionDisconnect(t *testing.T) {
	env := sponsorNewEnv(t)

	for _, keys := range []string{"p\r", "p\r1\r"} {
		r := env.runCmd("SPONSORMENU", env.sysop, "", keys)
		if r.user != nil || r.next != "LOGOFF" {
			t.Errorf("input %q: got (%v, %q), want (nil, LOGOFF)", keys, r.user, r.next)
		}
	}
	if got := sponsorDiskOrder(env, 1); got != "GENERAL PRIVMAIL THIRD" {
		t.Errorf("order changed to %q by an abandoned move", got)
	}
}

// TestSponsorMenuRepositionSaveFailure pins that a move which cannot be
// written is reported rather than passed off as done.
func TestSponsorMenuRepositionSaveFailure(t *testing.T) {
	env := sponsorNewEnv(t)
	sponsorReadOnlyDir(t, env.cfgDir())

	r := env.runCmd("SPONSORMENU", env.sysop, "", "p\r1\re\rq\rq\r")
	if !r.has("Error saving areas.") {
		t.Errorf("want the save error:\n%s", r.text())
	}
	if got := sponsorDiskOrder(env, 1); got != "GENERAL PRIVMAIL THIRD" {
		t.Errorf("order on disk = %q, want it untouched", got)
	}
}

// TestSponsorMenuEditCommand pins E: it runs the area editor, and on return
// the menu is redrawn showing the area as edited.
func TestSponsorMenuEditCommand(t *testing.T) {
	env := sponsorNewEnv(t)

	r := env.runCmd("SPONSORMENU", env.sysop, "", "e\rnRenamed Area\rqq\r")
	if r.err != nil || r.user != env.sysop {
		t.Fatalf("got (%v, %v), want the sysop and nil", r.user, r.err)
	}
	if got := sponsorDiskArea(env, 1).Name; got != "Renamed Area" {
		t.Errorf("saved name = %q, want Renamed Area", got)
	}
	if n := strings.Count(r.text(), sponsorHeaderMark); n != 2 {
		t.Errorf("header drawn %d times, want 2 (entry and after the edit)", n)
	}
	if !r.has("Edit Area: GENERAL (ID 1)", "Area GENERAL saved.", "> Renamed Area") {
		t.Errorf("want the editor, its save notice and the renamed area in the prompt:\n%s", r.text())
	}

	// A disconnect inside the editor logs the caller off from the menu too.
	r = env.runCmd("SPONSORMENU", env.sysop, "", "e\r")
	if r.user != nil || r.next != "LOGOFF" {
		t.Errorf("disconnect in the editor: got (%v, %q), want (nil, LOGOFF)", r.user, r.next)
	}
}

// TestSponsorEditAreaGuards pins who SPONSOREDITAREA turns away, mirroring
// the sponsor menu's own gate.
func TestSponsorEditAreaGuards(t *testing.T) {
	env := sponsorNewEnv(t)

	r := env.sub(t).runCmd("SPONSOREDITAREA", nil, "", "q")
	if r.user != nil || r.next != "" || r.raw != "" {
		t.Errorf("no user: got (%v, %q) output %q, want nothing at all", r.user, r.next, r.raw)
	}

	env.caller.CurrentMessageAreaID = 0
	r = env.runCmd("SPONSOREDITAREA", env.caller, "", "q")
	if r.user != env.caller || !r.has("No message area selected.") {
		t.Errorf("no area: want the no-area notice:\n%s", r.text())
	}

	env.caller.CurrentMessageAreaID = 99
	r = env.runCmd("SPONSOREDITAREA", env.caller, "", "q")
	if r.user != env.caller || !r.has("Area not found.") {
		t.Errorf("unknown area: want the not-found notice:\n%s", r.text())
	}

	env.caller.CurrentMessageAreaID = 2
	r = env.runCmd("SPONSOREDITAREA", env.caller, "", "q")
	if r.user != env.caller || r.raw != "" {
		t.Errorf("non-sponsor: output %q, want a silent refusal", r.raw)
	}
}

// TestSponsorEditAreaShowsFields pins the editor's field list, and that Q
// with nothing changed leaves without saving.
func TestSponsorEditAreaShowsFields(t *testing.T) {
	env := sponsorNewEnv(t)

	r := env.runCmd("SPONSOREDITAREA", env.sysop, "", "q")
	if r.err != nil || r.user != env.sysop || r.next != "" {
		t.Fatalf("got (%v, %q, %v), want the sysop back", r.user, r.next, r.err)
	}
	for _, want := range []string{
		"Edit Area: GENERAL (ID 1)",
		"T) Tag           : GENERAL",
		"N) Name          : General Discussion",
		"D) Description   : General discussion area",
		"R) ACS Read      : s10",
		"W) ACS Write     : s20",
		"S) Sponsor       : Caller",
		"M) Max Messages  : 0",
		"G) Max Age (days): 0",
		"A) Allow Anon    : no",
		"L) Real Name Only: false",
		"J) Auto Join     : true",
		"C) Conference ID : 1",
		"B) Base Path     : msgbases/loc_general",
		"Y) Area Type     : local",
		"[/]=Prev/Next  Q=Save/Quit  ESC=Cancel: ",
	} {
		if !r.has(want) {
			t.Errorf("field list missing %q:\n%s", want, r.text())
		}
	}
	if r.has("saved.") {
		t.Errorf("Q with no changes should not save:\n%s", r.text())
	}
}

// TestSponsorEditAreaSysopEditsEveryField drives each field key once as the
// sysop and pins that Q writes all of them, and that the user's cached area
// tag follows a tag change.
func TestSponsorEditAreaSysopEditsEveryField(t *testing.T) {
	env := sponsorNewEnv(t)

	keys := "tNEWTAG\r" + "nNew Name\r" + "dNew description\r" + "rs30\r" + "ws40\r" +
		"sSysop\r" + "m500\r" + "g90\r" + "ayes\r" + "lyes\r" + "jno\r" + "c2\r" +
		"bmsgbases/new_base\r" + "yechomail\r" + "eNEW_ECHO\r" + "o21:3/110\r" + "kfsxnet\r" + "q"
	r := env.runCmd("SPONSOREDITAREA", env.sysop, "", keys)
	if r.err != nil || r.user != env.sysop {
		t.Fatalf("got (%v, %v), want the sysop and nil", r.user, r.err)
	}
	if !r.has("Area NEWTAG saved.", "Edit Area: NEWTAG (ID 1)") {
		t.Errorf("want the save notice and the header following the new tag:\n%s", r.text())
	}

	yes := true
	want := message.MessageArea{
		ID: 1, Position: 1, Tag: "NEWTAG", Name: "New Name", Description: "New description",
		ACSRead: "s30", ACSWrite: "s40", AllowAnon: &yes, RealNameOnly: true, ConferenceID: 2,
		BasePath: "msgbases/new_base", MaxMessages: 500, MaxAge: 90, AutoJoin: false,
		AreaType: "echomail", EchoTag: "NEW_ECHO", OriginAddr: "21:3/110", Network: "fsxnet",
		Sponsor: "Sysop",
	}
	got := sponsorDiskArea(env, 1)
	if got.AllowAnon == nil || !*got.AllowAnon {
		t.Errorf("saved AllowAnon = %v, want true", got.AllowAnon)
	}
	got.AllowAnon, want.AllowAnon = nil, nil
	if got != want {
		t.Errorf("saved area:\n got %+v\nwant %+v", got, want)
	}
	if env.sysop.CurrentMessageAreaTag != "NEWTAG" {
		t.Errorf("user's area tag = %q, want it to follow the rename", env.sysop.CurrentMessageAreaTag)
	}
	if _, ok := env.e.MessageMgr.GetAreaByTag("NEWTAG"); !ok {
		t.Errorf("the live manager does not know the area by its new tag")
	}
}

// TestSponsorEditAreaSponsorRestrictions pins what a plain sponsor cannot do
// in the editor: the structural fields, the conference and the sponsor are
// sysop/co-sysop only, [ and ] are ignored, and the fields they may edit still
// save.
func TestSponsorEditAreaSponsorRestrictions(t *testing.T) {
	env := sponsorNewEnv(t)
	before := sponsorArea(env, 1)

	r := env.runCmd("SPONSOREDITAREA", env.caller, "", "tbyeokcs]q")
	if r.err != nil || r.user != env.caller {
		t.Fatalf("got (%v, %v), want the caller and nil", r.user, r.err)
	}
	for _, want := range []string{
		"Tag - sysop/co-sysop only.", "Base Path - sysop/co-sysop only.",
		"Area Type - sysop/co-sysop only.", "Echo Tag - sysop/co-sysop only.",
		"Origin Address - sysop/co-sysop only.", "Network - sysop/co-sysop only.",
		"Conference ID - sysop/co-sysop only.", "Sponsor - sysop/co-sysop only.",
	} {
		if !r.has(want) {
			t.Errorf("missing refusal %q:\n%s", want, r.text())
		}
	}
	if r.has("Prev/Next") || r.has("saved.") {
		t.Errorf("a sponsor should be offered no area navigation and nothing was changed:\n%s", r.text())
	}
	if env.caller.CurrentMessageAreaID != 1 {
		t.Errorf("] moved a plain sponsor to area %d", env.caller.CurrentMessageAreaID)
	}
	if got := sponsorArea(env, 1); got != before {
		t.Errorf("area changed by refused keys:\n got %+v\nwant %+v", got, before)
	}

	r = env.runCmd("SPONSOREDITAREA", env.caller, "", "DSponsor's words\rQ")
	if got := sponsorDiskArea(env, 1).Description; got != "Sponsor's words" || !r.has("Area GENERAL saved.") {
		t.Errorf("saved description = %q, want the sponsor's edit saved:\n%s", got, r.text())
	}
}

// TestSponsorEditAreaSponsorField pins the Sponsor field's validation: the
// handle must belong to a user, "-" clears it, and Enter keeps it.
func TestSponsorEditAreaSponsorField(t *testing.T) {
	tests := []struct {
		name, reply, want string
		rejected          bool
	}{
		{"known user", "Sysop", "Sysop", false},
		{"unknown user", "Nobody", "Caller", true},
		{"dash clears", "-", "", false},
		{"enter keeps", "", "Caller", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := sponsorNewEnv(t)
			r := env.runCmd("SPONSOREDITAREA", env.sysop, "", "s"+tc.reply+"\rq")
			if !r.has("Sponsor handle (- to clear) [Caller]: ") {
				t.Errorf("want the sponsor prompt showing the current handle:\n%s", r.text())
			}
			if got := r.has("User 'Nobody' not found - sponsor unchanged."); got != tc.rejected {
				t.Errorf("not-found notice shown = %v, want %v:\n%s", got, tc.rejected, r.text())
			}
			if got := sponsorDiskArea(env, 1).Sponsor; got != tc.want {
				t.Errorf("saved sponsor = %q, want %q", got, tc.want)
			}
			// Only a real change is saved.
			if got, changed := r.has("saved."), tc.want != "Caller"; got != changed {
				t.Errorf("save notice shown = %v, want %v", got, changed)
			}
		})
	}

	// With no user database to check against, the handle is taken as typed.
	env := sponsorNewEnv(t)
	sysop := env.sysop
	env.um = nil
	env.runCmd("SPONSOREDITAREA", sysop, "", "sNobody\rq")
	if got := sponsorDiskArea(env, 1).Sponsor; got != "Nobody" {
		t.Errorf("without a user manager: saved sponsor = %q, want Nobody", got)
	}
}

// TestSponsorEditAreaNumericFields pins the two numeric fields a sponsor may
// edit: a non-negative number is taken, anything else is reported and ignored.
// Conference ID has its own test, as it is co-sysop only and validated.
func TestSponsorEditAreaNumericFields(t *testing.T) {
	fields := []struct {
		key  string
		read func(message.MessageArea) int
	}{
		{"m", func(a message.MessageArea) int { return a.MaxMessages }},
		{"g", func(a message.MessageArea) int { return a.MaxAge }},
	}
	for _, f := range fields {
		t.Run(f.key, func(t *testing.T) {
			env := sponsorNewEnv(t)
			start := f.read(sponsorArea(env, 1))

			for _, bad := range []string{"abc", "-5"} {
				r := env.runCmd("SPONSOREDITAREA", env.caller, "", f.key+bad+"\rq")
				if !r.has("Invalid number - unchanged.") || r.has("saved.") {
					t.Errorf("reply %q: want it rejected and nothing saved:\n%s", bad, r.text())
				}
				if got := f.read(sponsorDiskArea(env, 1)); got != start {
					t.Errorf("reply %q: saved value = %d, want %d", bad, got, start)
				}
			}

			r := env.runCmd("SPONSOREDITAREA", env.caller, "", f.key+"7\rq")
			if got := f.read(sponsorDiskArea(env, 1)); got != 7 || !r.has("saved.") {
				t.Errorf("saved value = %d, want 7:\n%s", got, r.text())
			}
			// Enter alone keeps the value, so there is nothing to save.
			r = env.runCmd("SPONSOREDITAREA", env.caller, "", f.key+"\rq")
			if got := f.read(sponsorArea(env, 1)); got != 7 || r.has("saved.") || r.has("Invalid number") {
				t.Errorf("after a bare Enter: value = %d, want 7 kept quietly:\n%s", got, r.text())
			}
		})
	}
}

// TestSponsorEditAreaAllowAnon pins the three-state Allow Anonymous field.
// GENERAL ships with it set to no.
func TestSponsorEditAreaAllowAnon(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		reply string
		want  *bool
		shown string
	}{
		{"yes", &yes, "yes"},
		{"Y", &yes, "yes"},
		{"1", &yes, "yes"},
		{"no", &no, "no"},
		{"default", nil, "default"},
		{"d", nil, "default"},
		{"maybe", &no, "no"},
		{"", &no, "no"},
	}
	for _, tc := range tests {
		t.Run("reply "+tc.reply, func(t *testing.T) {
			env := sponsorNewEnv(t)
			r := env.runCmd("SPONSOREDITAREA", env.caller, "", "a"+tc.reply+"\rq")
			if !r.has("Allow Anonymous (yes/no/default) [no]: ") {
				t.Errorf("want the prompt showing the current setting:\n%s", r.text())
			}
			if got := sponsorDiskArea(env, 1).AllowAnon; !allowAnonEqual(got, tc.want) {
				t.Errorf("saved AllowAnon = %v, want %v", sponsorAnon(got), sponsorAnon(tc.want))
			}
			if got := r.has("Enter yes, no, or default."); got != (tc.reply == "maybe") {
				t.Errorf("bad-value notice shown = %v:\n%s", got, r.text())
			}
			// The redrawn row is the last thing written to the field list.
			if !strings.Contains(r.text(), "A) Allow Anon    : "+tc.shown+"Edit (") {
				t.Errorf("row not redrawn as %q:\n%s", tc.shown, r.text())
			}
		})
	}

	// From "default", the prompt says so and "no" is a change worth saving.
	env := sponsorNewEnv(t)
	env.runCmd("SPONSOREDITAREA", env.caller, "", "adefault\rq")
	r := env.runCmd("SPONSOREDITAREA", env.caller, "", "ano\rq")
	if !r.has("A) Allow Anon    : default", "Allow Anonymous (yes/no/default) [default]: ", "saved.") {
		t.Errorf("want the default state shown and the change to no saved:\n%s", r.text())
	}
	if got := sponsorDiskArea(env, 1).AllowAnon; got == nil || *got {
		t.Errorf("saved AllowAnon = %v, want false", sponsorAnon(got))
	}
	r = env.runCmd("SPONSOREDITAREA", env.caller, "", "ayes\ra\rq")
	if !r.has("Allow Anonymous (yes/no/default) [yes]: ") {
		t.Errorf("want the second prompt to show yes:\n%s", r.text())
	}
}

// sponsorAnon renders an AllowAnon value for failure messages.
func sponsorAnon(b *bool) string {
	if b == nil {
		return "default"
	}
	if *b {
		return "yes"
	}
	return "no"
}

// TestSponsorAllowAnonEqual pins the tri-state comparison the editor uses to
// decide whether Allow Anonymous changed.
func TestSponsorAllowAnonEqual(t *testing.T) {
	yes, yes2, no := true, true, false
	tests := []struct {
		a, b *bool
		want bool
	}{
		{nil, nil, true},
		{&yes, &yes2, true},
		{&yes, &no, false},
		{nil, &no, false},
		{&yes, nil, false},
	}
	for _, tc := range tests {
		if got := allowAnonEqual(tc.a, tc.b); got != tc.want {
			t.Errorf("allowAnonEqual(%s, %s) = %v, want %v", sponsorAnon(tc.a), sponsorAnon(tc.b), got, tc.want)
		}
	}
}

// TestSponsorEditAreaYesNoFields pins the two boolean fields. GENERAL ships
// with Real Name Only off and Auto Join on.
func TestSponsorEditAreaYesNoFields(t *testing.T) {
	env := sponsorNewEnv(t)

	r := env.sub(t).runCmd("SPONSOREDITAREA", env.caller, "", "lyes\rjn\rq")
	if !r.has("Real Name Only (yes/no) [no]: ", "Auto Join (yes/no) [yes]: ", "saved.") {
		t.Errorf("want both prompts with their current values and a save:\n%s", r.text())
	}
	if a := sponsorDiskArea(env, 1); !a.RealNameOnly || a.AutoJoin {
		t.Errorf("saved RealNameOnly=%v AutoJoin=%v, want true and false", a.RealNameOnly, a.AutoJoin)
	}

	// The prompts now show the flipped values; "true" and "1" also mean yes,
	// anything else means no, and Enter keeps the setting.
	r = env.sub(t).runCmd("SPONSOREDITAREA", env.caller, "", "l\rj1\rq")
	if !r.has("Real Name Only (yes/no) [yes]: ", "Auto Join (yes/no) [no]: ") {
		t.Errorf("want the prompts to show the saved values:\n%s", r.text())
	}
	if a := sponsorDiskArea(env, 1); !a.RealNameOnly || !a.AutoJoin {
		t.Errorf("saved RealNameOnly=%v AutoJoin=%v, want both true", a.RealNameOnly, a.AutoJoin)
	}
	env.sub(t).runCmd("SPONSOREDITAREA", env.caller, "", "lx\rjtrue\rq")
	if a := sponsorDiskArea(env, 1); a.RealNameOnly || !a.AutoJoin {
		t.Errorf("saved RealNameOnly=%v AutoJoin=%v, want false and true", a.RealNameOnly, a.AutoJoin)
	}
}

// TestSponsorEditAreaDiscard pins ESC: edits made in the session are dropped,
// in memory and on disk.
func TestSponsorEditAreaDiscard(t *testing.T) {
	env := sponsorNewEnv(t)

	r := env.runCmd("SPONSOREDITAREA", env.sysop, "", "nThrown Away\r\x1b")
	if r.err != nil || r.user != env.sysop || r.next != "" {
		t.Fatalf("got (%v, %q, %v), want the sysop back", r.user, r.next, r.err)
	}
	if !r.has("N) Name          : Thrown Away", "Changes discarded.") || r.has("saved.") {
		t.Errorf("want the edit shown, then discarded:\n%s", r.text())
	}
	if got := sponsorArea(env, 1).Name; got != "General Discussion" {
		t.Errorf("live name = %q, want it untouched", got)
	}
	if got := sponsorDiskArea(env, 1).Name; got != "General Discussion" {
		t.Errorf("saved name = %q, want it untouched", got)
	}
}

// TestSponsorEditAreaDisconnect pins that running out of input at the editor
// prompt is a LOGOFF and saves nothing, even with edits pending.
func TestSponsorEditAreaDisconnect(t *testing.T) {
	env := sponsorNewEnv(t)

	r := env.runCmd("SPONSOREDITAREA", env.sysop, "", "nHalf Done\r")
	if r.user != nil || r.next != "LOGOFF" {
		t.Errorf("got (%v, %q), want (nil, LOGOFF)", r.user, r.next)
	}
	if got := sponsorDiskArea(env, 1).Name; got != "General Discussion" {
		t.Errorf("saved name = %q, want the unsaved edit dropped", got)
	}
}

// TestSponsorEditAreaFieldLengthCap pins that a reply longer than the field
// allows is cut to the limit, counted in runes rather than bytes.
func TestSponsorEditAreaFieldLengthCap(t *testing.T) {
	env := sponsorNewEnv(t)

	long := strings.Repeat("é", 70)
	env.runCmd("SPONSOREDITAREA", env.sysop, "", "n"+long+"\rq")
	if got, want := sponsorDiskArea(env, 1).Name, strings.Repeat("é", 60); got != want {
		t.Errorf("saved name is %d runes, want it cut to 60", len([]rune(got)))
	}
}

// TestSponsorEditAreaUpdateRejected pins the save path when the manager
// refuses the edit (here a tag another area already uses): the error is
// shown and the area is left as it was.
func TestSponsorEditAreaUpdateRejected(t *testing.T) {
	env := sponsorNewEnv(t)

	r := env.runCmd("SPONSOREDITAREA", env.sysop, "", "tPRIVMAIL\rq")
	if r.err != nil || r.user != env.sysop {
		t.Fatalf("got (%v, %v), want the sysop and nil", r.user, r.err)
	}
	if !r.has("Error updating area - changes may be lost.") || r.has("saved.") {
		t.Errorf("want the update error and no save notice:\n%s", r.text())
	}
	if got := sponsorDiskArea(env, 1).Tag; got != "GENERAL" {
		t.Errorf("saved tag = %q, want GENERAL", got)
	}
	if a, _ := env.e.MessageMgr.GetAreaByTag("PRIVMAIL"); a == nil || a.ID != 2 {
		t.Errorf("PRIVMAIL no longer resolves to area 2: %+v", a)
	}
	if env.sysop.CurrentMessageAreaTag != "GENERAL" {
		t.Errorf("user's area tag = %q, want GENERAL", env.sysop.CurrentMessageAreaTag)
	}
}

// TestSponsorEditAreaSaveFailureRollsBack pins that when message_areas.json
// cannot be written the edit is reported lost and taken back out of the live
// manager, so memory and disk do not disagree.
func TestSponsorEditAreaSaveFailureRollsBack(t *testing.T) {
	env := sponsorNewEnv(t)
	sponsorReadOnlyDir(t, env.cfgDir())

	r := env.runCmd("SPONSOREDITAREA", env.sysop, "", "nNever Saved\rtLOSTTAG\rq")
	if !r.has("Error saving area - changes may be lost.") || r.has("saved.") {
		t.Errorf("want the save error and no save notice:\n%s", r.text())
	}
	if a := sponsorArea(env, 1); a.Name != "General Discussion" || a.Tag != "GENERAL" {
		t.Errorf("live area = %q/%q, want it rolled back to GENERAL/General Discussion", a.Tag, a.Name)
	}
	if _, ok := env.e.MessageMgr.GetAreaByTag("LOSTTAG"); ok {
		t.Errorf("the rolled-back tag is still indexed")
	}
	if env.sysop.CurrentMessageAreaTag != "GENERAL" {
		t.Errorf("user's area tag = %q, want GENERAL", env.sysop.CurrentMessageAreaTag)
	}
	if got := sponsorDiskArea(env, 1).Name; got != "General Discussion" {
		t.Errorf("saved name = %q, want it untouched", got)
	}
}

// TestSponsorEditAreaNavigation pins [ and ] for a sysop with nothing
// pending: they step through every area of the conference in position order,
// wrapping, redraw the editor for the new area and save it as current.
func TestSponsorEditAreaNavigation(t *testing.T) {
	tests := []struct {
		keys    string
		wantID  int
		wantTag string
	}{
		{"]q", 2, "PRIVMAIL"},
		{"[q", 3, "THIRD"},
		{"]]]q", 1, "GENERAL"},
		{"][[q", 3, "THIRD"},
	}
	for _, tc := range tests {
		t.Run(tc.keys, func(t *testing.T) {
			env := sponsorNewEnv(t)
			r := env.runCmd("SPONSOREDITAREA", env.sysop, "", tc.keys)
			if r.err != nil || r.user != env.sysop {
				t.Fatalf("got (%v, %v), want the sysop and nil", r.user, r.err)
			}
			saved := env.mustDiskUser(env.sysop.ID)
			if saved.CurrentMessageAreaID != tc.wantID || saved.CurrentMessageAreaTag != tc.wantTag {
				t.Errorf("saved area = %d/%q, want %d/%q",
					saved.CurrentMessageAreaID, saved.CurrentMessageAreaTag, tc.wantID, tc.wantTag)
			}
			if !strings.Contains(r.text()[strings.LastIndex(r.text(), "Edit Area: "):], "T) Tag           : "+tc.wantTag) {
				t.Errorf("the last field list drawn is not %s's:\n%s", tc.wantTag, r.text())
			}
		})
	}
}

// TestSponsorEditAreaNavigationNowhereToGo pins that [ and ] do nothing when
// the conference has a single area, or when the area being edited is not in
// the user's current conference at all.
func TestSponsorEditAreaNavigationNowhereToGo(t *testing.T) {
	env := sponsorNewEnv(t)

	env.sysop.CurrentMsgConferenceID = 2
	env.sysop.CurrentMessageAreaID, env.sysop.CurrentMessageAreaTag = 4, "FAR"
	r := env.sub(t).runCmd("SPONSOREDITAREA", env.sysop, "", "][q")
	if env.sysop.CurrentMessageAreaID != 4 || strings.Count(r.text(), "Edit Area: ") != 1 {
		t.Errorf("single-area conference: moved to %d or redrew the editor:\n%s",
			env.sysop.CurrentMessageAreaID, r.text())
	}

	// Editing FAR while joined to the Local conference, which has three areas.
	env.sysop.CurrentMsgConferenceID = 1
	r = env.sub(t).runCmd("SPONSOREDITAREA", env.sysop, "", "]q")
	if env.sysop.CurrentMessageAreaID != 4 || strings.Count(r.text(), "Edit Area: ") != 1 {
		t.Errorf("area outside the conference: moved to %d or redrew the editor:\n%s",
			env.sysop.CurrentMessageAreaID, r.text())
	}
}

// TestSponsorEditAreaNavigationWithPendingEdits pins the save-first question
// asked when [ or ] is pressed with unsaved changes.
func TestSponsorEditAreaNavigationWithPendingEdits(t *testing.T) {
	const ask = "Save changes before switching? (Y/N/ESC=Cancel): "

	t.Run("yes saves and moves on", func(t *testing.T) {
		env := sponsorNewEnv(t)
		r := env.runCmd("SPONSOREDITAREA", env.sysop, "", "tRETAGGED\r]yq")
		if !r.has(ask, "Area RETAGGED saved.", "Edit Area: PRIVMAIL (ID 2)") {
			t.Errorf("want the question, the save and the next area:\n%s", r.text())
		}
		if got := sponsorDiskArea(env, 1).Tag; got != "RETAGGED" {
			t.Errorf("saved tag = %q, want RETAGGED", got)
		}
		if saved := env.mustDiskUser(env.sysop.ID); saved.CurrentMessageAreaID != 2 {
			t.Errorf("saved current area = %d, want 2", saved.CurrentMessageAreaID)
		}
		// The second Q had nothing left to save.
		if n := strings.Count(r.text(), "saved."); n != 1 {
			t.Errorf("save notice shown %d times, want 1", n)
		}
	})

	t.Run("no discards and moves on", func(t *testing.T) {
		env := sponsorNewEnv(t)
		r := env.runCmd("SPONSOREDITAREA", env.sysop, "", "nDropped\r]Nq")
		if !r.has(ask, "Edit Area: PRIVMAIL (ID 2)") || r.has("saved.") {
			t.Errorf("want the question and the next area, with no save:\n%s", r.text())
		}
		if got := sponsorDiskArea(env, 1).Name; got != "General Discussion" {
			t.Errorf("saved name = %q, want the edit discarded", got)
		}
		if env.sysop.CurrentMessageAreaID != 2 {
			t.Errorf("current area = %d, want 2", env.sysop.CurrentMessageAreaID)
		}
	})

	t.Run("any other key stays with the edits", func(t *testing.T) {
		env := sponsorNewEnv(t)
		r := env.runCmd("SPONSOREDITAREA", env.sysop, "", "nKept\r]xq")
		if env.sysop.CurrentMessageAreaID != 1 || r.has("Edit Area: PRIVMAIL") {
			t.Errorf("cancel should stay on GENERAL:\n%s", r.text())
		}
		// The edit is still pending, so the final Q saves it.
		if got := sponsorDiskArea(env, 1).Name; got != "Kept" {
			t.Errorf("saved name = %q, want Kept", got)
		}
	})

	t.Run("a rejected save stays put", func(t *testing.T) {
		env := sponsorNewEnv(t)
		r := env.runCmd("SPONSOREDITAREA", env.sysop, "", "tPRIVMAIL\r]y\x1b")
		if !r.has("Error updating area.") || r.has("Edit Area: PRIVMAIL (ID 2)") {
			t.Errorf("want the update error and no move:\n%s", r.text())
		}
		if env.sysop.CurrentMessageAreaID != 1 {
			t.Errorf("current area = %d, want 1", env.sysop.CurrentMessageAreaID)
		}
	})

	t.Run("a failed write stays put", func(t *testing.T) {
		env := sponsorNewEnv(t)
		sponsorReadOnlyDir(t, env.cfgDir())
		r := env.runCmd("SPONSOREDITAREA", env.sysop, "", "nUnwritable\r]y\x1b")
		if !r.has("Error saving area.") || r.has("Edit Area: PRIVMAIL (ID 2)") {
			t.Errorf("want the save error and no move:\n%s", r.text())
		}
		if got := sponsorDiskArea(env, 1).Name; got != "General Discussion" {
			t.Errorf("saved name = %q, want it untouched", got)
		}
	})

	t.Run("disconnect at the question", func(t *testing.T) {
		env := sponsorNewEnv(t)
		r := env.runCmd("SPONSOREDITAREA", env.sysop, "", "nGone\r]")
		if r.user != nil || r.next != "LOGOFF" {
			t.Errorf("got (%v, %q), want (nil, LOGOFF)", r.user, r.next)
		}
		if got := sponsorDiskArea(env, 1).Name; got != "General Discussion" {
			t.Errorf("saved name = %q, want the edit dropped", got)
		}
	})
}

// TestSponsorMenuRepositionAsSponsor pins P for a plain sponsor, whose list
// leaves out PRIVMAIL (which sits between GENERAL and THIRD). The position
// picked in that shorter list must be resolved against the whole conference,
// and the area the sponsor cannot see must keep its place relative to others.
func TestSponsorMenuRepositionAsSponsor(t *testing.T) {
	tests := []struct {
		name, keys, want string
	}{
		{"first to the end", "1\re\r", "PRIVMAIL THIRD GENERAL"},
		{"first before the second is a no-op", "1\r2\r", "GENERAL PRIVMAIL THIRD"},
		{"last before the first", "2\r1\r", "THIRD GENERAL PRIVMAIL"},
		{"last to the end is a no-op", "2\re\r", "GENERAL PRIVMAIL THIRD"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := sponsorNewEnv(t)
			r := env.runCmd("SPONSORMENU", env.caller, "", "p\r"+tc.keys+"q\rq\r")
			if r.err != nil || r.user != env.caller {
				t.Fatalf("got (%v, %v), want the caller and nil", r.user, r.err)
			}
			if !r.has("Select area to move (1-2, Q=Quit): ") || r.has("Private Mail") {
				t.Errorf("want only the two sponsored areas listed:\n%s", r.text())
			}
			if got := sponsorDiskOrder(env, 1); got != tc.want {
				t.Errorf("saved order = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSponsorMoveTarget pins the translation from a position in a filtered
// list to the conference-wide index MoveAreaPositionInConference takes.
func TestSponsorMoveTarget(t *testing.T) {
	a := func(id int) *message.MessageArea { return &message.MessageArea{ID: id} }
	// Conference order 1..5; the user sees 1, 3 and 5.
	all := []*message.MessageArea{a(1), a(2), a(3), a(4), a(5)}
	listed := []*message.MessageArea{all[0], all[2], all[4]}
	tests := []struct {
		name      string
		moving    *message.MessageArea
		beforeIdx int
		want      int
	}{
		{"1 before 5", all[0], 2, 4},
		{"5 before 1", all[4], 0, 1},
		{"5 before 3", all[4], 1, 3},
		{"1 to the end", all[0], -1, 5},
		{"3 to the end", all[2], -1, 5},
		{"5 to the end lands straight after 3", all[4], -1, 4},
		{"out of range falls back to the end", all[0], 9, 5},
	}
	for _, tc := range tests {
		if got := sponsorMoveTarget(all, listed, tc.moving, tc.beforeIdx); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestSponsorEditAreaRenamePersistsUser pins that renaming the tag of the
// user's current area saves the new tag to the user record, both on Q and on
// the save-before-switching path.
func TestSponsorEditAreaRenamePersistsUser(t *testing.T) {
	env := sponsorNewEnv(t)
	env.sub(t).runCmd("SPONSOREDITAREA", env.sysop, "", "tRENAMED\rq")
	if saved := env.mustDiskUser(env.sysop.ID); saved.CurrentMessageAreaTag != "RENAMED" {
		t.Errorf("Q: saved area tag = %q, want RENAMED", saved.CurrentMessageAreaTag)
	}

	// FAR is alone in its conference, so ] saves and then has nowhere to go.
	env.sysop.CurrentMsgConferenceID = 2
	env.sysop.CurrentMessageAreaID, env.sysop.CurrentMessageAreaTag = 4, "FAR"
	r := env.sub(t).runCmd("SPONSOREDITAREA", env.sysop, "", "tFARTHER\r]y\x1b")
	if !r.has("Area FARTHER saved.") {
		t.Fatalf("want the save notice:\n%s", r.text())
	}
	if saved := env.mustDiskUser(env.sysop.ID); saved.CurrentMessageAreaTag != "FARTHER" {
		t.Errorf("save-before-switch: saved area tag = %q, want FARTHER", saved.CurrentMessageAreaTag)
	}
}

// TestSponsorEditAreaClearsOptionalFields pins "-" on the optional text
// fields: it empties them, where Enter keeps them. Name takes "-" as a literal
// value rather than clearing.
func TestSponsorEditAreaClearsOptionalFields(t *testing.T) {
	env := sponsorNewEnv(t)
	env.sub(t).runCmd("SPONSOREDITAREA", env.sysop, "", "eECHO\roOrigin\rkfsxnet\rq")

	r := env.sub(t).runCmd("SPONSOREDITAREA", env.sysop, "", "d-\rr-\rw-\re-\ro-\rk-\rq")
	for _, want := range []string{
		"Description (- to clear) [General discussion area]: ",
		"ACS Read (- to clear) [s10]: ", "ACS Write (- to clear) [s20]: ",
		"Echo Tag (- to clear) [ECHO]: ", "Origin Address (- to clear) [Origin]: ",
		"Network (- to clear) [fsxnet]: ",
	} {
		if !r.has(want) {
			t.Errorf("missing prompt %q:\n%s", want, r.text())
		}
	}
	got := sponsorDiskArea(env, 1)
	if got.Description != "" || got.ACSRead != "" || got.ACSWrite != "" ||
		got.EchoTag != "" || got.OriginAddr != "" || got.Network != "" {
		t.Errorf("want every optional text field cleared, got %+v", got)
	}

	// A plain sponsor may clear the fields they may edit, and Enter keeps them.
	env.sub(t).runCmd("SPONSOREDITAREA", env.caller, "", "dWords\rq")
	env.sub(t).runCmd("SPONSOREDITAREA", env.caller, "", "d\rq")
	if got := sponsorDiskArea(env, 1).Description; got != "Words" {
		t.Errorf("Enter should keep the description, got %q", got)
	}
	env.sub(t).runCmd("SPONSOREDITAREA", env.caller, "", "d-\rq")
	if got := sponsorDiskArea(env, 1).Description; got != "" {
		t.Errorf("sponsor's - should clear the description, got %q", got)
	}

	env.sub(t).runCmd("SPONSOREDITAREA", env.sysop, "", "n-\rq")
	if got := sponsorDiskArea(env, 1).Name; got != "-" {
		t.Errorf("name = %q, want - taken literally", got)
	}
}

// TestSponsorEditAreaConferenceID pins the Conference ID field for a sysop: it
// must be a non-negative number naming a configured conference, or 0 for
// ungrouped.
func TestSponsorEditAreaConferenceID(t *testing.T) {
	env := sponsorNewEnv(t)

	for _, bad := range []string{"abc", "-5"} {
		r := env.sub(t).runCmd("SPONSOREDITAREA", env.sysop, "", "c"+bad+"\rq")
		if !r.has("Invalid number - unchanged.") || r.has("saved.") {
			t.Errorf("reply %q: want it rejected and nothing saved:\n%s", bad, r.text())
		}
	}
	r := env.sub(t).runCmd("SPONSOREDITAREA", env.sysop, "", "c9\rq")
	if !r.has("Conference 9 does not exist - unchanged.") || r.has("saved.") {
		t.Errorf("unknown conference: want it rejected and nothing saved:\n%s", r.text())
	}
	if got := sponsorDiskArea(env, 1).ConferenceID; got != 1 {
		t.Errorf("saved conference = %d, want 1 untouched", got)
	}

	for _, tc := range []struct {
		reply string
		want  int
	}{{"2", 2}, {"0", 0}} {
		env.sub(t).runCmd("SPONSOREDITAREA", env.sysop, "", "c"+tc.reply+"\rq")
		if got := sponsorDiskArea(env, 1).ConferenceID; got != tc.want {
			t.Errorf("saved conference = %d, want %d", got, tc.want)
		}
	}

	// Without a conference manager only 0 can be vouched for.
	env.e.ConferenceMgr = nil
	r = env.sub(t).runCmd("SPONSOREDITAREA", env.sysop, "", "c1\rq")
	if !r.has("Conference 1 does not exist - unchanged.") {
		t.Errorf("no conference manager: want 1 rejected:\n%s", r.text())
	}
}

// TestSponsorEditAreaAreaType pins the Area Type field: only the recognised
// types are taken, in any case, and are saved in lower case.
func TestSponsorEditAreaAreaType(t *testing.T) {
	env := sponsorNewEnv(t)

	r := env.sub(t).runCmd("SPONSOREDITAREA", env.sysop, "", "ybogus\rq")
	if !r.has("Area Type (local/echomail/netmail/v3net/qwknet) [local]: ",
		"Unknown area type 'bogus' - unchanged.") || r.has("saved.") {
		t.Errorf("want the prompt listing the types and bogus rejected:\n%s", r.text())
	}
	for _, tc := range []struct{ reply, want string }{
		{"EchoMail", "echomail"}, {"netmail", "netmail"}, {"v3net", "v3net"},
		{"qwknet", "qwknet"}, {"LOCAL", "local"},
	} {
		env.sub(t).runCmd("SPONSOREDITAREA", env.sysop, "", "y"+tc.reply+"\rq")
		if got := sponsorDiskArea(env, 1).AreaType; got != tc.want {
			t.Errorf("reply %q: saved type = %q, want %q", tc.reply, got, tc.want)
		}
	}
}
