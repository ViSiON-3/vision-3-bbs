package configeditor

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// templateEvents mirrors the relevant seeded events from
// templates/configs/events.json.
func templateEvents() config.EventsConfig {
	return config.EventsConfig{
		Enabled:             false,
		MaxConcurrentEvents: 3,
		Events: []config.EventConfig{
			{ID: "echomail_poll_hub", Name: "Poll Hub (21:4/100)",
				Schedule: "*/15 * * * *", Command: "{BBS_ROOT}/bin/binkd",
				Args: []string{"-p", "-P", "21:4/100@fsxnet", "{BBS_ROOT}/data/ftn/binkd.conf"}},
			{ID: "echomail_toss", Name: "Toss Echomail", Schedule: "1,16,31,46 * * * *",
				Command: "{BBS_ROOT}/v3mail"},
			{ID: "example_nightly_msgbase_fix", Schedule: "0 2 * * *", Command: "{BBS_ROOT}/v3mail"},
			{ID: "example_nightly_msgbase_purge", Schedule: "15 2 * * *", Command: "{BBS_ROOT}/v3mail"},
			{ID: "example_nightly_msgbase_pack", Schedule: "30 2 * * *", Command: "{BBS_ROOT}/v3mail"},
		},
	}
}

func findEvent(ev config.EventsConfig, id string) *config.EventConfig {
	for i := range ev.Events {
		if ev.Events[i].ID == id {
			return &ev.Events[i]
		}
	}
	return nil
}

func TestWireFTNEventsCreatesEnabledPollForHub(t *testing.T) {
	ev := templateEvents()
	wireFTNEvents(&ev, "fsxnet", "21:1/100")

	if !ev.Enabled {
		t.Error("scheduler must be enabled so the poll actually runs")
	}
	poll := findEvent(ev, "echomail_poll_fsxnet")
	if poll == nil {
		t.Fatal("expected per-network poll event echomail_poll_fsxnet")
	}
	if !poll.Enabled {
		t.Error("poll event must be enabled")
	}
	want := []string{"-p", "-P", "21:1/100@fsxnet", "{BBS_ROOT}/data/ftn/binkd.conf"}
	if len(poll.Args) != len(want) {
		t.Fatalf("poll args = %v, want %v", poll.Args, want)
	}
	for i := range want {
		if poll.Args[i] != want[i] {
			t.Fatalf("poll args = %v, want %v", poll.Args, want)
		}
	}
	// The inert seeded placeholder poll must be gone.
	if findEvent(ev, "echomail_poll_hub") != nil {
		t.Error("template placeholder poll event must be removed")
	}
}

func TestWireFTNEventsEnablesTossAndNightlyMaintenance(t *testing.T) {
	ev := templateEvents()
	wireFTNEvents(&ev, "fsxnet", "21:1/100")

	for _, id := range []string{
		"echomail_toss",
		"example_nightly_msgbase_fix",
		"example_nightly_msgbase_purge",
		"example_nightly_msgbase_pack",
	} {
		e := findEvent(ev, id)
		if e == nil {
			t.Fatalf("event %s missing", id)
		}
		if !e.Enabled {
			t.Errorf("event %s must be enabled", id)
		}
	}
}

func TestWireFTNEventsIdempotentAndPreservesUserSchedule(t *testing.T) {
	ev := templateEvents()
	wireFTNEvents(&ev, "fsxnet", "21:1/100")

	// Sysop tweaks the poll cadence, then re-runs a wizard (another network
	// or re-save): their schedule must survive, and no duplicate appears.
	findEvent(ev, "echomail_poll_fsxnet").Schedule = "*/30 * * * *"
	wireFTNEvents(&ev, "fsxnet", "21:1/100")

	count := 0
	for _, e := range ev.Events {
		if e.ID == "echomail_poll_fsxnet" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("want exactly 1 poll event, got %d", count)
	}
	if got := findEvent(ev, "echomail_poll_fsxnet").Schedule; got != "*/30 * * * *" {
		t.Errorf("user schedule overwritten: %q", got)
	}
}

func TestWireFTNEventsMissingOptionalEventsNotCreated(t *testing.T) {
	// A trimmed events.json without the toss/maintenance entries: the wizard
	// must not invent them, only the poll event.
	ev := config.EventsConfig{}
	wireFTNEvents(&ev, "fsxnet", "21:1/100")

	if len(ev.Events) != 1 {
		t.Fatalf("want only the poll event, got %d events", len(ev.Events))
	}
	if ev.Events[0].ID != "echomail_poll_fsxnet" {
		t.Fatalf("unexpected event %q", ev.Events[0].ID)
	}
	if !ev.Enabled {
		t.Error("scheduler must be enabled")
	}
}
