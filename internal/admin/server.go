package admin

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ServerConfig configures an admin Server.
type ServerConfig struct {
	Reg        RegistrySource
	SystemName string
	StartedAt  time.Time
	Refresh    time.Duration
	MaxEvents  int
	CallsToday func() int // returns -1 if unavailable; may be nil
}

// Server polls SessionRegistry, keeps the latest snapshot, and fans out
// diff-synthesized events to subscribers. Read-only; v1 implements no mutations.
type Server struct {
	cfg  ServerConfig
	mu   sync.RWMutex
	prev *SystemSnapshot
	ring []Event
	subs map[chan Event]struct{}
}

// NewServer creates a Server. Call Run to start polling, or tick() in tests.
func NewServer(cfg ServerConfig) *Server {
	if cfg.MaxEvents <= 0 {
		cfg.MaxEvents = 200
	}
	if cfg.Refresh <= 0 {
		cfg.Refresh = time.Second
	}
	return &Server{cfg: cfg, subs: make(map[chan Event]struct{})}
}

// Run polls until ctx is cancelled.
func (s *Server) Run(ctx context.Context) {
	t := time.NewTicker(s.cfg.Refresh)
	defer t.Stop()
	s.tick(time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s.tick(now)
		}
	}
}

// tick builds a snapshot, diffs it, stores events, and fans them out.
func (s *Server) tick(now time.Time) {
	calls := -1
	if s.cfg.CallsToday != nil {
		calls = s.cfg.CallsToday()
	}
	snap := BuildSnapshot(s.cfg.Reg, s.cfg.SystemName, s.cfg.StartedAt, now, calls)

	s.mu.Lock()
	events := DiffSnapshots(s.prev, snap)
	s.prev = snap
	for _, e := range events {
		s.ring = append(s.ring, e)
		if len(s.ring) > s.cfg.MaxEvents {
			s.ring = s.ring[len(s.ring)-s.cfg.MaxEvents:]
		}
	}
	// Fan-out while holding the lock so sends cannot race a concurrent close.
	// Sends are non-blocking, so holding the lock here is bounded and safe.
	for _, e := range events {
		for c := range s.subs {
			select {
			case c <- e:
			default: // drop for slow subscribers; ring buffer holds history
			}
		}
	}
	s.mu.Unlock()
}

// Snapshot returns the most recent snapshot (nil before the first tick).
func (s *Server) Snapshot() *SystemSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.prev
}

// Subscribe returns a channel that first replays the current ring buffer and
// then receives live events until ctx is cancelled.
func (s *Server) Subscribe(ctx context.Context) <-chan Event {
	ch := make(chan Event, 256)
	s.mu.Lock()
	for _, e := range s.ring {
		select {
		case ch <- e:
		default:
		}
	}
	s.subs[ch] = struct{}{}
	s.mu.Unlock()

	go func() {
		<-ctx.Done()
		s.mu.Lock()
		delete(s.subs, ch)
		close(ch)
		s.mu.Unlock()
	}()
	return ch
}

// Execute runs an admin command. v1 supports only CommandRefresh.
func (s *Server) Execute(cmd AdminCommand) (*Result, error) {
	switch cmd.Command {
	case CommandRefresh:
		s.tick(time.Now())
		return &Result{OK: true}, nil
	default:
		return nil, fmt.Errorf("admin: command not supported in read-only v1: %s", cmd.Command)
	}
}
