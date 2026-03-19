package leaf

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// Leaf is a V3Net leaf client that polls a hub for messages and maintains
// an SSE connection for real-time events.
type Leaf struct {
	cfg      Config
	client   *http.Client
	eventCb  atomic.Value // stores func(protocol.Event)
	nalCache *nal.Cache
}

// New creates a new Leaf with the given configuration.
func New(cfg Config) *Leaf {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = DefaultPollInterval
	}
	l := &Leaf{
		cfg:      cfg,
		client:   &http.Client{Timeout: 10 * time.Second},
		nalCache: nal.NewCache(1 * time.Hour),
	}
	if cfg.OnEvent != nil {
		l.eventCb.Store(cfg.OnEvent)
	}
	return l
}

// HubURL returns the hub URL this leaf connects to.
func (l *Leaf) HubURL() string {
	return l.cfg.HubURL
}

// Poll runs a single poll cycle. Exported for integration testing.
func (l *Leaf) Poll(ctx context.Context) (int, error) {
	return l.poll(ctx)
}

// RunSSE runs the SSE connection loop. Exported for integration testing.
func (l *Leaf) RunSSE(ctx context.Context) {
	l.runSSE(ctx)
}

// SetOnEvent sets the event callback. Safe for concurrent use.
func (l *Leaf) SetOnEvent(fn func(protocol.Event)) {
	l.eventCb.Store(fn)
}

// onEvent loads and invokes the event callback if set.
func (l *Leaf) onEvent(ev protocol.Event) {
	if v := l.eventCb.Load(); v != nil {
		v.(func(protocol.Event))(ev)
	}
}

// Close releases resources held by the leaf (idle HTTP connections, etc.).
func (l *Leaf) Close() {
	l.client.CloseIdleConnections()
}

// Start begins the polling and SSE goroutines. Blocks until ctx is cancelled.
func (l *Leaf) Start(ctx context.Context) {
	slog.Info("leaf: starting", "network", l.cfg.Network, "hub", l.cfg.HubURL)

	// Subscribe to the hub (bootstrap — no auth required).
	// Retry with exponential backoff if the hub is temporarily unreachable.
	subscribeBackoff := 5 * time.Second
	for {
		if err := l.subscribe(ctx); err != nil {
			slog.Warn("leaf: subscribe failed, retrying", "network", l.cfg.Network, "error", err, "retry_in", subscribeBackoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(subscribeBackoff):
			}
			subscribeBackoff *= 2
			if subscribeBackoff > 5*time.Minute {
				subscribeBackoff = 5 * time.Minute
			}
			continue
		}
		break
	}
	slog.Info("leaf: subscribed to hub", "network", l.cfg.Network, "hub", l.cfg.HubURL)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		l.runPoller(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		l.runSSE(ctx)
	}()

	wg.Wait()
	slog.Info("leaf: stopped", "network", l.cfg.Network)
}
