package menu

import (
	"testing"

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

// TestVisibleCallRecords_CapsAtDisplayLimit verifies the screen renders at most
// lastCallersDisplayLimit rows and keeps the most recent ones, in order.
func TestVisibleCallRecords_CapsAtDisplayLimit(t *testing.T) {
	records := make([]user.CallRecord, 0, lastCallersDisplayLimit+10)
	for i := 0; i < lastCallersDisplayLimit+10; i++ {
		records = append(records, user.CallRecord{UserID: i, CallNumber: uint64(i)})
	}

	got := visibleCallRecords(records)

	if len(got) != lastCallersDisplayLimit {
		t.Fatalf("visibleCallRecords returned %d records, want %d", len(got), lastCallersDisplayLimit)
	}
	// The oldest 10 must have been dropped, not the newest.
	if got[0].CallNumber != 10 {
		t.Errorf("kept the wrong window: first record is call %d, want 10", got[0].CallNumber)
	}
	if got[len(got)-1].CallNumber != uint64(lastCallersDisplayLimit+9) {
		t.Errorf("newest record missing: last is call %d, want %d", got[len(got)-1].CallNumber, lastCallersDisplayLimit+9)
	}
}

// TestVisibleCallRecords_HiddenRecordsDoNotConsumeSlots is the regression guard
// for the starvation case: a deep history that is mostly invisible logins must
// still fill the screen with the visible callers it contains.
func TestVisibleCallRecords_HiddenRecordsDoNotConsumeSlots(t *testing.T) {
	// 100 hidden logins interleaved with 25 real callers.
	records := make([]user.CallRecord, 0, 125)
	realSeen := 0
	for i := 0; i < 125; i++ {
		if i%5 == 0 {
			realSeen++
			records = append(records, user.CallRecord{UserID: 1, CallNumber: uint64(i)})
			continue
		}
		records = append(records, user.CallRecord{UserID: 2, CallNumber: uint64(i), Invisible: true})
	}
	if realSeen != 25 {
		t.Fatalf("test setup wrong: built %d visible records, want 25", realSeen)
	}

	got := visibleCallRecords(records)

	if len(got) != lastCallersDisplayLimit {
		t.Fatalf("got %d rows, want a full screen of %d", len(got), lastCallersDisplayLimit)
	}
	for _, rec := range got {
		if rec.Invisible {
			t.Errorf("invisible record (call %d) leaked into the visible list", rec.CallNumber)
		}
	}
}
