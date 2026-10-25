package scheduler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	configtemplates "github.com/ViSiON-3/vision-3-bbs/templates/configs"
)

// TestTemplateNightlyMaintenanceIsChained pins #695: the template's nightly
// fix fires on a minute no other scheduled template event fires on, and
// purge and pack follow it in order with no schedule of their own, so the
// sequence does not compete with the hub polls and the scan for slots.
func TestTemplateNightlyMaintenanceIsChained(t *testing.T) {
	data, err := configtemplates.FS.ReadFile("events.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg config.EventsConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	byID := map[string]config.EventConfig{}
	for _, e := range cfg.Events {
		byID[e.ID] = e
	}

	fix, purge, pack := byID["example_nightly_msgbase_fix"], byID["example_nightly_msgbase_purge"], byID["example_nightly_msgbase_pack"]
	if purge.RunAfter != fix.ID || purge.Schedule != "" {
		t.Errorf("purge: run_after %q schedule %q, want after %q with no schedule", purge.RunAfter, purge.Schedule, fix.ID)
	}
	if pack.RunAfter != purge.ID || pack.Schedule != "" {
		t.Errorf("pack: run_after %q schedule %q, want after %q with no schedule", pack.RunAfter, pack.Schedule, purge.ID)
	}

	fixSched, err := cron.ParseStandard(fix.Schedule)
	if err != nil {
		t.Fatalf("fix schedule %q: %v", fix.Schedule, err)
	}
	day := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	at := fixSched.Next(day)
	for _, e := range cfg.Events {
		// The every-minute demo fires on every minute by design.
		if e.ID == fix.ID || e.Schedule == "" || e.Schedule == "* * * * *" {
			continue
		}
		sched, err := cron.ParseStandard(e.Schedule)
		if err != nil {
			t.Fatalf("%s schedule %q: %v", e.ID, e.Schedule, err)
		}
		if sched.Next(at.Add(-time.Minute)).Equal(at) {
			t.Errorf("%s (%s) fires at %s, the same minute as the nightly fix", e.ID, e.Schedule, at.Format("15:04"))
		}
	}
}
