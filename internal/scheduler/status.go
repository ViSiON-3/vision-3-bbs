package scheduler

import (
	"time"

	"github.com/robfig/cron/v3"
)

// EventStatus is a read-only view of one configured event for monitoring
// (the WFC console's Events tab).
type EventStatus struct {
	ID             string
	Name           string
	Schedule       string // cron spec; empty for startup-only events
	Enabled        bool
	RunAtStartup   bool
	Running        bool
	NextRun        time.Time // zero when the event is disabled or has no schedule
	LastRun        time.Time
	LastStatus     string // "success", "failure", "timeout", or "" if never run
	LastDurationMs int64
	RunCount       int
	FailureCount   int
}

// Status reports every configured event with its schedule, run history, and
// next fire time as of now. Events are returned in configuration order.
func (s *Scheduler) Status(now time.Time) []EventStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]EventStatus, 0, len(s.config.Events))
	for _, e := range s.config.Events {
		st := EventStatus{
			ID:           e.ID,
			Name:         e.Name,
			Schedule:     e.Schedule,
			Enabled:      e.Enabled,
			RunAtStartup: e.RunAtStartup,
			Running:      s.runningEvents[e.ID],
		}
		if h := s.history[e.ID]; h != nil {
			st.LastRun = h.LastRun
			st.LastStatus = h.LastStatus
			st.LastDurationMs = h.LastDuration
			st.RunCount = h.RunCount
			st.FailureCount = h.FailureCount
		}
		// The cron entries do not carry the event ID, so derive the next
		// fire time from the spec with the same parser cron.New() uses.
		if e.Enabled && e.Schedule != "" && s.cron != nil {
			if sched, err := cron.ParseStandard(e.Schedule); err == nil {
				st.NextRun = sched.Next(now)
			}
		}
		out = append(out, st)
	}
	return out
}
