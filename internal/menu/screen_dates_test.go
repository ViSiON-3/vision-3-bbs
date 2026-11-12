package menu

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// The other screens show dates in the board's zone too. These tests set the
// board to Pacific/Kiritimati (UTC+14), where posted is 11/13/26, a day later
// than in any zone from UTC-12 to UTC+10 (see board_time_test.go).

// boardKiritimati sets env's board to Pacific/Kiritimati, as config.json's
// timezone does, skipping the test where that can't show.
func boardKiritimati(t *testing.T, env *menuEnv) {
	t.Helper()
	inKiritimati(t)
	cfg := env.e.GetServerConfig()
	cfg.Timezone = "Pacific/Kiritimati"
	env.e.SetServerConfig(cfg)
}

// News shows its items' dates in the board's zone, on the item and in the
// list.
func TestNewsDatesInBoardZone(t *testing.T) {
	env := newMenuEnv(t)
	boardKiritimati(t, env)
	seedNews(t, env, newsFixture(posted))

	if r := env.runCmd("PRINTNEWS", env.caller, "", "\r\r\r\r"); !r.has("11/13/2026") {
		t.Errorf("PRINTNEWS:\n%s", r.text())
	}
	if r := env.runCmd("LISTNEWS", env.caller, "", "q\r"); !r.has("11/13/26") {
		t.Errorf("LISTNEWS:\n%s", r.text())
	}
}

// The NUV screens date the candidate and their votes in the board's zone.
func TestNUVDatesInBoardZone(t *testing.T) {
	env := newMenuEnv(t)
	boardKiritimati(t, env)
	cand := NUVCandidate{Handle: "newbie", When: posted, Votes: []NUVVote{{Voter: "Sysop", Yes: true, Comment: "welcome", VotedAt: posted}}}

	ts := newTestSession("")
	nuvDisplayStats(env.e, newTestTerminal(ts), &cand, 0, env.outputMode)
	out := ts.output()
	if !strings.Contains(out, "11/13/2026") || !strings.Contains(out, "11/13/26") {
		t.Errorf("candidate stats:\n%s", out)
	}

	if err := saveNUVData(env.dataDir(), &NUVData{Candidates: []NUVCandidate{cand}}); err != nil {
		t.Fatal(err)
	}
	if r := env.runCmd("LISTNUV", env.sysop, "", "\r\r"); !r.has("newbie", "11/13/26") {
		t.Errorf("LISTNUV:\n%s", r.text())
	}
}

// The sysop's user and purge screens use the board's zone.
func TestAdminDatesInBoardZone(t *testing.T) {
	env := newMenuEnv(t)
	boardKiritimati(t, env)
	if got := adminTime(posted); got != "2026-11-13 03:00" {
		t.Errorf("adminTime = %q", got)
	}
	if got := adminDate(posted); got != "2026-11-13" {
		t.Errorf("adminDate = %q", got)
	}

	cfg := env.e.GetServerConfig()
	cfg.DeletedUserRetentionDays = 1
	env.e.SetServerConfig(cfg)
	when := posted.AddDate(0, 0, -30)
	env.writeUsers(append(defaultTestUsers(), deletedUserAt(3, "gone", &when))...)
	r := env.runCmd("PURGEUSERS", env.sysop, "", "N\r")
	if !r.has("gone", boardTime(when).Format("2006-01-02")) {
		t.Errorf("PURGEUSERS:\n%s", r.text())
	}
}

// A QWK device's last use, the |LCALL code and a file newscan date set by
// the caller are the board's too.
func TestOtherDatesInBoardZone(t *testing.T) {
	env := newMenuEnv(t)
	boardKiritimati(t, env)
	if got := deviceLastUsed(posted, "never"); got != "2026-11-13" {
		t.Errorf("deviceLastUsed = %q", got)
	}

	ph := map[string]string{}
	applyUserPlaceholders(ph, &user.User{Handle: "Caller", PreviousLogin: posted})
	if ph["|LCALL"] != "11/13/26" {
		t.Errorf("|LCALL = %q, want 11/13/26", ph["|LCALL"])
	}

	r := env.runCmd("SETFILESCANDATE", env.caller, "", "11/13/26\r")
	if !r.has("files since 11/13/2026") {
		t.Errorf("SETFILESCANDATE:\n%s", r.text())
	}
	if since := env.caller.FileNewscanSince; since == nil || !since.Equal(time.Date(2026, 11, 13, 0, 0, 0, 0, boardLocation())) {
		t.Errorf("FileNewscanSince = %v, want midnight 11/13/26 on the board", since)
	}
}

// A newscan date the caller types is midnight on the board, and the scan
// menu shows it as typed.
func TestScanDateInBoardZone(t *testing.T) {
	inKiritimati(t)
	cfg, out := runScanTypeWithInput(t, "D11/13/26\r\r", 10)
	if want := time.Date(2026, 11, 13, 0, 0, 0, 0, boardLocation()); cfg.ScanDate != want.Unix() {
		t.Errorf("ScanDate = %v, want %v", time.Unix(cfg.ScanDate, 0), want)
	}
	if !strings.Contains(out, "Since 11/13/26") {
		t.Errorf("scan menu:\n%s", out)
	}
	if got, ok := parseScanDate("11/13/26"); !ok || !got.Equal(time.Date(2026, 11, 13, 0, 0, 0, 0, boardLocation())) {
		t.Errorf("parseScanDate = %v, %v", got, ok)
	}
}

// The news header's time code, and the plain header used when NEWSHDR.ANS is
// missing, are the board's too.
func TestNewsHeaderInBoardZone(t *testing.T) {
	env := newMenuEnv(t)
	boardKiritimati(t, env)
	item := &NewsItem{ID: 1, Title: "Hello", From: "Sysop", When: posted}

	hdr, _ := renderNewsHeader(env.e, item, 1, env.outputMode)
	if !strings.Contains(string(hdr), "3:00 am") {
		t.Errorf("header: %q", hdr)
	}
	env.e.MenuSetPath = t.TempDir() // no NEWSHDR.ANS
	hdr, _ = renderNewsHeader(env.e, item, 1, env.outputMode)
	if !strings.Contains(string(hdr), "News #1") || !strings.Contains(string(hdr), "11/13/2026") {
		t.Errorf("fallback header: %q", hdr)
	}
}

// LISTNUV dates the queue in the board's zone when it redraws it after an
// action, as when it first draws it.
func TestNUVListRedrawInBoardZone(t *testing.T) {
	env := newMenuEnv(t)
	boardKiritimati(t, env)
	if err := saveNUVData(env.dataDir(), &NUVData{Candidates: []NUVCandidate{
		{Handle: "newbie", When: posted}, {Handle: "other", When: posted},
	}}); err != nil {
		t.Fatal(err)
	}
	// Two dates on the first drawing, and newbie's again after other is
	// removed.
	r := env.runCmd("LISTNUV", env.sysop, "", "R2\rQ")
	if n := strings.Count(r.text(), "11/13/26"); n != 3 {
		t.Errorf("date shown %d times, want 3:\n%s", n, r.text())
	}
}

// The BBS list's details, the purge cutoff and the QWK device list use the
// board's zone.
func TestListDatesInBoardZone(t *testing.T) {
	env := newMenuEnv(t)
	boardKiritimati(t, env)
	if err := saveBBSListData(env.dataDir(), &bbsListData{NextID: 2, Listings: []BBSListing{
		{ID: 1, Name: "Dated Board", Address: "dated.example", AddedBy: "Sysop", AddedDate: posted},
	}}); err != nil {
		t.Fatal(err)
	}
	if r := env.runCmd("BBSLIST", env.caller, "", "q"); !r.has("Dated Board", "11/13/2026") {
		t.Errorf("BBSLIST:\n%s", r.text())
	}

	cfg := env.e.GetServerConfig()
	cfg.DeletedUserRetentionDays = 1
	env.e.SetServerConfig(cfg)
	when := posted.AddDate(0, 0, -30)
	env.writeUsers(append(defaultTestUsers(), deletedUserAt(3, "gone", &when))...)
	before := boardTime(time.Now().AddDate(0, 0, -1)).Format("2006-01-02")
	r := env.runCmd("PURGEUSERS", env.sysop, "", "N\r")
	after := boardTime(time.Now().AddDate(0, 0, -1)).Format("2006-01-02")
	if !r.has(before) && !r.has(after) {
		t.Errorf("PURGEUSERS cutoff, want %s:\n%s", after, r.text())
	}

	c, screen, um := devicesCtx(t, true, "\r", "Phone")
	dcfg := c.e.GetServerConfig()
	dcfg.Timezone = "Pacific/Kiritimati" // devicesCtx's own executor
	c.e.SetServerConfig(dcfg)
	d := deviceScreen{e: c.e, s: c.s, terminal: c.terminal, mode: c.outputMode, um: um, width: 80, height: 24,
		userID: c.currentUser.ID, owner: c.currentUser.Handle}
	_ = d.run()
	if want := boardTime(time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)).Format(deviceDateFormat); !strings.Contains(screen.Snapshot(), want) {
		t.Errorf("device list, want %s:\n%s", want, screen.Snapshot())
	}
}

// The file lightbar's info view gives the upload time in the board's zone.
func TestFileLightbarInfoInBoardZone(t *testing.T) {
	_, _, out, _ := runFileLightbar(t, "i q q")
	if want := boardTime(fixedUploadTime).Format("01/02/2006 15:04"); !strings.Contains(testAnsiEscape.ReplaceAllString(out, ""), "Uploaded  : "+want) {
		t.Errorf("info view, want %s:\n%s", want, out)
	}
}
