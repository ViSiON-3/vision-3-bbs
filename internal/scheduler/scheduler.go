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
	// Everything shared with Reload is written under the lock, and the loop
	// below iterates a snapshot: the config watcher can call Reload as soon
	// as it is up, which may be while startup scheduling is still in
	// progress.
	c := cron.New()
	s.mu.Lock()
	s.ctx, s.cancel = context.WithCancel(ctx)
	// If a reload already installed a cron before Start got here, its config
	// is newer than the boot config: leave it in place and skip boot-time
	// cron scheduling below, rather than clobbering the reloaded instance —
	// which would keep running, orphaned, alongside the stale one. Startup
	// events still fire either way; they belong to process startup, which is
	// happening regardless of config age.
	bootCronCurrent := s.cron == nil
	if bootCronCurrent {
		s.cron = c
	}
	cfg := s.config
	s.mu.Unlock()
	defer s.cancel()

	// Schedule all enabled events
	enabledCount := 0
	startupCount := 0
	for _, event := range cfg.Events {
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
			if !bootCronCurrent {
				continue // a reload owns the cron; its schedules are newer
			}
			if err := s.scheduleEvent(c, event); err != nil {
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
		// Nothing to run yet, but stay alive: a reload of events.json can
		// schedule events later, and exiting here would leave any cron it
		// starts unstopped at shutdown.
		slog.Warn("no enabled events to schedule")
	}

	// Start the cron scheduler — unless a reload already replaced it while the
	// loop above was scheduling, in which case starting c would resurrect the
	// instance Reload just retired and its events would fire alongside the
	// replacement's.
	// The check and Start happen under the same lock Reload swaps under, so a
	// reload either lands before (the check fails and c stays retired) or
	// waits until c is running and then stops it cleanly.
	s.mu.Lock()
	current := s.cron == c
	maxConcurrent := s.config.MaxConcurrentEvents
	if current {
		c.Start() // non-blocking: just launches the cron run loop
	}
	s.mu.Unlock()
	if current {
		slog.Info("event scheduler running", "scheduled", enabledCount, "startup", startupCount,
			"max_concurrent", maxConcurrent)
	} else {
		slog.Info("event scheduler superseded by a reload during startup")
	}

	// Wait for context cancellation
	<-s.ctx.Done()

	// Graceful shutdown
	slog.Info("event scheduler stopping")
	s.Stop()
}

// Stop gracefully stops the scheduler
func (s *Scheduler) Stop() {
	s.mu.Lock()
	c := s.cron
	s.mu.Unlock()
	if c != nil {
		// Stop accepting new jobs
		cronCtx := c.Stop()

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

// ScheduledCount reports how many cron entries the scheduler currently
// carries. Startup-only events are not cron entries and are not counted.
func (s *Scheduler) ScheduledCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cron == nil {
		return 0
	}
	return len(s.cron.Entries())
}

// Reload replaces the scheduled events with those in cfg, applied without a
// restart when events.json changes.
//
// Only cron-scheduled entries are affected. Run-at-startup events belong to
// process startup and do not re-fire on a reload, and events already mid-run
// are left to finish — the old cron is stopped in the background and its
// running jobs drain on their own. History, the running-event set, and the
// scheduler context all carry over.
func (s *Scheduler) Reload(cfg config.EventsConfig) {
	if cfg.MaxConcurrentEvents <= 0 {
		cfg.MaxConcurrentEvents = 3
	}

	// Build and populate the replacement cron before swapping anything, so a
	// config full of bad schedules still leaves every valid one running.
	newCron := cron.New()
	scheduled := 0
	for _, event := range cfg.Events {
		if !event.Enabled || event.Schedule == "" {
			continue
		}
		e := event // capture for the closure
		if _, err := newCron.AddFunc(e.Schedule, func() {
			s.executeEventWithConcurrency(e)
		}); err != nil {
			slog.Error("failed to schedule event on reload", "id", e.ID, "name", e.Name, "error", err)
			continue
		}
		scheduled++
	}

	s.mu.Lock()
	oldCron := s.cron
	if cfg.MaxConcurrentEvents != s.config.MaxConcurrentEvents {
		// Resize by replacement. In-flight events release into the semaphore
		// they acquired from (captured locally in executeEventWithConcurrency),
		// so the old one drains harmlessly. Until they finish, total
		// concurrency can briefly exceed the new limit.
		s.concurrencySem = make(chan struct{}, cfg.MaxConcurrentEvents)
	}
	s.config = cfg
	s.cron = newCron
	s.mu.Unlock()

	newCron.Start()

	if oldCron != nil {
		// Stop in the background: Stop's context waits for running jobs, and
		// the caller is the config watcher's poll loop.
		go func() { <-oldCron.Stop().Done() }()
	}

	slog.Info("event scheduler reloaded", "scheduled", scheduled,
		"max_concurrent", cfg.MaxConcurrentEvents)
}

// scheduleEvent registers an event with the given cron instance. The instance
// is passed explicitly rather than read from s.cron, which Reload may have
// already replaced.
func (s *Scheduler) scheduleEvent(c *cron.Cron, event config.EventConfig) error {
	// Parse and add the cron schedule
	_, err := c.AddFunc(event.Schedule, func() {
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

	// Capture the semaphore while locked: Reload replaces it when
	// maxConcurrentEvents changes, and a release must always pair with the
	// semaphore its slot was acquired from — releasing into a fresh, empty
	// replacement would block forever.
	sem := s.concurrencySem
	maxConcurrent := s.config.MaxConcurrentEvents
	ctx := s.ctx
	if ctx == nil {
		// A reload can install and start a cron before Start assigns s.ctx; a
		// job firing in that window must not hand executeEvent a nil context.
		ctx = context.Background()
	}

	// Try to acquire concurrency semaphore while holding the lock
	select {
	case sem <- struct{}{}:
		// Acquired slot, mark as running before releasing lock
		s.runningEvents[event.ID] = true
		s.mu.Unlock()
		defer func() { <-sem }()
	default:
		s.mu.Unlock()
		// At concurrency limit, skip execution
		slog.Warn("event skipped: max concurrent events reached",
			"id", event.ID, "name", event.Name, "max_concurrent", maxConcurrent)
		return
	}

	defer func() {
		s.mu.Lock()
		delete(s.runningEvents, event.ID)
		s.mu.Unlock()
	}()

	// Execute the event
	result := s.executeEvent(ctx, event)

	// Update history
	s.updateHistory(result)
}
