package configeditor

import (
	"fmt"

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
		events.Events = append(events.Events, config.EventConfig{
			ID:               pollID,
			Name:             fmt.Sprintf("Poll Hub (%s)", hubAddress),
			Schedule:         "*/15 * * * *",
			Command:          "{BBS_ROOT}/bin/binkd",
			Args:             []string{"-p", "-P", hubFull, "{BBS_ROOT}/data/ftn/binkd.conf"},
			WorkingDirectory: "{BBS_ROOT}",
			TimeoutSeconds:   300,
			Enabled:          true,
		})
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

// refreshPollEvents updates each existing per-network poll event to point at
// the network's current first-link hub address, so a hub change in the
// Echomail Links editor propagates on save. It never creates events — only
// the wizard does that.
func refreshPollEvents(events *config.EventsConfig, networks map[string]config.FTNNetworkConfig) {
	for netKey, nc := range networks {
		if len(nc.Links) == 0 {
			continue
		}
		hub := nc.Links[0].Address
		for i := range events.Events {
			if events.Events[i].ID != "echomail_poll_"+netKey {
				continue
			}
			e := &events.Events[i]
			e.Name = fmt.Sprintf("Poll Hub (%s)", hub)
			e.Args = []string{"-p", "-P", fmt.Sprintf("%s@%s", hub, netKey), "{BBS_ROOT}/data/ftn/binkd.conf"}
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
