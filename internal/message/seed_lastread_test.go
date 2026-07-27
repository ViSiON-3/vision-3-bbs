package message

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newSeedTestManager(t *testing.T) *MessageManager {
	t.Helper()
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	areas := `[{"id":1,"tag":"GENERAL","name":"General","base_path":"general","area_type":"local"}]`
	if err := os.WriteFile(filepath.Join(cfg, "message_areas.json"), []byte(areas), 0o644); err != nil {
		t.Fatal(err)
	}
	mm, err := NewMessageManager(tmp, cfg, "TestBBS", nil)
	if err != nil {
		t.Fatalf("NewMessageManager: %v", err)
	}
	return mm
}

// post adds a message dated `age` before the reference time.
func postAged(t *testing.T, mm *MessageManager, ref time.Time, age time.Duration) {
	t.Helper()
	if _, err := mm.AddMessageWithDate(1, "alice", "All", "subj", "body", "", ref.Add(-age)); err != nil {
		t.Fatal(err)
	}
}

func TestSeedLastRead_RecentAndOldMessages(t *testing.T) {
	mm := newSeedTestManager(t)
	ref := time.Now()
	postAged(t, mm, ref, 30*24*time.Hour) // msg 1: 30d old
	postAged(t, mm, ref, 10*24*time.Hour) // msg 2: 10d old
	postAged(t, mm, ref, 2*24*time.Hour)  // msg 3: 2d old
	postAged(t, mm, ref, 1*time.Hour)     // msg 4: 1h old

	since := ref.AddDate(0, 0, -NewscanSeedDays)
	if err := mm.SeedLastRead(1, "newbie", since); err != nil {
		t.Fatalf("SeedLastRead: %v", err)
	}
	lr, err := mm.GetLastRead(1, "newbie")
	if err != nil {
		t.Fatal(err)
	}
	if lr != 2 {
		t.Errorf("lastread = %d, want 2 (msgs 3-4 unread)", lr)
	}
	unread, err := mm.GetNewMessageCount(1, "newbie")
	if err != nil {
		t.Fatal(err)
	}
	if unread != 2 {
		t.Errorf("unread = %d, want 2", unread)
	}
}

func TestSeedLastRead_QuietAreaAllOld(t *testing.T) {
	mm := newSeedTestManager(t)
	ref := time.Now()
	postAged(t, mm, ref, 60*24*time.Hour)
	postAged(t, mm, ref, 30*24*time.Hour)

	if err := mm.SeedLastRead(1, "newbie", ref.AddDate(0, 0, -NewscanSeedDays)); err != nil {
		t.Fatal(err)
	}
	unread, err := mm.GetNewMessageCount(1, "newbie")
	if err != nil {
		t.Fatal(err)
	}
	if unread != 0 {
		t.Errorf("quiet area unread = %d, want 0", unread)
	}
}

func TestSeedLastRead_AllRecent(t *testing.T) {
	mm := newSeedTestManager(t)
	ref := time.Now()
	postAged(t, mm, ref, 2*24*time.Hour)
	postAged(t, mm, ref, 1*24*time.Hour)

	if err := mm.SeedLastRead(1, "newbie", ref.AddDate(0, 0, -NewscanSeedDays)); err != nil {
		t.Fatal(err)
	}
	unread, err := mm.GetNewMessageCount(1, "newbie")
	if err != nil {
		t.Fatal(err)
	}
	if unread != 2 {
		t.Errorf("unread = %d, want 2 (all messages recent)", unread)
	}
}

func TestSeedLastRead_EmptyAreaWritesNothing(t *testing.T) {
	mm := newSeedTestManager(t)
	if err := mm.SeedLastRead(1, "newbie", time.Now().AddDate(0, 0, -7)); err != nil {
		t.Fatal(err)
	}
	lr, err := mm.GetLastRead(1, "newbie")
	if err != nil {
		t.Fatal(err)
	}
	if lr != 0 {
		t.Errorf("empty area lastread = %d, want 0 (no record)", lr)
	}
}

func TestSeedLastRead_NeverOverwritesExistingPointer(t *testing.T) {
	mm := newSeedTestManager(t)
	ref := time.Now()
	postAged(t, mm, ref, 10*24*time.Hour)
	postAged(t, mm, ref, 1*24*time.Hour)

	if err := mm.SetLastRead(1, "oldtimer", 2); err != nil {
		t.Fatal(err)
	}
	if err := mm.SeedLastRead(1, "oldtimer", ref.AddDate(0, 0, -NewscanSeedDays)); err != nil {
		t.Fatal(err)
	}
	lr, err := mm.GetLastRead(1, "oldtimer")
	if err != nil {
		t.Fatal(err)
	}
	if lr != 2 {
		t.Errorf("existing pointer changed to %d, want 2", lr)
	}
}
