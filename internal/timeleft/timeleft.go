// Package timeleft computes a caller's remaining time. Every place that
// shows or hands out time left goes through it, so sysop chat credit is
// applied the same way everywhere.
package timeleft

import "time"

// Remaining returns the time left of a limitMin-minute limit for a session
// that began at start, as of now. Chat time, credit, is not charged to the
// caller. It never goes below zero. limited is false when limitMin <= 0.
func Remaining(limitMin int, start, now time.Time, credit time.Duration) (left time.Duration, limited bool) {
	if limitMin <= 0 {
		return 0, false
	}
	left = time.Duration(limitMin)*time.Minute - (now.Sub(start) - credit)
	if left < 0 {
		left = 0
	}
	return left, true
}

// Minutes is Remaining in whole minutes, counting elapsed time down in whole
// minutes first, as the dropfiles and the WFC column always have.
func Minutes(limitMin int, start, now time.Time, credit time.Duration) (mins int, limited bool) {
	if limitMin <= 0 {
		return 0, false
	}
	mins = limitMin - int((now.Sub(start) - credit).Minutes())
	if mins < 0 {
		mins = 0
	}
	return mins, true
}
