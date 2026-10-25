package main

import (
	"log/slog"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// warnUnenforcedPurgeLimits logs when message areas set max_messages or
// max_age but no enabled event runs "v3mail purge", the only thing that
// enforces them. Without it the areas grow without bound while their
// settings say otherwise (#691).
func warnUnenforcedPurgeLimits(events config.EventsConfig, areas []*message.MessageArea) {
	if events.RunsMessagePurge() {
		return
	}
	if n := countPurgeLimitedAreas(areas); n > 0 {
		slog.Warn("message areas have purge limits but no enabled event runs 'v3mail purge', so the limits are not enforced; enable the nightly purge event in events.json",
			"areas", n)
	}
}

// countPurgeLimitedAreas returns how many areas set max_messages or max_age.
func countPurgeLimitedAreas(areas []*message.MessageArea) int {
	n := 0
	for _, a := range areas {
		if a.MaxMessages > 0 || a.MaxAge > 0 {
			n++
		}
	}
	return n
}
