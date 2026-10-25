package scheduler

import (
	"time"
)

// EventResult captures the outcome of an event execution
type EventResult struct {
	EventID     string
	StartTime   time.Time
	EndTime     time.Time
	Success     bool
	ExitCode    int
	Output      string
	ErrorOutput string
	Error       error
}

// EventHistory tracks historical execution data for an event
type EventHistory struct {
	EventID      string    `json:"event_id"`
	LastRun      time.Time `json:"last_run"`
	LastStatus   string    `json:"last_status"` // "success", "failure", "timeout"
	LastDuration int64     `json:"last_duration_ms"`
	RunCount     int       `json:"run_count"`
	SuccessCount int       `json:"success_count"`
	FailureCount int       `json:"failure_count"`
	// SkipCount counts firings that did not run: the event was already
	// running, or no concurrency slot came free within its timeout.
	SkipCount      int       `json:"skip_count,omitempty"`
	LastSkipped    time.Time `json:"last_skipped,omitzero"`
	LastSkipReason string    `json:"last_skip_reason,omitempty"`
}
