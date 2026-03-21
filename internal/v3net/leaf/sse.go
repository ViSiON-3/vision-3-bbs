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
	for {
		if ctx.Err() != nil {
			return
		}

		err := l.connectSSE(ctx)
		if ctx.Err() != nil {
			return
		}

		if err == nil {
			attempt = 0
		} else {
			slog.Warn("leaf: SSE disconnected", "network", l.cfg.Network, "error", err)
			attempt++
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

func (l *Leaf) connectSSE(ctx context.Context) error {
	path := fmt.Sprintf("/v3net/v1/%s/events", l.cfg.Network)
	resp, err := l.signedGetSSE(ctx, path)
	if err != nil {
		return fmt.Errorf("SSE connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("SSE status: %d", resp.StatusCode)
	}

	slog.Info("leaf: SSE connected", "network", l.cfg.Network)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20) // 1MB max token size
	var eventType string

	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if eventType != "" {
				ev := protocol.Event{
					Type: eventType,
					Data: json.RawMessage(data),
				}
				l.onEvent(ev)
			}
			eventType = ""
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("SSE read: %w", err)
	}
	return fmt.Errorf("SSE stream ended")
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
