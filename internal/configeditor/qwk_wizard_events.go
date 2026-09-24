package configeditor

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// qwkPollEventPrefix is the ID prefix of per-network QWK poll events; the
// rest of the ID is the network key.
const qwkPollEventPrefix = "qwknet_poll_"

// defaultQWKPollSchedule polls twice an hour, which is what most QWK hubs
// suggest for a node.
const defaultQWKPollSchedule = "*/30 * * * *"

// newQWKPollEvent builds the enabled poll event for a network: one v3mail
// run that packs, uploads, downloads and tosses.
func newQWKPollEvent(netKey, hubID, schedule string) config.EventConfig {
	if strings.TrimSpace(schedule) == "" {
		schedule = defaultQWKPollSchedule
	}
	return config.EventConfig{
		ID:               qwkPollEventPrefix + netKey,
		Name:             fmt.Sprintf("Poll QWK Hub (%s)", hubID),
		Schedule:         schedule,
		Command:          "{BBS_ROOT}/v3mail",
		Args:             []string{"qwk-poll", "--network", netKey, "--config", "{BBS_ROOT}/configs", "--data", "{BBS_ROOT}/data"},
		WorkingDirectory: "{BBS_ROOT}",
		TimeoutSeconds:   600,
		Enabled:          true,
	}
}

// wireQWKEvents upserts the network's poll event after the wizard saves. A
// non-blank schedule is applied to an existing event too, since the wizard
// pre-fills the field from that event and what comes back is the sysop's
// current choice; blank keeps whatever is there. Extra arguments a sysop
// added survive either way.
//
// Only adding a network switches polling on (the event and the scheduler).
// Editing one, to add conferences say, leaves an existing event's Enabled
// alone: a sysop may have paused it, and nothing here should undo that. An
// event that is missing is created enabled only when the network is.
func wireQWKEvents(events *config.EventsConfig, netKey, hubID, schedule string, adding, netEnabled bool) {
	id := qwkPollEventPrefix + netKey
	for i := range events.Events {
		if events.Events[i].ID != id {
			continue
		}
		e := &events.Events[i]
		e.Name = fmt.Sprintf("Poll QWK Hub (%s)", hubID)
		e.Command = "{BBS_ROOT}/v3mail"
		e.Args = retargetQWKArgs(e.Args, netKey)
		if s := strings.TrimSpace(schedule); s != "" {
			e.Schedule = s
		}
		if adding {
			e.Enabled = true
			turnOnScheduler(events)
		}
		return
	}
	ev := newQWKPollEvent(netKey, hubID, schedule)
	ev.Enabled = netEnabled
	events.Events = append(events.Events, ev)
	if netEnabled {
		turnOnScheduler(events)
	}
}

// turnOnScheduler enables the event scheduler so a new poll event runs.
func turnOnScheduler(events *config.EventsConfig) {
	events.Enabled = true
	if events.MaxConcurrentEvents <= 0 {
		events.MaxConcurrentEvents = 3
	}
}

// retargetQWKArgs points the --network value at netKey, rebuilding the
// standard argument set when it no longer has one.
func retargetQWKArgs(args []string, netKey string) []string {
	for j := 0; j < len(args)-1; j++ {
		if args[j] == "--network" {
			args[j+1] = netKey
			return args
		}
	}
	return []string{"qwk-poll", "--network", netKey, "--config", "{BBS_ROOT}/configs", "--data", "{BBS_ROOT}/data"}
}

// refreshQWKPollEvents keeps poll events in step with qwknet.json on save.
// An enabled network without an event gets one; an event whose network is
// gone, or disabled, is switched off rather than deleted so a tuned
// schedule survives the network coming back. Nothing here re-enables an
// event a sysop switched off.
func refreshQWKPollEvents(events *config.EventsConfig, networks map[string]config.QWKNetworkConfig) {
	for netKey, nc := range networks {
		id := qwkPollEventPrefix + netKey
		found := false
		for i := range events.Events {
			if events.Events[i].ID != id {
				continue
			}
			found = true
			e := &events.Events[i]
			e.Name = fmt.Sprintf("Poll QWK Hub (%s)", config.NormalizeQWKID(nc.HubID))
			e.Args = retargetQWKArgs(e.Args, netKey)
			if !nc.Enabled && e.Enabled {
				e.Enabled = false
				slog.Info("qwk network is disabled, so its poll event was disabled too", "network", netKey, "event", id)
			}
		}
		if !found && nc.Enabled {
			events.Events = append(events.Events, newQWKPollEvent(netKey, config.NormalizeQWKID(nc.HubID), ""))
		}
	}
	for i := range events.Events {
		e := &events.Events[i]
		netKey, ok := strings.CutPrefix(e.ID, qwkPollEventPrefix)
		if !ok || !e.Enabled {
			continue
		}
		if _, exists := networks[netKey]; exists {
			continue
		}
		e.Enabled = false
		slog.Warn("poll event names a QWK network that is no longer configured, so it was disabled",
			"event", e.ID, "network", netKey)
	}
}

// renameQWKPollEvent carries a network's poll event along with a key
// rename. A leftover event already under the new key is dropped first.
func renameQWKPollEvent(events *config.EventsConfig, oldKey, newKey string) {
	kept := events.Events[:0]
	hasOld := false
	for _, e := range events.Events {
		if e.ID == qwkPollEventPrefix+oldKey {
			hasOld = true
		}
	}
	for _, e := range events.Events {
		if hasOld && e.ID == qwkPollEventPrefix+newKey {
			continue
		}
		kept = append(kept, e)
	}
	events.Events = kept
	for i := range events.Events {
		e := &events.Events[i]
		if e.ID != qwkPollEventPrefix+oldKey {
			continue
		}
		e.ID = qwkPollEventPrefix + newKey
		e.Args = retargetQWKArgs(e.Args, newKey)
	}
}
