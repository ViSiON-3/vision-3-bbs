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
	subs map[string][]eventSubscription // network → subscriptions
}

type eventSubscription struct {
	nodeID string
	ch     chan protocol.Event
}

// NewBroadcaster creates a new event broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subs: make(map[string][]eventSubscription),
	}
}

// Subscribe returns a channel for network-wide events and a cancel function
// to unsubscribe. It does not receive events addressed to a specific node.
func (b *Broadcaster) Subscribe(network string) (<-chan protocol.Event, func()) {
	return b.subscribe(network, "")
}

// subscribe binds a stream to the node authenticated by the hub's middleware.
// An empty node ID receives only network-wide events.
func (b *Broadcaster) subscribe(network, nodeID string) (<-chan protocol.Event, func()) {
	ch := make(chan protocol.Event, eventBufSize)

	b.mu.Lock()
	b.subs[network] = append(b.subs[network], eventSubscription{nodeID: nodeID, ch: ch})
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		channels := b.subs[network]
		for i, c := range channels {
			if c.ch == ch {
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
	b.publish(network, "", ev)
}

// publishToNode delivers only to streams authenticated as nodeID on network.
// A missing destination must never turn a targeted event into a broadcast.
func (b *Broadcaster) publishToNode(network, nodeID string, ev protocol.Event) {
	if nodeID == "" {
		return
	}
	b.publish(network, nodeID, ev)
}

func (b *Broadcaster) publish(network, nodeID string, ev protocol.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, sub := range b.subs[network] {
		if nodeID != "" && sub.nodeID != nodeID {
			continue
		}
		select {
		case sub.ch <- ev:
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

// ServeSSE writes network events and events addressed to the authenticated
// X-V3Net-Node-ID to the response. The caller must authenticate the request
// before calling ServeSSE; an absent node ID receives only network events.
//
// With a signer, each event also carries an id field, its sequence number in
// this stream starting at 1, and a sig field, the signer's signature over
// protocol.EventSignedPayload, bound to the request that opened the stream
// (its signature and nonce):
//
//	event: {type}
//	id: {seq}
//	sig: {base64url signature}
//	data: {json}
//
// The extra fields come before data, so a leaf that predates them, which
// dispatches an event at its data line and skips lines it does not know,
// still reads every event. A nil signer or a request without a signature
// header sends events unsigned, as before.
func (b *Broadcaster) ServeSSE(w http.ResponseWriter, r *http.Request, network string, signer protocol.HubSigner) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Subscribe BEFORE announcing readiness to the client. The client's
	// request returns as soon as these headers are flushed, so if we
	// subscribed afterward a caller could publish an event in the window
	// between receiving the headers and this handler registering its
	// channel — that event would fan out to zero subscribers and be lost.
	// Registering first closes that publish-before-subscribe race.
	ch, cancel := b.subscribe(network, r.Header.Get(headerNodeID))
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	reqSig := r.Header.Get(headerSignature)
	nonce := protocol.RequestNonce(r.Header.Get(protocol.HeaderNonce))
	if reqSig == "" {
		signer = nil
	}
	var seq uint64

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			// Best-effort: a dead connection is detected by the request context.
			if signer == nil {
				_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, ev.Data)
				flusher.Flush()
				continue
			}
			seq++
			sig, err := protocol.SignEvent(signer, network, reqSig, nonce, seq, ev.Type, ev.Data)
			if err != nil {
				// A gap in the sequence would only make the leaf drop the
				// stream, so end it here; the leaf reconnects.
				slog.Error("hub: sign SSE event", "network", network, "error", err)
				return
			}
			_, _ = fmt.Fprintf(w, "event: %s\nid: %d\nsig: %s\ndata: %s\n\n", ev.Type, seq, sig, ev.Data)
			flusher.Flush()
		}
	}
}
