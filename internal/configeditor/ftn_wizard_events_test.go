package configeditor

import (
	"bytes"
	"log/slog"
	"strings"
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

func TestWireFTNEventsPreservesUserTunedPollFields(t *testing.T) {
	ev := templateEvents()
	wireFTNEvents(&ev, "fsxnet", "21:1/100")

	// Sysop tunes timeout and adds an env var; a re-run must keep both while
	// still refreshing the hub args.
	poll := findEvent(ev, "echomail_poll_fsxnet")
	poll.TimeoutSeconds = 900
	poll.EnvironmentVars = map[string]string{"BINKD_OPT": "x"}
	wireFTNEvents(&ev, "fsxnet", "21:9/999")

	poll = findEvent(ev, "echomail_poll_fsxnet")
	if poll.TimeoutSeconds != 900 {
		t.Errorf("user timeout overwritten: %d", poll.TimeoutSeconds)
	}
	if poll.EnvironmentVars["BINKD_OPT"] != "x" {
		t.Errorf("user env vars dropped: %v", poll.EnvironmentVars)
	}
	if !containsArg(poll.Args, "21:9/999@fsxnet") {
		t.Errorf("hub arg not refreshed: %v", poll.Args)
	}
}

func TestRefreshPollEventsFollowsHubChange(t *testing.T) {
	// Hub change scenario: the sysop edits the fsxnet link's address in the
	// TUI; on save the existing poll event must follow, but no event is
	// created for networks without one.
	ev := templateEvents()
	wireFTNEvents(&ev, "fsxnet", "21:4/100")

	nets := map[string]config.FTNNetworkConfig{
		"fsxnet":   {Links: []config.FTNLinkConfig{{Address: "21:4/158"}}},
		"othernet": {Links: []config.FTNLinkConfig{{Address: "1:2/3"}}},
	}
	refreshPollEvents(&ev, nets)

	poll := findEvent(ev, "echomail_poll_fsxnet")
	if poll == nil {
		t.Fatal("poll event missing")
	}
	if !containsArg(poll.Args, "21:4/158@fsxnet") {
		t.Errorf("poll -P arg not refreshed: %v", poll.Args)
	}
	if poll.Name != "Poll Hub (21:4/158)" {
		t.Errorf("poll name not refreshed: %q", poll.Name)
	}
	if findEvent(ev, "echomail_poll_othernet") != nil {
		t.Error("refresh must not create events for networks without one")
	}
}

func TestRefreshPollEventsPreservesCustomArgs(t *testing.T) {
	// A sysop may add extra binkd flags to the poll event; refresh must only
	// retarget the -P value (and name), not clobber the rest of the args.
	ev := templateEvents()
	wireFTNEvents(&ev, "fsxnet", "21:4/100")
	poll := findEvent(ev, "echomail_poll_fsxnet")
	poll.Args = []string{"-p", "-q", "-P", "21:4/100@fsxnet", "{BBS_ROOT}/data/ftn/binkd.conf"}

	nets := map[string]config.FTNNetworkConfig{
		"fsxnet": {Links: []config.FTNLinkConfig{{Address: "21:4/158"}}},
	}
	refreshPollEvents(&ev, nets)

	poll = findEvent(ev, "echomail_poll_fsxnet")
	want := []string{"-p", "-q", "-P", "21:4/158@fsxnet", "{BBS_ROOT}/data/ftn/binkd.conf"}
	if len(poll.Args) != len(want) {
		t.Fatalf("args = %v, want %v", poll.Args, want)
	}
	for i := range want {
		if poll.Args[i] != want[i] {
			t.Fatalf("args = %v, want %v", poll.Args, want)
		}
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

// A network whose link carries no hostname gets no poll event — there is
// nothing to dial. It must also be warned about rather than skipped in
// silence: such a network looks configured but only receives mail when the
// uplink calls in, or (sharing an uplink with another network) as a side
// effect of that network's poll, which is indistinguishable from working
// until the other link changes.
func TestRefreshPollEventsSkipsLinkWithoutHostname(t *testing.T) {
	var logged bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(prev)

	ev := templateEvents()
	nets := map[string]config.FTNNetworkConfig{
		"tqwnet": {Links: []config.FTNLinkConfig{{Address: "1337:3/123"}}},
	}
	refreshPollEvents(&ev, nets)

	if findEvent(ev, "echomail_poll_tqwnet") != nil {
		t.Error("a link with no hostname has nothing to dial, so no poll event should be created")
	}
	out := logged.String()
	if !strings.Contains(out, "no hub hostname") {
		t.Errorf("skipping a hostname-less network must warn, got: %q", out)
	}
	if !strings.Contains(out, "tqwnet") {
		t.Errorf("warning must name the network, got: %q", out)
	}
}

// The counterpart: a hostname makes the network pollable, so the event is
// created and nothing is warned about.
func TestRefreshPollEventsCreatesForLinkWithHostname(t *testing.T) {
	var logged bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(prev)

	ev := templateEvents()
	nets := map[string]config.FTNNetworkConfig{
		"tqwnet": {Links: []config.FTNLinkConfig{
			{Address: "1337:3/123", Hostname: "get-ghosted.com", Port: 24555},
		}},
	}
	refreshPollEvents(&ev, nets)

	poll := findEvent(ev, "echomail_poll_tqwnet")
	if poll == nil {
		t.Fatal("a link with a hostname must get a poll event")
	}
	if !containsArg(poll.Args, "1337:3/123@tqwnet") {
		t.Errorf("poll must target the hub: %v", poll.Args)
	}
	if strings.Contains(logged.String(), "no hub hostname") {
		t.Errorf("a pollable network must not warn, got: %q", logged.String())
	}
}

// A network deleted in the editor left its poll event enabled: binkd failed
// the poll every 15 minutes with "unknown domain", and the events list showed
// a live poll for a network that no longer existed.
func TestRefreshPollEventsDisablesEventForRemovedNetwork(t *testing.T) {
	ev := templateEvents()
	wireFTNEvents(&ev, "fsxnet", "21:4/100")
	wireFTNEvents(&ev, "tqwnet", "1337:3/123")

	nets := map[string]config.FTNNetworkConfig{
		"fsxnet": {Links: []config.FTNLinkConfig{{Address: "21:4/100", Hostname: "hub.example.org"}}},
	}
	refreshPollEvents(&ev, nets)

	stale := findEvent(ev, "echomail_poll_tqwnet")
	if stale == nil {
		t.Fatal("the stale event must be disabled, not deleted, so a tuned schedule survives")
	}
	if stale.Enabled {
		t.Error("poll event for a removed network must be disabled")
	}
	if live := findEvent(ev, "echomail_poll_fsxnet"); live == nil || !live.Enabled {
		t.Error("the remaining network's poll must be left enabled")
	}
}

// A hub that loses its hostname can no longer be dialled; its poll event is
// switched off rather than left failing, and nothing switches it back on
// behind the sysop's back — a hostname restored later re-enables nothing.
func TestRefreshPollEventsDisablesEventWhenHostnameIsRemoved(t *testing.T) {
	ev := templateEvents()
	wireFTNEvents(&ev, "tqwnet", "1337:3/123")

	nets := map[string]config.FTNNetworkConfig{
		"tqwnet": {Links: []config.FTNLinkConfig{{Address: "1337:3/123"}}}, // no hostname
	}
	refreshPollEvents(&ev, nets)

	poll := findEvent(ev, "echomail_poll_tqwnet")
	if poll == nil {
		t.Fatal("event must be kept")
	}
	if poll.Enabled {
		t.Error("poll event must be disabled once its hub has no hostname")
	}

	nets["tqwnet"] = config.FTNNetworkConfig{Links: []config.FTNLinkConfig{
		{Address: "1337:3/123", Hostname: "get-ghosted.com"},
	}}
	refreshPollEvents(&ev, nets)
	if findEvent(ev, "echomail_poll_tqwnet").Enabled {
		t.Error("refresh must not re-enable an event; that is the sysop's call")
	}
}

// Renaming a network in the editor carries its poll event along: the ID and
// the -P target both embed the key, and leaving them was one event polling a
// domain binkd no longer knew plus a fresh duplicate under the new key.
func TestRenamePollEventFollowsNetworkKey(t *testing.T) {
	ev := templateEvents()
	wireFTNEvents(&ev, "fsxnet", "21:4/100")
	poll := findEvent(ev, "echomail_poll_fsxnet")
	poll.Schedule = "*/5 * * * *" // sysop-tuned; must survive

	renamePollEvent(&ev, "fsxnet", "fsx")

	if findEvent(ev, "echomail_poll_fsxnet") != nil {
		t.Error("event must not remain under the old key")
	}
	moved := findEvent(ev, "echomail_poll_fsx")
	if moved == nil {
		t.Fatal("event missing under the new key")
	}
	if !containsArg(moved.Args, "21:4/100@fsx") {
		t.Errorf("-P target must follow the key: %v", moved.Args)
	}
	if moved.Schedule != "*/5 * * * *" {
		t.Errorf("schedule not preserved: %q", moved.Schedule)
	}

	// And a save afterwards sees one event for the renamed network.
	nets := map[string]config.FTNNetworkConfig{
		"fsx": {Links: []config.FTNLinkConfig{{Address: "21:4/100", Hostname: "hub.example.org"}}},
	}
	refreshPollEvents(&ev, nets)
	count := 0
	for _, e := range ev.Events {
		if strings.HasPrefix(e.ID, "echomail_poll_") && e.Enabled {
			count++
		}
	}
	if count != 1 {
		t.Errorf("want exactly one enabled poll event after rename+save, got %d", count)
	}
}
