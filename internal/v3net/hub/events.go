package hub

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

const (
	pingInterval = 30 * time.Second
	eventBufSize = 64
)

// Broadcaster fans out SSE events to connected leaf nodes.
type Broadcaster struct {
	mu   sync.RWMutex
	subs map[string][]chan protocol.Event // network → channels
}

// NewBroadcaster creates a new event broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subs: make(map[string][]chan protocol.Event),
	}
}

// Subscribe returns a channel that receives events for the given network
// and a cancel function to unsubscribe.
func (b *Broadcaster) Subscribe(network string) (<-chan protocol.Event, func()) {
	ch := make(chan protocol.Event, eventBufSize)

	b.mu.Lock()
	b.subs[network] = append(b.subs[network], ch)
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		channels := b.subs[network]
		for i, c := range channels {
			if c == ch {
				b.subs[network] = append(channels[:i], channels[i+1:]...)
				close(ch)
				ch = nil // prevent double-close
				return
			}
		}
	}
	return ch, cancel
}

// Publish sends an event to all subscribers of a network.
func (b *Broadcaster) Publish(network string, ev protocol.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.subs[network] {
		select {
		case ch <- ev:
		default:
			// Slow consumer — drop event to avoid blocking.
			slog.Warn("dropping SSE event for slow consumer", "network", network, "type", ev.Type)
		}
	}
}

// StartPing sends ping events to all networks every 30 seconds until ctx is cancelled.
func (b *Broadcaster) StartPing(ctx context.Context) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	pingEvent, _ := protocol.NewEvent(protocol.EventPing, protocol.PingPayload{})

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Copy network keys under RLock, then publish outside the lock
			// to avoid nested RLock (Publish also acquires RLock).
			b.mu.RLock()
			networks := make([]string, 0, len(b.subs))
			for network := range b.subs {
				networks = append(networks, network)
			}
			b.mu.RUnlock()

			for _, network := range networks {
				b.Publish(network, pingEvent)
			}
		}
	}
}

// ServeSSE writes SSE events to the HTTP response for the given network.
func (b *Broadcaster) ServeSSE(w http.ResponseWriter, r *http.Request, network string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch, cancel := b.Subscribe(network)
	defer cancel()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, ev.Data)
			flusher.Flush()
		}
	}
}
