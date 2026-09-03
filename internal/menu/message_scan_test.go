package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TestParseScanDate checks every accepted date layout yields local midnight of
// the same day and that malformed or ambiguous input is rejected.
func TestParseScanDate(t *testing.T) {
	want := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.Local)
	for _, in := range []string{
		"09/01/26", "9/1/26", "09/01/2026", "9/1/2026",
		"09-01-26", "9-1-2026", "09.01.26", "9.1.2026",
		"2026-09-01", "2026-9-1", "2026/09/01",
		"090126", "09012026", "  09/01/26  ",
	} {
		got, ok := parseScanDate(in)
		if !ok {
			t.Errorf("parseScanDate(%q) rejected", in)
			continue
		}
		if !got.Equal(want) {
			t.Errorf("parseScanDate(%q) = %v, want %v", in, got, want)
		}
	}
	for _, in := range []string{"", "foo", "13/01/26", "09/32/26", "2026", "9/1", "09/01/26/1"} {
		if got, ok := parseScanDate(in); ok {
			t.Errorf("parseScanDate(%q) accepted as %v, want rejection", in, got)
		}
	}
}

// TestScanConfigFilter checks that To/From match case-insensitively on their
// own field only, that range and date bounds are inclusive, and that a scan
// with no search settings has no filter at all.
func TestScanConfigFilter(t *testing.T) {
	msg := func(n int, from, to string, when time.Time) *message.DisplayMessage {
		return &message.DisplayMessage{MsgNum: n, From: from, To: to, DateTime: when}
	}
	now := time.Now()

	if f := (&ScanConfig{ScanDate: scanDateNewOnly}).filter(); f != nil {
		t.Error("new-only scan with no search should have no filter")
	}
	if f := (&ScanConfig{ScanDate: scanDateAll}).filter(); f != nil {
		t.Error("all-messages scan with no search should have no filter")
	}

	from := (&ScanConfig{ScanDate: scanDateNewOnly, SearchFrom: "  joe "}).filter()
	if from == nil {
		t.Fatal("From search produced no filter")
	}
	if !from(msg(1, "Joe Smith", "All", now)) {
		t.Error("From filter should match case-insensitively as a substring")
	}
	if from(msg(2, "Bob Jones", "Joe Smith", now)) {
		t.Error("From filter must not match the To field")
	}

	to := (&ScanConfig{SearchTo: "SYSOP"}).filter()
	if !to(msg(1, "Joe", "sysop", now)) || to(msg(2, "Sysop", "All", now)) {
		t.Error("To filter should match only the To field, case-insensitively")
	}

	rng := (&ScanConfig{RangeStart: 3, RangeEnd: 5}).filter()
	for n, want := range map[int]bool{2: false, 3: true, 5: true, 6: false} {
		if got := rng(msg(n, "a", "b", now)); got != want {
			t.Errorf("range 3-5 filter(msg %d) = %v, want %v", n, got, want)
		}
	}

	since := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.Local)
	dated := (&ScanConfig{ScanDate: since.Unix()}).filter()
	if dated(msg(1, "a", "b", since.Add(-time.Second))) {
		t.Error("date filter should reject a message from the day before")
	}
	if !dated(msg(2, "a", "b", since)) || !dated(msg(3, "a", "b", since.Add(23*time.Hour))) {
		t.Error("date filter should accept messages from the chosen day onward (inclusive)")
	}
}

// newScanTestArea creates a message manager with one local area holding
// messages from Alice, Bob, Carol and Bob again (numbers 1-4), dated one day
// apart ending today.
func newScanTestArea(t *testing.T) (*message.MessageManager, int) {
	t.Helper()
	mm, err := message.NewMessageManager(t.TempDir(), t.TempDir(), "TestBBS", nil)
	if err != nil {
		t.Fatalf("NewMessageManager: %v", err)
	}
	areaID, err := mm.AddArea(message.MessageArea{Tag: "GENERAL", Name: "General", AreaType: "local"})
	if err != nil {
		t.Fatalf("AddArea: %v", err)
	}
	base := time.Now().Add(-3 * 24 * time.Hour)
	for i, m := range []struct{ from, to, subj string }{
		{"Alice", "All", "alice-post"},
		{"Bob Jones", "Alice", "bob-first"},
		{"Carol", "Bob Jones", "carol-post"},
		{"Bob Jones", "All", "bob-second"},
	} {
		if _, err := mm.AddMessageWithDate(areaID, m.from, m.to, m.subj, "body", "", base.Add(time.Duration(i)*24*time.Hour)); err != nil {
			t.Fatalf("AddMessageWithDate %d: %v", i+1, err)
		}
	}
	return mm, areaID
}

// TestDetermineStartMessageHonoursSearchAndRange checks the start message
// against a real JAM area for each combination of date mode, search and range,
// including the "skip this area" result when nothing matches.
func TestDetermineStartMessageHonoursSearchAndRange(t *testing.T) {
	mm, areaID := newScanTestArea(t)
	e := &MenuExecutor{MessageMgr: mm}
	const total = 4

	cases := []struct {
		name string
		cfg  ScanConfig
		want int
	}{
		{"all messages, no search", ScanConfig{ScanDate: scanDateAll}, 1},
		{"from search finds first match", ScanConfig{ScanDate: scanDateAll, SearchFrom: "bob"}, 2},
		{"to search finds first match", ScanConfig{ScanDate: scanDateAll, SearchTo: "bob"}, 3},
		{"no match skips the area", ScanConfig{ScanDate: scanDateAll, SearchFrom: "zed"}, total + 1},
		{"range start is honoured", ScanConfig{ScanDate: scanDateAll, RangeStart: 3, RangeEnd: 4}, 3},
		{"search stops at range end", ScanConfig{ScanDate: scanDateAll, RangeStart: 1, RangeEnd: 2, SearchFrom: "carol"}, total + 1},
		{"new only with search skips read matches", ScanConfig{ScanDate: scanDateNewOnly, SearchFrom: "bob"}, 4},
		{"date scan starts on the chosen day", ScanConfig{ScanDate: time.Now().Add(-24 * time.Hour).Truncate(time.Hour).Unix()}, 3},
	}

	// Mark messages 1-2 read so "new only" starts at 3.
	if err := mm.SetLastRead(areaID, "tester", 2); err != nil {
		t.Fatalf("SetLastRead: %v", err)
	}
	for _, tc := range cases {
		cfg := tc.cfg
		if got := determineStartMessage(e, &cfg, areaID, "tester", total); got != tc.want {
			t.Errorf("%s: determineStartMessage = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestPreserveLastReadRestoresPointerWhenUpdatesAreOff checks that the restore
// func puts the last-read pointer back only when pointer updates are off.
func TestPreserveLastReadRestoresPointerWhenUpdatesAreOff(t *testing.T) {
	mm, areaID := newScanTestArea(t)
	e := &MenuExecutor{MessageMgr: mm}
	if err := mm.SetLastRead(areaID, "tester", 1); err != nil {
		t.Fatalf("SetLastRead: %v", err)
	}

	restore := preserveLastRead(e, &ScanConfig{UpdatePointers: false}, areaID, "tester", 1)
	if err := mm.SetLastRead(areaID, "tester", 4); err != nil {
		t.Fatalf("SetLastRead: %v", err)
	}
	restore()
	if lr, _ := mm.GetLastRead(areaID, "tester"); lr != 1 {
		t.Errorf("lastread after restore = %d, want 1", lr)
	}

	restore = preserveLastRead(e, &ScanConfig{UpdatePointers: true}, areaID, "tester", 1)
	if err := mm.SetLastRead(areaID, "tester", 3); err != nil {
		t.Fatalf("SetLastRead: %v", err)
	}
	restore()
	if lr, _ := mm.GetLastRead(areaID, "tester"); lr != 3 {
		t.Errorf("lastread with updates on = %d, want 3 (kept)", lr)
	}
}

// loadTestStrings loads the shipped strings.json so the scan menu renders
// with the real prompts.
func loadTestStrings(t *testing.T) config.StringsConfig {
	t.Helper()
	strs, err := config.LoadStrings(filepath.Join("..", "..", "templates", "configs"))
	if err != nil {
		t.Fatalf("LoadStrings: %v", err)
	}
	return strs
}

// runScanTypeWithInput drives runGetScanType with scripted keystrokes and
// returns the resulting config plus the rendered output with ANSI stripped.
func runScanTypeWithInput(t *testing.T, input string, numMsgs int) (*ScanConfig, string) {
	t.Helper()
	scanNoticePause = 0
	t.Cleanup(func() { scanNoticePause = time.Second })

	e := &MenuExecutor{LoadedStrings: loadTestStrings(t)}
	ts := newTestSession(input)
	terminal := newTestTerminal(ts)
	ih := getSessionIH(ts)
	t.Cleanup(func() { resetSessionIH(ts) })

	cfg, err := runGetScanType(ih, e, terminal, ansi.OutputModeUTF8, numMsgs, true)
	if err != nil {
		t.Fatalf("runGetScanType: %v", err)
	}
	return cfg, testAnsiEscape.ReplaceAllString(ts.output(), "")
}

// TestRunGetScanTypeCollectsSearchFields checks the T, F and U options are
// stored and echoed back on the menu.
func TestRunGetScanTypeCollectsSearchFields(t *testing.T) {
	cfg, out := runScanTypeWithInput(t, "Fbob jones\rTsysop\rU\r", 10)
	if cfg.Aborted {
		t.Fatal("scan aborted unexpectedly")
	}
	if cfg.SearchFrom != "bob jones" || cfg.SearchTo != "sysop" {
		t.Errorf("From/To = %q/%q, want bob jones/sysop", cfg.SearchFrom, cfg.SearchTo)
	}
	if cfg.UpdatePointers {
		t.Error("U should toggle Update Pointers off")
	}
	if !strings.Contains(out, "Search For bob jones") || !strings.Contains(out, "Search For sysop") {
		t.Errorf("menu should echo the search strings; output:\n%s", out)
	}
}

// TestRunGetScanTypeDateAcceptsCommonFormatsAndReportsBadOnes checks the D
// option: an ISO date is accepted and displayed, the prompt states the
// format, a bad date is reported and leaves the setting alone, and "all"
// selects every message.
func TestRunGetScanTypeDateAcceptsCommonFormatsAndReportsBadOnes(t *testing.T) {
	want := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.Local)

	cfg, out := runScanTypeWithInput(t, "D2026-09-01\r\r", 10)
	if cfg.ScanDate != want.Unix() {
		t.Errorf("ScanDate = %d, want %d (2026-09-01 local midnight)", cfg.ScanDate, want.Unix())
	}
	if !strings.Contains(out, "Since 09/01/26") {
		t.Errorf("menu should show the chosen date; output:\n%s", out)
	}
	if !strings.Contains(out, "MM/DD/YY") {
		t.Errorf("date prompt should show an example format; output:\n%s", out)
	}
	if !strings.Contains(out, "Since 09/01/26") {
		t.Errorf("menu should show the chosen date; output:\n%s", out)
	}

	// "xyz" (not "not-a-date": a leading N means New, like a leading A means All)
	cfg, out = runScanTypeWithInput(t, "Dxyz\r\r", 10)
	if cfg.ScanDate != scanDateNewOnly {
		t.Errorf("bad date changed ScanDate to %d; want it left at new-only", cfg.ScanDate)
	}
	if !strings.Contains(out, "Invalid date") {
		t.Errorf("bad date should be reported; output:\n%s", out)
	}

	cfg, _ = runScanTypeWithInput(t, "Dall\r\r", 10)
	if cfg.ScanDate != scanDateAll {
		t.Errorf("'all' gave ScanDate %d, want all-messages", cfg.ScanDate)
	}
}

// TestRunGetScanTypeRangeValidation checks a valid range is stored, ESC at
// the prompt keeps an existing range, Enter alone clears it, and an
// out-of-bounds end clears the whole range with a notice.
func TestRunGetScanTypeRangeValidation(t *testing.T) {
	cfg, _ := runScanTypeWithInput(t, "R2\r5\r\r", 10)
	if cfg.RangeStart != 2 || cfg.RangeEnd != 5 {
		t.Errorf("range = %d-%d, want 2-5", cfg.RangeStart, cfg.RangeEnd)
	}

	// ESC at the start prompt must leave the previously set range alone.
	cfg, _ = runScanTypeWithInput(t, "R2\r5\rR\x1b\r", 10)
	if cfg.RangeStart != 2 || cfg.RangeEnd != 5 {
		t.Errorf("range after ESC = %d-%d, want 2-5 kept", cfg.RangeStart, cfg.RangeEnd)
	}

	// Enter alone at the start prompt clears the range without a notice.
	cfg, out := runScanTypeWithInput(t, "R2\r5\rR\r\r", 10)
	if cfg.RangeStart != 0 || cfg.RangeEnd != 0 {
		t.Errorf("range after Enter = %d-%d, want cleared", cfg.RangeStart, cfg.RangeEnd)
	}
	if strings.Contains(out, "Invalid range") {
		t.Errorf("clearing with Enter should not report an invalid range; output:\n%s", out)
	}

	// A new start followed by Enter alone at the end prompt is an abandoned
	// edit: the old range must not stay in effect.
	cfg, _ = runScanTypeWithInput(t, "R2\r5\rR3\r\r\r", 10)
	if cfg.RangeStart != 0 || cfg.RangeEnd != 0 {
		t.Errorf("range after abandoned edit = %d-%d, want cleared", cfg.RangeStart, cfg.RangeEnd)
	}

	// Bad end must not leave a stale start bound in effect while the menu says "All".
	cfg, out = runScanTypeWithInput(t, "R2\r99\r\r", 10)
	if cfg.RangeStart != 0 || cfg.RangeEnd != 0 {
		t.Errorf("range after bad end = %d-%d, want cleared", cfg.RangeStart, cfg.RangeEnd)
	}
	if !strings.Contains(out, "Invalid range") {
		t.Errorf("bad range should be reported; output:\n%s", out)
	}
}

// TestRunGetScanTypeArrowKeysAreNotHotkeys checks decoded arrow keys are
// ignored by the menu while a lone ESC aborts it.
func TestRunGetScanTypeArrowKeysAreNotHotkeys(t *testing.T) {
	// Up arrow (ESC [ A) used to be read byte-by-byte, so its "A" aborted the
	// scan; left arrow (ESC [ D) opened the date prompt.
	cfg, _ := runScanTypeWithInput(t, "\x1b[A\x1b[D\r", 10)
	if cfg.Aborted {
		t.Error("an arrow key aborted the scan")
	}
	if cfg.ScanDate != scanDateNewOnly {
		t.Errorf("an arrow key changed the scan date to %d", cfg.ScanDate)
	}

	cfg, _ = runScanTypeWithInput(t, "\x1b", 10)
	if !cfg.Aborted {
		t.Error("a lone ESC should abort the scan")
	}
}

// TestNewScanMultiAreaReportsNoMatches drives an all-areas-in-conference
// scan whose From search matches nothing: every area is skipped and the scan
// must end with the no-matches notice, not "Newscan complete".
func TestNewScanMultiAreaReportsNoMatches(t *testing.T) {
	scanNoticePause = 0
	t.Cleanup(func() { scanNoticePause = time.Second })

	mm, areaID := newScanTestArea(t)
	um, err := user.NewUserManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewUserManager: %v", err)
	}
	u, err := um.AddUser("password", "Tester", "Real Name", "Loc")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	u.CurrentMessageAreaID = areaID
	u.CurrentMessageAreaTag = "GENERAL"

	e := &MenuExecutor{MessageMgr: mm, MenuSetPath: t.TempDir(), LoadedStrings: loadTestStrings(t)}

	// Scan menu: Date=All, From=zed (no such author), S then A = all areas
	// in the conference, Enter to scan.
	ts := newTestSession("Dall\rFzed\rSA\r")
	terminal := newTestTerminal(ts)
	t.Cleanup(func() { resetSessionIH(ts) })

	if _, action, err := runNewScanAll(e, ts, terminal, um, u, 1, time.Now(), ansi.OutputModeUTF8, false, 80, 24); err != nil || action == "LOGOFF" {
		t.Fatalf("runNewScanAll: action=%q err=%v", action, err)
	}

	out := testAnsiEscape.ReplaceAllString(ts.output(), "")
	if !strings.Contains(out, "No messages match") {
		t.Errorf("filtered scan with no matches should say so; output:\n%s", out)
	}
	if strings.Contains(out, "Newscan complete") {
		t.Errorf("filtered scan with no matches should not report completion; output:\n%s", out)
	}
}

// TestNewScanCurrentAreaAppliesFromSearchAndKeepsPointers drives the whole
// current-area scan: From search "bob" must show only Bob's messages, and with
// Update Pointers off the last-read pointer must be back where it started.
func TestNewScanCurrentAreaAppliesFromSearchAndKeepsPointers(t *testing.T) {
	scanNoticePause = 0
	t.Cleanup(func() { scanNoticePause = time.Second })

	mm, areaID := newScanTestArea(t)
	if err := mm.SetLastRead(areaID, "Tester", 1); err != nil {
		t.Fatalf("SetLastRead: %v", err)
	}

	// The reader needs a MSGHDR template under the menu set path.
	menuSet := t.TempDir()
	hdrDir := filepath.Join(menuSet, "templates", "message_headers")
	if err := os.MkdirAll(hdrDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hdr, err := os.ReadFile(filepath.Join("..", "..", "menus", "v3", "templates", "message_headers", "MSGHDR.2.ans"))
	if err != nil {
		t.Fatalf("read shipped MSGHDR.2.ans: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hdrDir, "MSGHDR.2.ans"), hdr, 0o644); err != nil {
		t.Fatal(err)
	}

	um, err := user.NewUserManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewUserManager: %v", err)
	}
	u, err := um.AddUser("password", "Tester", "Real Name", "Loc")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	u.AccessLevel = 10
	u.MsgHdr = 2
	u.CurrentMessageAreaID = areaID
	u.CurrentMessageAreaTag = "GENERAL"

	e := &MenuExecutor{MessageMgr: mm, MenuSetPath: menuSet, LoadedStrings: loadTestStrings(t)}
	e.ServerCfg.CoSysOpLevel = 200

	// Scan menu: Date=All, From=bob, Update Pointers off, Enter to scan.
	// Reader: N (next) twice runs past the last match and ends the scan.
	ts := newTestSession("Dall\rFbob\rU\rNN")
	terminal := newTestTerminal(ts)
	t.Cleanup(func() { resetSessionIH(ts) })

	if _, action, err := runNewScanAll(e, ts, terminal, um, u, 1, time.Now(), ansi.OutputModeUTF8, true, 80, 24); err != nil || action == "LOGOFF" {
		t.Fatalf("runNewScanAll: action=%q err=%v", action, err)
	}

	out := testAnsiEscape.ReplaceAllString(ts.output(), "")
	for _, want := range []string{"bob-first", "bob-second"} {
		if !strings.Contains(out, want) {
			t.Errorf("scan output missing Bob's message %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"alice-post", "carol-post"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("scan output shows non-matching message %q:\n%s", unwanted, out)
		}
	}
	if lr, _ := mm.GetLastRead(areaID, "Tester"); lr != 1 {
		t.Errorf("lastread after scan with Update Pointers off = %d, want 1", lr)
	}
}
