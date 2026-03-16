package leaf

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// poll runs a single poll cycle: fetches new messages from the hub, deduplicates,
// and writes to JAM. Returns the number of new messages processed.
func (l *Leaf) poll(ctx context.Context) (int, error) {
	cursor, err := l.cfg.DedupIndex.LastSeen(l.cfg.Network)
	if err != nil {
		return 0, fmt.Errorf("leaf: get cursor: %w", err)
	}

	total := 0
	for {
		if ctx.Err() != nil {
			return total, ctx.Err()
		}

		path := fmt.Sprintf("/v3net/v1/%s/messages?since=%s&limit=100", l.cfg.Network, cursor)
		resp, err := l.signedGet(path)
		if err != nil {
			return total, fmt.Errorf("leaf: fetch messages: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			return total, fmt.Errorf("leaf: read response: %w", err)
		}
		if resp.StatusCode != 200 {
			return total, fmt.Errorf("leaf: fetch messages returned %d", resp.StatusCode)
		}

		var messages []protocol.Message
		if err := json.Unmarshal(body, &messages); err != nil {
			return total, fmt.Errorf("leaf: decode messages: %w", err)
		}

		for _, msg := range messages {
			seen, err := l.cfg.DedupIndex.Seen(msg.MsgUUID)
			if err != nil {
				return total, fmt.Errorf("leaf: dedup check: %w", err)
			}
			if seen {
				continue
			}

			localNum, err := l.cfg.JAMWriter.WriteMessage(msg)
			if err != nil {
				slog.Error("leaf: write message to JAM", "uuid", msg.MsgUUID, "error", err)
				continue
			}

			if err := l.cfg.DedupIndex.MarkSeen(msg.MsgUUID, l.cfg.Network, &localNum); err != nil {
				slog.Error("leaf: mark seen", "uuid", msg.MsgUUID, "error", err)
			}

			total++
			cursor = msg.MsgUUID
		}

		hasMore := resp.Header.Get("X-V3Net-Has-More") == "true"
		if !hasMore || len(messages) == 0 {
			break
		}
	}
	return total, nil
}

// runPoller starts the polling loop. It runs until ctx is cancelled.
func (l *Leaf) runPoller(ctx context.Context) {
	interval := l.cfg.PollInterval
	if interval <= 0 {
		interval = DefaultPollInterval
	}

	// Initial poll on startup.
	if count, err := l.poll(ctx); err != nil {
		slog.Warn("leaf: initial poll failed", "network", l.cfg.Network, "error", err)
	} else if count > 0 {
		slog.Info("leaf: received messages", "network", l.cfg.Network, "count", count)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			slog.Debug("leaf: polling", "network", l.cfg.Network)
			if count, err := l.poll(ctx); err != nil {
				slog.Warn("leaf: poll failed", "network", l.cfg.Network, "error", err)
			} else if count > 0 {
				slog.Info("leaf: received messages", "network", l.cfg.Network, "count", count)
			}
		}
	}
}
