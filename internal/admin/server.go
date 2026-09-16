package admin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// timeNow is overridable in tests; defaults to time.Now.
var timeNow = time.Now

// ServerConfig configures an admin Server.
type ServerConfig struct {
	Reg        RegistrySource
	SystemName string
	StartedAt  time.Time
	Refresh    time.Duration
	MaxEvents  int
	// Header counter getters. Each returns -1 if unavailable; any may be nil.
	// They run on every tick, so anything expensive should be wrapped in
	// CachedInt.
	TotalUsers  func() int
	CallsToday  func() int
	NewUsers    func() int
	MailWaiting func() int
	// MaxNodes reports the configured node limit; may be nil.
	MaxNodes func() int
	// PendingReloads lists structural config reloads queued for the next
	// idle window; may be nil.
	PendingReloads func() []string
	// ScheduledEvents reports the event scheduler's entries; may be nil.
	ScheduledEvents func() []ScheduledEvent
	// Kick disconnects the caller on nodeID whose session started at
	// connectedAt (a zero time skips that check). Nil means the server
	// rejects CommandKick as unsupported.
	Kick func(nodeID int, connectedAt time.Time) error
}

// Server polls SessionRegistry, keeps the latest snapshot, and fans out
// diff-synthesized events to subscribers.
type Server struct {
	cfg      ServerConfig
	mu       sync.RWMutex
	tickMu   sync.Mutex // serialises tick end-to-end to prevent out-of-order snapshots
	prev     *SystemSnapshot
	ring     []Event
	subs     map[chan Event]struct{}
	lastTick time.Time
}

// RefreshInterval returns the configured polling interval.
func (s *Server) RefreshInterval() time.Duration { return s.cfg.Refresh }

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
// tickMu ensures only one tick runs at a time so snapshots are strictly ordered.
func (s *Server) tick(now time.Time) {
	s.tickMu.Lock()
	defer s.tickMu.Unlock()
	s.tickLocked(now)
}

// refreshIfDue runs a tick only if the last one is older than the refresh
// interval. The freshness check happens under tickMu, and the timestamp is
// taken there too, so concurrent CommandRefresh calls collapse into a single
// poll instead of each observing the same stale lastTick.
func (s *Server) refreshIfDue() {
	s.tickMu.Lock()
	defer s.tickMu.Unlock()

	now := timeNow()
	s.mu.RLock()
	fresh := now.Sub(s.lastTick) < s.cfg.Refresh
	s.mu.RUnlock()
	if fresh {
		return
	}
	s.tickLocked(now)
}

// counterOr calls get if non-nil, else returns -1 (unavailable).
func counterOr(get func() int) int {
	if get == nil {
		return -1
	}
	return get()
}

// tickLocked is the body of tick. Callers must hold tickMu.
func (s *Server) tickLocked(now time.Time) {
	counters := Counters{
		TotalUsers:  counterOr(s.cfg.TotalUsers),
		CallsToday:  counterOr(s.cfg.CallsToday),
		NewUsers:    counterOr(s.cfg.NewUsers),
		MailWaiting: counterOr(s.cfg.MailWaiting),
	}
	var pending []string
	if s.cfg.PendingReloads != nil {
		pending = s.cfg.PendingReloads()
	}
	snap := BuildSnapshot(s.cfg.Reg, s.cfg.SystemName, s.cfg.StartedAt, now, counters)
	snap.Schema = SnapshotSchema
	snap.PendingReloads = pending
	if s.cfg.MaxNodes != nil {
		snap.MaxNodes = s.cfg.MaxNodes()
	}
	if s.cfg.ScheduledEvents != nil {
		snap.ScheduledEvents = s.cfg.ScheduledEvents()
	}

	s.mu.Lock()
	events := DiffSnapshots(s.prev, snap)
	s.prev = snap
	// Monotonic: tick() captures `now` before contending for tickMu, so a
	// refresh-forced tick can land after a later periodic tick. Letting the
	// stale timestamp win would weaken the refresh rate limit.
	if now.After(s.lastTick) {
		s.lastTick = now
	}
	s.publishLocked(events)
	s.mu.Unlock()
}

// publishLocked appends events to the ring buffer and fans them out to every
// subscriber. Callers must hold s.mu. Fan-out happens under the lock so sends
// cannot race a concurrent close; sends are non-blocking, so holding the lock
// here is bounded and safe.
func (s *Server) publishLocked(events []Event) {
	for _, e := range events {
		s.ring = append(s.ring, e)
		if len(s.ring) > s.cfg.MaxEvents {
			s.ring = s.ring[len(s.ring)-s.cfg.MaxEvents:]
		}
	}
	for _, e := range events {
		for c := range s.subs {
			select {
			case c <- e:
			default: // drop for slow subscribers; ring buffer holds history
			}
		}
	}
}

// emit publishes a single server-originated event (one that is not derived
// from a snapshot diff).
func (s *Server) emit(e Event) {
	s.mu.Lock()
	s.publishLocked([]Event{e})
	s.mu.Unlock()
}

// Snapshot returns the most recent snapshot (nil before the first tick).
func (s *Server) Snapshot() *SystemSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.prev
}

// nodeIdentity returns the handle and address of the session on nodeID in
// the latest snapshot, or empty strings when no session with that connect
// time is there (a zero connectedAt matches whatever holds the slot).
func (s *Server) nodeIdentity(nodeID int, connectedAt time.Time) (handle, addr string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.prev == nil {
		return "", ""
	}
	for _, n := range s.prev.Nodes {
		if n.NodeID == nodeID && (connectedAt.IsZero() || n.ConnectedAt.Equal(connectedAt)) {
			return n.Handle, n.RemoteAddr
		}
	}
	return "", ""
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

// ErrKickUnsupported is returned for CommandKick when the server has no
// Kick hook wired.
var ErrKickUnsupported = errors.New("admin: kick not supported by this server")

// Execute runs an admin command.
func (s *Server) Execute(cmd AdminCommand) (*Result, error) {
	switch cmd.Command {
	case CommandRefresh:
		// Rate-limit forced ticks: a client spamming refresh must not drive
		// snapshot rebuilds faster than the configured polling interval.
		s.refreshIfDue()
		return &Result{OK: true}, nil
	case CommandKick:
		if s.cfg.Kick == nil {
			return nil, ErrKickUnsupported
		}
		if cmd.NodeID <= 0 {
			return nil, fmt.Errorf("admin: kick: node id required")
		}
		handle, addr := s.nodeIdentity(cmd.NodeID, cmd.ConnectedAt)
		if err := s.cfg.Kick(cmd.NodeID, cmd.ConnectedAt); err != nil {
			return nil, fmt.Errorf("admin: kick node %d: %w", cmd.NodeID, err)
		}
		s.emit(Event{Time: timeNow(), Type: EventNodeKicked, NodeID: cmd.NodeID, Handle: handle, Addr: addr, Message: "kicked by sysop"})
		// Poll right away so the node's disappearance reaches consoles now
		// rather than on the next scheduled tick.
		s.refreshIfDue()
		return &Result{OK: true, Message: fmt.Sprintf("node %d disconnected", cmd.NodeID)}, nil
	default:
		return nil, fmt.Errorf("admin: unsupported command: %s", cmd.Command)
	}
}
