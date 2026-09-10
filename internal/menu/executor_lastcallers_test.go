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
