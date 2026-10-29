package leaf

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// Limits on one SSE event, as read before the blank line that ends it. The
// scanner caps each line, but an event can be any number of lines and
// nothing is verified until it ends, so these cap what is buffered for it.
// A stream over either limit is dropped, signed or not. The hub sends four
// fields per event and its largest event is far below the byte limit.
const (
	maxSSEEventFields = 16
	maxSSEEventBytes  = 1 << 20 // 1MB of field values
)

// errSSEEventTooLarge is returned when an event goes over the limits above.
var errSSEEventTooLarge = errors.New("SSE event too large")

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
// is dropped and syncing paused. A pause from elsewhere also drops it, as
// does an event over maxSSEEventFields or maxSSEEventBytes. A refusal is
// checked like any signed reply before its status is returned.
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
	resp, req, err := l.signedGetSSE(streamCtx, path)
	if err != nil {
		return fmt.Errorf("SSE connect: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // read side

	if resp.StatusCode != 200 {
		// A refusal is signed like any other reply. Checking it lets a hub
		// that now signs with another key (and so refuses or is refused)
		// pause syncing and report the key, instead of reconnecting forever.
		body, err := readBody(resp.Body, maxRespBytes)
		if err != nil {
			return fmt.Errorf("SSE status: %d: read body: %w", resp.StatusCode, err)
		}
		if err := l.verifyHubResponse(ctx, resp, path, req, body); err != nil {
			return fmt.Errorf("SSE status: %d: %w", resp.StatusCode, err)
		}
		return fmt.Errorf("SSE status: %d", resp.StatusCode)
	}
	verifier, err := l.newEventVerifier(ctx, resp, path, req)
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
	fields, size := 0, 0

	for scanner.Scan() {
		if streamCtx.Err() != nil {
			return streamCtx.Err()
		}

		line := scanner.Text()
		if line == "" {
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
			fields, size = 0, 0
			continue
		}

		fields++
		if fields > maxSSEEventFields {
			return fmt.Errorf("%w: over %d fields", errSSEEventTooLarge, maxSSEEventFields)
		}
		switch {
		case strings.HasPrefix(line, "event: "):
			eventType = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "id: "):
			id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "sig: "):
			sig = strings.TrimPrefix(line, "sig: ")
		case strings.HasPrefix(line, "data: "):
			data = append(data, strings.TrimPrefix(line, "data: "))
		default:
			continue
		}
		if size += len(line); size > maxSSEEventBytes {
			return fmt.Errorf("%w: over %d bytes", errSSEEventTooLarge, maxSSEEventBytes)
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
