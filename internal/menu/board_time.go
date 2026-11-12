package menu

import "time"

// boardLocation is the board's time zone (config.json's timezone), resolved
// when the config is set; the host's until then.
func boardLocation() *time.Location {
	if loc := acsLocation.Load(); loc != nil {
		return loc
	}
	return time.Local
}

// boardTime is t in the board's time zone, as the screens' clocks show it.
// Times are stored as instants and read back in the host's zone, so on a
// board set to another zone than its host's they would show the wrong hour,
// or day. A zero time is returned as it is.
func boardTime(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return t.In(boardLocation())
}
