package menu

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TestVisibleCallRecords_HidesInvisibleForEveryViewer covers issue #334: a
// login recorded as invisible must be dropped from the last callers list
// regardless of who is viewing it. The filter takes no viewer, so a SysOp
// looking at the list gets exactly the same rows as any other user.
func TestVisibleCallRecords_HidesInvisibleForEveryViewer(t *testing.T) {
	records := []user.CallRecord{
		{UserID: 1, Handle: "sysop", Invisible: true},
		{UserID: 2, Handle: "alice"},
		{UserID: 1, Handle: "sysop", Invisible: true},
		{UserID: 3, Handle: "bob"},
	}

	got := visibleCallRecords(records)

	if len(got) != 2 {
		t.Fatalf("visibleCallRecords returned %d records, want 2: %+v", len(got), got)
	}
	if got[0].Handle != "alice" || got[1].Handle != "bob" {
		t.Errorf("visibleCallRecords order/content wrong: got %q, %q", got[0].Handle, got[1].Handle)
	}
	for _, rec := range got {
		if rec.Invisible {
			t.Errorf("invisible record %q leaked into the visible list", rec.Handle)
		}
	}
}

func TestVisibleCallRecords_EmptyInputIsEmptyNonNil(t *testing.T) {
	got := visibleCallRecords(nil)
	if got == nil || len(got) != 0 {
		t.Fatalf("visibleCallRecords(nil) = %v, want empty non-nil slice", got)
	}
}

// TestMostRecentCallRecords_KeepsNewestWindow verifies the row limit keeps the
// most recent records and preserves the oldest-first render order.
func TestMostRecentCallRecords_KeepsNewestWindow(t *testing.T) {
	records := make([]user.CallRecord, 0, 30)
	for i := 0; i < 30; i++ {
		records = append(records, user.CallRecord{CallNumber: uint64(i)})
	}

	got := mostRecentCallRecords(records, 20)

	if len(got) != 20 {
		t.Fatalf("mostRecentCallRecords returned %d records, want 20", len(got))
	}
	if got[0].CallNumber != 10 {
		t.Errorf("kept the wrong window: first record is call %d, want 10", got[0].CallNumber)
	}
	if got[len(got)-1].CallNumber != 29 {
		t.Errorf("newest record missing: last is call %d, want 29", got[len(got)-1].CallNumber)
	}
}

// TestMostRecentCallRecords_PassThrough covers the limits that must not slice:
// a limit at or above the record count, and the zero/negative "no limit" cases.
func TestMostRecentCallRecords_PassThrough(t *testing.T) {
	records := []user.CallRecord{{CallNumber: 1}, {CallNumber: 2}, {CallNumber: 3}}

	for _, limit := range []int{3, 10, 0, -1} {
		got := mostRecentCallRecords(records, limit)
		if len(got) != len(records) {
			t.Errorf("limit %d: got %d records, want all %d", limit, len(got), len(records))
		}
	}
}

// TestLastCallerRows_HiddenLoginsDoNotConsumeRows is the regression guard for
// the starvation case behind this change: filtering runs before the row limit,
// so a history that is mostly hidden logins still fills the screen with real
// callers. Ordering mirrors runLastCallers.
func TestLastCallerRows_HiddenLoginsDoNotConsumeRows(t *testing.T) {
	// 125 stored records: every fifth is a real caller, the rest are hidden.
	records := make([]user.CallRecord, 0, 125)
	for i := 0; i < 125; i++ {
		if i%5 == 0 {
			records = append(records, user.CallRecord{UserID: 1, CallNumber: uint64(i)})
			continue
		}
		records = append(records, user.CallRecord{UserID: 2, CallNumber: uint64(i), Invisible: true})
	}

	got := mostRecentCallRecords(visibleCallRecords(records), defaultLastCallerRows)

	if len(got) != defaultLastCallerRows {
		t.Fatalf("got %d rows, want a full screen of %d", len(got), defaultLastCallerRows)
	}
	for _, rec := range got {
		if rec.Invisible {
			t.Errorf("invisible record (call %d) leaked into the visible list", rec.CallNumber)
		}
	}
}

// TestLastCallerRows_LimitIsNotCapped guards the documented contract that
// RUN:LASTCALLERS 25 shows 25 entries; the default must not impose a ceiling.
func TestLastCallerRows_LimitIsNotCapped(t *testing.T) {
	records := make([]user.CallRecord, 0, 40)
	for i := 0; i < 40; i++ {
		records = append(records, user.CallRecord{CallNumber: uint64(i)})
	}

	got := mostRecentCallRecords(visibleCallRecords(records), 25)

	if len(got) != 25 {
		t.Fatalf("a request for 25 rows returned %d; the default must not cap it", len(got))
	}
}

// TestLastCallerRowsThatFit_StockTemplatesOn25Rows covers the login screen
// overflowing a 25-row terminal: a 7-line header, 2-line footer and the pause
// prompt line leave room for 15 caller rows, not the default 20.
func TestLastCallerRowsThatFit_StockTemplatesOn25Rows(t *testing.T) {
	top := "logo1\r\nlogo2\r\nlogo3\r\nlogo4\r\n----\r\nheader\r\n----" // no trailing break, like LASTCALL.TOP
	bot := "----\r\nTotal Users: 14"
	pause := "|15SlAm eNtEr!" // stock prompt: no leading break, so the helper adds one

	if got := lastCallerRowsThatFit(25, top, "row\r\n", bot, pause); got != 15 {
		t.Errorf("25 rows: got %d caller rows, want 15", got)
	}
	if got := lastCallerRowsThatFit(24, top, "row\r\n", bot, pause); got != 14 {
		t.Errorf("24 rows: got %d caller rows, want 14", got)
	}
	// A top template that already ends in a break gets no extra one.
	if got := lastCallerRowsThatFit(25, top+"\r\n", "row\r\n", bot, pause); got != 15 {
		t.Errorf("top with trailing break: got %d, want 15", got)
	}
}

// TestLastCallerRowsThatFit_LeadingBreakPrompt mirrors writeCenteredPausePrompt:
// a prompt that starts with a break has it stripped and no break written, so
// the break a bottom template ends with is the only one before the prompt.
func TestLastCallerRowsThatFit_LeadingBreakPrompt(t *testing.T) {
	// One-line top, "footer\n" bottom, "\r\nprompt": top, one caller, footer
	// and prompt fill exactly four rows.
	for _, prompt := range []string{"\r\nprompt", "\nprompt"} {
		if got := lastCallerRowsThatFit(4, "top", "row\r\n", "footer\n", prompt); got != 1 {
			t.Errorf("prompt %q: got %d caller rows, want 1", prompt, got)
		}
	}
	// Without the bottom's trailing break the helper still adds none, so the
	// prompt shares the footer's row.
	if got := lastCallerRowsThatFit(3, "top", "row\r\n", "footer", "\r\nprompt"); got != 1 {
		t.Errorf("unterminated footer: got %d caller rows, want 1", got)
	}
	// Breaks inside the prompt text each take a row.
	if got := lastCallerRowsThatFit(5, "top", "row\r\n", "footer", "line one\r\nline two"); got != 1 {
		t.Errorf("two-line prompt: got %d caller rows, want 1", got)
	}
}

func TestLastCallerRowsThatFit_Edges(t *testing.T) {
	if got := lastCallerRowsThatFit(0, "a", "row\r\n", "b", "c"); got != -1 {
		t.Errorf("unknown height: got %d, want -1 (no trimming)", got)
	}
	if got := lastCallerRowsThatFit(2, "a\r\nb\r\nc", "row\r\n", "d", "e"); got != 0 {
		t.Errorf("screen shorter than the frame: got %d, want 0", got)
	}
}

// TestLastCallerRowsThatFit_MultiLineMid covers a custom LASTCALL.MID that
// spans several lines: each caller costs that many rows.
func TestLastCallerRowsThatFit_MultiLineMid(t *testing.T) {
	top := "top"      // 1 row
	bot := "footer"   // 1 row
	pause := "prompt" // 1 row after the helper's break
	// 25 rows leave 22 for callers.
	if got := lastCallerRowsThatFit(25, top, "line one\r\nline two\r\n", bot, pause); got != 11 {
		t.Errorf("two-line mid: got %d callers, want 11", got)
	}
	if got := lastCallerRowsThatFit(25, top, "a\r\nb\r\nc", bot, pause); got != 7 {
		t.Errorf("three-line mid: got %d callers, want 7", got)
	}
}

// seedLastCallers records three visible calls (oldest first) and one
// invisible call, and gives the caller a private note for @NOTE@.
func seedLastCallers(t *testing.T, env *menuEnv) {
	t.Helper()
	c := *env.caller
	c.PrivateNote = "regular"
	if err := env.um.UpdateUserByID(&c); err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-3 * time.Hour)
	for i, h := range []string{"Alpha", "Bravo", "Caller"} {
		id := 10 + i
		if h == "Caller" {
			id = 2
		}
		env.um.AddCallRecord(user.CallRecord{
			UserID: id, Handle: h, NodeID: i + 1,
			ConnectTime: base.Add(time.Duration(i) * time.Hour), Duration: 42 * time.Minute,
		})
	}
	env.um.AddCallRecord(user.CallRecord{UserID: 1, Handle: "Sysop", NodeID: 4, ConnectTime: time.Now(), Invisible: true})
}

// TestLastCallers_ListsRecentVisibleCalls pins the LASTCALLERS screen over
// seeded call history: each visible call appears with its minutes online and
// the user's note, invisible logins are hidden, and the footer's user count
// is filled in.
func TestLastCallers_ListsRecentVisibleCalls(t *testing.T) {
	env := newMenuEnv(t)
	seedLastCallers(t, env)
	r := env.runCmd("LASTCALLERS", env.caller, "", "\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("Alpha", "Bravo", "Caller", "regular", "42") {
		t.Errorf("output:\n%s", r.text())
	}
	if r.has("Sysop") {
		t.Errorf("invisible login listed:\n%s", r.text())
	}
	if strings.Contains(r.text(), "@USERCT@") || !r.has("Total Users:") {
		t.Errorf("user count token not rendered:\n%s", r.text())
	}
	if strings.Index(r.text(), "Alpha") > strings.Index(r.text(), "Caller") {
		t.Error("calls not listed oldest to newest")
	}
}

// TestLastCallers_ArgumentLimitsRows pins that a numeric argument keeps only
// the newest N visible calls, and a junk argument falls back to the default.
func TestLastCallers_ArgumentLimitsRows(t *testing.T) {
	env := newMenuEnv(t)
	seedLastCallers(t, env)
	r := env.runCmd("LASTCALLERS", env.caller, "1", "\r")
	if !r.has("Caller") || r.has("Alpha") || r.has("Bravo") {
		t.Errorf("limit 1 output:\n%s", r.text())
	}
	r = env.runCmd("LASTCALLERS", env.caller, "lots", "\r")
	if !r.has("Alpha", "Bravo", "Caller") {
		t.Errorf("junk-arg output:\n%s", r.text())
	}
	if r := env.runCmd("LASTCALLERS", env.caller, "", ""); r.next != "LOGOFF" {
		t.Errorf("disconnect at pause: next = %q, want LOGOFF", r.next)
	}
}
