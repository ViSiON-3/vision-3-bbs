package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// Chained events: run_after names the event this one follows, and
// delay_after_seconds how long to wait once it finishes.
//
// Both fields shipped in EventConfig and in the events editor, and nothing read
// either of them. A sysop could set "Run After" in the TUI, see it saved, and
// get no chaining — while an event with only a run_after and no schedule was
// dropped at startup with "event has no schedule and run_at_startup is false".
//
// Chaining fires on completion regardless of the parent's exit status. "Run
// after" says when, not whether: a poll that failed is exactly when the next
// network still wants its turn, and making success a precondition would
// silently strand a whole chain behind one bad exit. An event that must not run
// after a failure is a different feature and should say so.

// maxChainDepth bounds how far one trigger can cascade. A cycle is rejected up
// front, so this only catches a chain long enough to look like a mistake, and
// bounds the goroutines one cron tick can spawn.
const maxChainDepth = 16

// chainCycle reports the first cycle reachable from the run_after links in
// events, as a human-readable path, or "" when the graph is acyclic.
//
// Rejecting a cycle is not optional: A after B after A would re-trigger
// forever, each hop spawning a goroutine, until the concurrency limit turned it
// into a flood of "event skipped" warnings.
//
// Only enabled events take part, matching chainedAfter: a disabled event is
// never chained, so a loop that runs through one cannot re-trigger, and
// counting it would switch chaining off for every valid chain on the board
// because of an event the sysop has already turned off.
func chainCycle(events []config.EventConfig) string {
	parent := make(map[string]string, len(events))
	known := make(map[string]bool, len(events))
	for _, e := range events {
		if !e.Enabled {
			continue
		}
		known[e.ID] = true
		if e.RunAfter != "" {
			parent[e.ID] = e.RunAfter
		}
	}

	// Walk each event's ancestry; a revisit within one walk is a cycle.
	for _, e := range events {
		if !e.Enabled {
			continue
		}
		seen := map[string]bool{e.ID: true}
		path := []string{e.ID}
		for id := e.ID; ; {
			next, ok := parent[id]
			if !ok || !known[next] {
				break // chain root, or a dangling reference (reported elsewhere)
			}
			path = append(path, next)
			if seen[next] {
				return joinPath(path)
			}
			seen[next] = true
			id = next
		}
	}
	return ""
}

// joinPath renders a chain as "a -> b -> a".
func joinPath(path []string) string {
	out := ""
	for i, p := range path {
		if i > 0 {
			out += " -> "
		}
		out += p
	}
	return out
}

// validateChains logs the run_after problems worth a sysop's attention and
// reports whether chaining is safe to run. A cycle disables chaining entirely
// rather than half of it, so the scheduler's behaviour matches the warning.
func validateChains(events []config.EventConfig) bool {
	known := make(map[string]bool, len(events))
	for _, e := range events {
		known[e.ID] = true
	}
	for _, e := range events {
		if e.RunAfter == "" {
			continue
		}
		switch {
		case e.RunAfter == e.ID:
			slog.Warn("event run_after names itself; it will never be chained",
				"id", e.ID, "name", e.Name)
		case !known[e.RunAfter]:
			slog.Warn("event run_after names an event that does not exist; it will never be chained",
				"id", e.ID, "name", e.Name, "run_after", e.RunAfter)
		}
	}
	if cycle := chainCycle(events); cycle != "" {
		slog.Error("run_after chain contains a cycle, so chaining is disabled for every event — "+
			"break the loop in the events editor", "cycle", cycle)
		return false
	}
	return true
}

// chainedAfter returns the enabled events that follow parentID, in config
// order. A disabled event is not chained, matching how it is not scheduled.
func chainedAfter(events []config.EventConfig, parentID string) []config.EventConfig {
	var next []config.EventConfig
	for _, e := range events {
		if e.Enabled && e.RunAfter == parentID && e.ID != parentID {
			next = append(next, e)
		}
	}
	return next
}

// runChainedEvents launches everything chained to parentID, after each one's
// delay_after_seconds. Called once the parent has finished and its history is
// recorded, so a chained event that inspects the parent's state sees the
// completed run.
//
// depth guards against a chain long enough to look accidental; a cycle is
// already refused by validateChains before any of this runs.
func (s *Scheduler) runChainedEvents(ctx context.Context, parentID string, depth int) {
	s.mu.RLock()
	events := s.config.Events
	enabled := s.chainingOK
	s.mu.RUnlock()

	if !enabled || ctx.Err() != nil {
		// A parent finishing after shutdown began (a job from a cron Reload
		// retired, say) must not add to chainWg once Stop has drained it.
		return
	}
	next := chainedAfter(events, parentID)
	if len(next) == 0 {
		return
	}
	if depth >= maxChainDepth {
		ids := make([]string, 0, len(next))
		for _, e := range next {
			ids = append(ids, e.ID)
		}
		slog.Error("run_after chain is too deep; not following it further",
			"after", parentID, "depth", depth, "max_depth", maxChainDepth, "skipped", ids)
		return
	}

	for _, e := range next {
		child := e
		s.chainWg.Add(1)
		go func() {
			defer s.chainWg.Done()
			if d := time.Duration(child.DelayAfterSeconds) * time.Second; d > 0 {
				slog.Info("chained event waiting", "id", child.ID, "after", parentID, "delay", d)
				select {
				case <-ctx.Done():
					return
				case <-time.After(d):
				}
			}
			if ctx.Err() != nil {
				return
			}
			slog.Info("chained event launching", "id", child.ID, "name", child.Name, "after", parentID)
			s.executeChain(child, depth+1)
		}()
	}
}

// chainDescription renders an event's chaining for a startup log line.
func chainDescription(e config.EventConfig) string {
	if e.DelayAfterSeconds > 0 {
		return fmt.Sprintf("%s +%ds", e.RunAfter, e.DelayAfterSeconds)
	}
	return e.RunAfter
}
