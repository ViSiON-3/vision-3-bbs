package configeditor

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// templatePollPlaceholder identifies the inert seeded poll event from
// templates/configs/events.json by its placeholder hub address.
const templatePollPlaceholder = "21:4/100@fsxnet"

// ftnSupportEventIDs are seeded events the wizard enables (but never creates)
// because they support a healthy echomail flow: the toss safety net and the
// nightly JAM message-base maintenance sequence.
var ftnSupportEventIDs = []string{
	"echomail_toss",
	"example_nightly_msgbase_fix",
	"example_nightly_msgbase_purge",
	"example_nightly_msgbase_pack",
}

// wireFTNEvents makes the event scheduler ready for FTN mail flow after the
// wizard saves a network: it upserts an enabled per-network hub poll event
// (the built-in binkd daemon only calls out when outbound mail is queued, so
// inbound needs a periodic poll), removes the template's placeholder poll,
// enables the supporting seeded events, and turns the scheduler on. An
// existing poll event for the network keeps every user-tuned field
// (schedule, timeout, env vars, chaining); only command, args, name, and
// enabled state are refreshed.
func wireFTNEvents(events *config.EventsConfig, netKey, hubAddress string) {
	pollID := "echomail_poll_" + netKey
	hubFull := fmt.Sprintf("%s@%s", hubAddress, netKey)

	// Drop the inert template placeholder poll event.
	kept := events.Events[:0]
	for _, e := range events.Events {
		if e.ID == "echomail_poll_hub" && containsArg(e.Args, templatePollPlaceholder) {
			continue
		}
		kept = append(kept, e)
	}
	events.Events = kept

	updated := false
	for i := range events.Events {
		if events.Events[i].ID != pollID {
			continue
		}
		e := &events.Events[i]
		e.Name = fmt.Sprintf("Poll Hub (%s)", hubAddress)
		e.Command = "{BBS_ROOT}/bin/binkd"
		e.Args = []string{"-p", "-P", hubFull, "{BBS_ROOT}/data/ftn/binkd.conf"}
		e.Enabled = true
		updated = true
		break
	}
	if !updated {
		events.Events = append(events.Events, newPollEvent(netKey, hubAddress))
	}

	// Enable supporting seeded events where present.
	for i := range events.Events {
		for _, id := range ftnSupportEventIDs {
			if events.Events[i].ID == id {
				events.Events[i].Enabled = true
			}
		}
	}

	events.Enabled = true
	if events.MaxConcurrentEvents <= 0 {
		events.MaxConcurrentEvents = 3
	}
}

// newPollEvent builds the standard enabled hub-poll event for a network.
func newPollEvent(netKey, hubAddress string) config.EventConfig {
	return config.EventConfig{
		ID:               "echomail_poll_" + netKey,
		Name:             fmt.Sprintf("Poll Hub (%s)", hubAddress),
		Schedule:         "*/15 * * * *",
		Command:          "{BBS_ROOT}/bin/binkd",
		Args:             []string{"-p", "-P", fmt.Sprintf("%s@%s", hubAddress, netKey), "{BBS_ROOT}/data/ftn/binkd.conf"},
		WorkingDirectory: "{BBS_ROOT}",
		TimeoutSeconds:   300,
		Enabled:          true,
	}
}

// pollEventPrefix is the ID prefix of per-network hub poll events; the rest
// of the ID is the network key.
const pollEventPrefix = "echomail_poll_"

// refreshPollEvents keeps per-network poll events in step with the networks
// on save: an existing event is retargeted to the first link's current hub
// address, and a network whose first link has a hostname (a real, pollable
// hub — including manually created networks that never went through the
// wizard) gets an enabled poll event created if none exists.
//
// It also reconciles the other direction. A poll event whose network is no
// longer configured, or whose hub has lost its hostname, is disabled rather
// than left running: binkd would fail the poll every 15 minutes, and a sysop
// reading the events list would see an enabled poll for a network that cannot
// be polled. Disabled rather than deleted so a tuned schedule or extra flags
// survive the network coming back. Nothing here re-enables an event, since a
// sysop who switched one off deliberately must not have that undone on every
// save.
func refreshPollEvents(events *config.EventsConfig, networks map[string]config.FTNNetworkConfig) {
	for netKey, nc := range networks {
		if len(nc.Links) == 0 {
			continue
		}
		hub := nc.Links[0].Address
		pollable := nc.Links[0].HostPort() != ""
		found := false
		for i := range events.Events {
			if events.Events[i].ID != pollEventPrefix+netKey {
				continue
			}
			found = true
			e := &events.Events[i]
			e.Name = fmt.Sprintf("Poll Hub (%s)", hub)
			hubFull := fmt.Sprintf("%s@%s", hub, netKey)
			// Retarget only the -P value so sysop-added flags survive; if
			// the args no longer contain -P, rebuild the standard set.
			retargeted := false
			for j := 0; j < len(e.Args)-1; j++ {
				if e.Args[j] == "-P" {
					e.Args[j+1] = hubFull
					retargeted = true
					break
				}
			}
			if !retargeted {
				e.Args = []string{"-p", "-P", hubFull, "{BBS_ROOT}/data/ftn/binkd.conf"}
			}
			if !pollable && e.Enabled {
				e.Enabled = false
				slog.Warn("ftn network's hub no longer has a hostname, so its poll event was disabled — "+
					"binkd has nothing to dial; set the link's Hostname under Echomail Links to poll it again",
					"network", netKey, "hub", hub, "event", e.ID)
			}
		}
		switch {
		case found:
			// Already has a poll event; retargeted above.
		case pollable:
			events.Events = append(events.Events, newPollEvent(netKey, hub))
		default:
			// No hostname means nothing to dial, so no poll event. Warned
			// because the network then looks configured but only ever receives
			// mail when the uplink calls in — or, where it shares an uplink
			// with another network, as a side effect of that network's poll,
			// which is indistinguishable from working until the other link
			// changes.
			slog.Warn("ftn network has no hub hostname, so no poll event was created — "+
				"inbound mail depends entirely on the uplink calling in; "+
				"set the link's Hostname under Echomail Links to poll it",
				"network", netKey, "hub", hub)
		}
	}

	// Poll events for networks that no longer exist: a network deleted in the
	// editor, or one renamed outside it (a rename inside the editor carries
	// the event along, see renamePollEvent).
	for i := range events.Events {
		e := &events.Events[i]
		netKey, ok := strings.CutPrefix(e.ID, pollEventPrefix)
		if !ok || !e.Enabled {
			continue
		}
		if _, exists := networks[netKey]; exists {
			continue
		}
		if netKey == "hub" && containsArg(e.Args, templatePollPlaceholder) {
			continue // the template's inert placeholder, removed by the wizard
		}
		e.Enabled = false
		slog.Warn("poll event names an ftn network that is no longer configured, so it was disabled",
			"event", e.ID, "network", netKey)
	}
}

// renamePollEvent moves a network's poll event from oldKey to newKey: the ID
// and the -P target both embed the key. The rest of the event is untouched;
// refreshPollEvents on save then retargets the hub address as usual.
func renamePollEvent(events *config.EventsConfig, oldKey, newKey string) {
	for i := range events.Events {
		e := &events.Events[i]
		if e.ID != pollEventPrefix+oldKey {
			continue
		}
		e.ID = pollEventPrefix + newKey
		for j := range e.Args {
			if rest, ok := strings.CutSuffix(e.Args[j], "@"+oldKey); ok {
				e.Args[j] = rest + "@" + newKey
			}
		}
	}
}

// containsArg reports whether args contains the exact value s.
func containsArg(args []string, s string) bool {
	for _, a := range args {
		if a == s {
			return true
		}
	}
	return false
}
