package scheduler

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func TestStatusReportsScheduleHistoryAndNextRun(t *testing.T) {
	cfg := config.EventsConfig{Events: []config.EventConfig{
		{ID: "nightly", Name: "Nightly toss", Schedule: "0 3 * * *", Enabled: true, Command: "true"},
		{ID: "off", Name: "Disabled", Schedule: "* * * * *", Enabled: false, Command: "true"},
		{ID: "boot", Name: "Startup only", RunAtStartup: true, Enabled: true, Command: "true"},
	}}
	s := NewScheduler(cfg, filepath.Join(t.TempDir(), "history.json"))
	s.history["nightly"] = &EventHistory{EventID: "nightly", LastRun: time.Date(2026, 9, 15, 3, 0, 0, 0, time.UTC), LastStatus: "success", LastDuration: 1200, RunCount: 4, FailureCount: 1}
	s.runningEvents["boot"] = true

	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

	// Before Start there is no cron, so no next-run times are predicted.
	if st := s.Status(now); len(st) != 3 || !st[0].NextRun.IsZero() {
		t.Fatalf("pre-start status = %+v", st)
	}

	s.Reload(cfg) // installs a cron without running startup events
	defer s.Stop()
	st := s.Status(now)
	if len(st) != 3 {
		t.Fatalf("got %d statuses, want 3", len(st))
	}
	n := st[0]
	if n.ID != "nightly" || n.LastStatus != "success" || n.RunCount != 4 || n.FailureCount != 1 || n.LastDurationMs != 1200 {
		t.Errorf("nightly history not carried: %+v", n)
	}
	if want := time.Date(2026, 9, 17, 3, 0, 0, 0, time.UTC); !n.NextRun.Equal(want) {
		t.Errorf("nightly next = %v, want %v", n.NextRun, want)
	}
	if !st[1].NextRun.IsZero() || st[1].Enabled {
		t.Errorf("disabled event must have no next run: %+v", st[1])
	}
	if !st[2].NextRun.IsZero() || !st[2].RunAtStartup || !st[2].Running {
		t.Errorf("startup event: %+v", st[2])
	}
}
