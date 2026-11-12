package menu

import "time"

// boardTime is t in the board's time zone (config.json's timezone), as the
// screens' clocks show it. Message dates are read from the base in the host's
// zone, so on a board set to another zone than its host's they would show the
// wrong hour, or day. Until a config is set, and for a zero time, t is
// returned as it is.
func boardTime(t time.Time) time.Time {
	if loc := acsLocation.Load(); loc != nil && !t.IsZero() {
		return t.In(loc)
	}
	return t
}
