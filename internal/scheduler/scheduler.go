package scheduler

import (
	"context"
	"log/slog"
	"sync"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/robfig/cron/v3"
)

// Scheduler manages scheduled event execution
type Scheduler struct {
	config         config.EventsConfig
	cron           *cron.Cron
	history        map[string]*EventHistory
	historyPath    string
	runningEvents  map[string]bool
	mu             sync.RWMutex
	concurrencySem chan struct{}
	startupWg      sync.WaitGroup
	ctx            context.Context
	cancel         context.CancelFunc
}

// NewScheduler creates a new event scheduler
func NewScheduler(cfg config.EventsConfig, historyPath string) *Scheduler {
	// Set default max concurrent events if not specified
	if cfg.MaxConcurrentEvents <= 0 {
		cfg.MaxConcurrentEvents = 3
	}

	// Load history
	history, err := LoadHistory(historyPath)
	if err != nil {
		slog.Warn("failed to load event history", "path", historyPath, "error", err)
		history = make(map[string]*EventHistory)
	}

	return &Scheduler{
		config:         cfg,
		history:        history,
		historyPath:    historyPath,
		runningEvents:  make(map[string]bool),
		concurrencySem: make(chan struct{}, cfg.MaxConcurrentEvents),
	}
}

// Start begins the scheduler with the given context
func (s *Scheduler) Start(ctx context.Context) {
	s.ctx, s.cancel = context.WithCancel(ctx)
	defer s.cancel()

	// Initialize cron scheduler with standard 5-field format (minute hour day month weekday)
	s.cron = cron.New()

	// Schedule all enabled events
	enabledCount := 0
	startupCount := 0
	for _, event := range s.config.Events {
		if !event.Enabled {
			slog.Debug("event disabled; skipping", "id", event.ID, "name", event.Name)
			continue
		}

		// Launch startup events immediately in background goroutines
		if event.RunAtStartup {
			startupCount++
			e := event // capture for goroutine
			s.startupWg.Add(1)
			go func() {
				defer s.startupWg.Done()
				slog.Info("startup event launching", "id", e.ID, "name", e.Name)
				s.executeEventWithConcurrency(e)
			}()
		}

		// Schedule cron events (startup-only events have no schedule)
		if event.Schedule != "" {
			if err := s.scheduleEvent(event); err != nil {
				slog.Error("failed to schedule event", "id", event.ID, "name", event.Name, "error", err)
			} else {
				enabledCount++
				slog.Info("event scheduled", "id", event.ID, "name", event.Name, "schedule", event.Schedule)
			}
		} else if !event.RunAtStartup {
			slog.Warn("event has no schedule and run_at_startup is false; skipping", "id", event.ID, "name", event.Name)
		}
	}

	if enabledCount == 0 && startupCount == 0 {
		slog.Warn("no enabled events to schedule")
		return
	}

	// Start the cron scheduler
	s.cron.Start()
	slog.Info("event scheduler running", "scheduled", enabledCount, "startup", startupCount,
		"max_concurrent", s.config.MaxConcurrentEvents)

	// Wait for context cancellation
	<-s.ctx.Done()

	// Graceful shutdown
	slog.Info("event scheduler stopping")
	s.Stop()
}

// Stop gracefully stops the scheduler
func (s *Scheduler) Stop() {
	if s.cron != nil {
		// Stop accepting new jobs
		cronCtx := s.cron.Stop()

		// Wait for running jobs to complete
		<-cronCtx.Done()
		slog.Info("all scheduled events completed")
	}

	// Wait for startup events to complete
	s.startupWg.Wait()
	slog.Info("all startup events completed")

	// Save history
	if err := SaveHistory(s.historyPath, s.history); err != nil {
		slog.Error("failed to save event history", "error", err)
	} else {
		slog.Info("event history saved", "path", s.historyPath)
	}
}

// scheduleEvent registers an event with the cron scheduler
func (s *Scheduler) scheduleEvent(event config.EventConfig) error {
	// Parse and add the cron schedule
	_, err := s.cron.AddFunc(event.Schedule, func() {
		s.executeEventWithConcurrency(event)
	})
	return err
}

// executeEventWithConcurrency executes an event with concurrency control
func (s *Scheduler) executeEventWithConcurrency(event config.EventConfig) {
	// Atomically check if event is already running and try to acquire semaphore
	s.mu.Lock()
	if s.runningEvents[event.ID] {
		s.mu.Unlock()
		slog.Warn("event skipped: already running", "id", event.ID, "name", event.Name)
		return
	}

	// Try to acquire concurrency semaphore while holding the lock
	select {
	case s.concurrencySem <- struct{}{}:
		// Acquired slot, mark as running before releasing lock
		s.runningEvents[event.ID] = true
		s.mu.Unlock()
		defer func() { <-s.concurrencySem }()
	default:
		s.mu.Unlock()
		// At concurrency limit, skip execution
		slog.Warn("event skipped: max concurrent events reached",
			"id", event.ID, "name", event.Name, "max_concurrent", s.config.MaxConcurrentEvents)
		return
	}

	defer func() {
		s.mu.Lock()
		delete(s.runningEvents, event.ID)
		s.mu.Unlock()
	}()

	// Execute the event
	result := s.executeEvent(s.ctx, event)

	// Update history
	s.updateHistory(result)
}
