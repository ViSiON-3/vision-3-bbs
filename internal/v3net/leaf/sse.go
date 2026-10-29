package leaf

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

var (
	sseBackoffBase = 5 * time.Second
	sseBackoffMax  = 5 * time.Minute
	sseJitter      = 0.10 // ±10%
)

// runSSE maintains a persistent SSE connection to the hub with exponential
// backoff on disconnect.
func (l *Leaf) runSSE(ctx context.Context) {
	attempt := 0
	firstConnect := true
	for {
		// While syncing is paused over the hub's key, hold the stream down
		// until the key matches the pin again.
		if l.waitHubTrusted(ctx) != nil {
			return
		}
		reconnect := !firstConnect
		err := l.connectSSE(ctx, reconnect)
		firstConnect = false
		if ctx.Err() != nil {
			return
		}
		if l.HubPaused() {
			slog.Warn("leaf: SSE stopped while sync with the hub is paused", "network", l.cfg.Network, "error", err)
			attempt = 0
			continue
		}
		if err != nil {
			slog.Warn("leaf: SSE disconnected", "network", l.cfg.Network, "error", err)
			attempt++
		} else {
			attempt = 0
		}
		delay := backoff(attempt)
		slog.Info("leaf: SSE reconnecting", "network", l.cfg.Network, "delay", delay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// connectSSE opens the event stream and dispatches its events until it ends.
// Events are read in the SSE form the hub sends, one field per line and a
// blank line after each event; fields other than event, id, sig and data are
// skipped. When the hub signs the stream, every event must carry the next
// id in sequence and a sig that verifies (see eventVerifier), or the stream
// is dropped and syncing paused. A pause from elsewhere also drops it.
func (l *Leaf) connectSSE(ctx context.Context, reconnect bool) error {
	// Taken before connecting, so a pause that starts at any point after
	// this drops the stream.
	pausedCh, _ := l.hubChans()
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-pausedCh:
			cancel()
		case <-streamCtx.Done():
		}
	}()

	path := fmt.Sprintf("/v3net/v1/%s/events", l.cfg.Network)
	resp, reqSig, err := l.signedGetSSE(streamCtx, path)
	if err != nil {
		return fmt.Errorf("SSE connect: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // read side

	if resp.StatusCode != 200 {
		return fmt.Errorf("SSE status: %d", resp.StatusCode)
	}
	verifier, err := l.newEventVerifier(ctx, resp, path, reqSig)
	if err != nil {
		return fmt.Errorf("SSE connect: %w", err)
	}

	slog.Info("leaf: SSE connected", "network", l.cfg.Network, "signed", verifier != nil)
	if reconnect {
		l.chatSessions.notifyReconnect()
		// A nal_updated sent while the stream was down is lost, and Start's
		// one-off fetch has long passed, so check the NAL again now.
		if l.cfg.OnNAL != nil && l.reconnectFetching.CompareAndSwap(false, true) {
			go l.refreshNALAfterReconnect(ctx)
		}
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20) // 1MB max token size
	var eventType, id, sig string
	var data []string

	for scanner.Scan() {
		if streamCtx.Err() != nil {
			return streamCtx.Err()
		}

		line := scanner.Text()
		switch {
		case line == "":
			if eventType != "" && data != nil {
				ev := protocol.Event{Type: eventType, Data: json.RawMessage(strings.Join(data, "\n"))}
				if err := verifier.check(id, sig, ev.Type, ev.Data); err != nil {
					l.hubSignatureFailed(ctx, path)
					return fmt.Errorf("SSE event: %w", err)
				}
				l.onEvent(ev)
				l.chatSessions.dispatch(ev)
				l.dispatchNALEvent(ctx, ev)
			}
			eventType, id, sig, data = "", "", "", nil
		case strings.HasPrefix(line, "event: "):
			eventType = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "id: "):
			id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "sig: "):
			sig = strings.TrimPrefix(line, "sig: ")
		case strings.HasPrefix(line, "data: "):
			data = append(data, strings.TrimPrefix(line, "data: "))
		}
	}

	if streamCtx.Err() != nil {
		return streamCtx.Err()
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("SSE read: %w", err)
	}
	return fmt.Errorf("SSE stream ended")
}

// refreshNALAfterReconnect fetches the NAL, retrying with backoff until it
// succeeds or ctx ends. A change the hub announced while the stream was down
// is otherwise not seen until the next announcement, however long that takes.
func (l *Leaf) refreshNALAfterReconnect(ctx context.Context) {
	defer l.reconnectFetching.Store(false)
	for attempt := 1; ; attempt++ {
		err := l.refreshNAL(ctx)
		if err == nil || ctx.Err() != nil {
			return
		}
		delay := backoff(attempt)
		slog.Warn("leaf: NAL fetch after SSE reconnect failed; retrying", "network", l.cfg.Network, "error", err, "delay", delay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func backoff(attempt int) time.Duration {
	d := sseBackoffBase * time.Duration(math.Pow(2, float64(attempt-1)))
	if d > sseBackoffMax {
		d = sseBackoffMax
	}
	// Apply jitter ±10%.
	jitter := 1.0 + (rand.Float64()*2-1)*sseJitter
	return time.Duration(float64(d) * jitter)
}
