package leaf

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// poll runs a single poll cycle: fetches new messages from the hub, deduplicates,
// and writes to JAM. Returns the number of new messages processed.
//
// Each page's cursor is the UUID of the last message the hub returned, in the
// hub's order, whether or not that message was imported, so a page of skipped
// messages still moves the poll forward. A hub can still hand back a cursor
// it does not recognise (it then starts again from the beginning), so a
// cursor already requested in this cycle ends the cycle instead of looping.
func (l *Leaf) poll(ctx context.Context) (int, error) {
	cursor, err := l.cfg.DedupIndex.LastSeen(l.cfg.Network)
	if err != nil {
		return 0, fmt.Errorf("leaf: get cursor: %w", err)
	}

	total := 0
	requested := make(map[string]bool)
	for {
		if ctx.Err() != nil {
			return total, ctx.Err()
		}
		if requested[cursor] {
			slog.Warn("leaf: hub returned a page that does not advance the cursor; ending poll",
				"network", l.cfg.Network, "cursor", cursor)
			break
		}
		requested[cursor] = true

		path := fmt.Sprintf("/v3net/v1/%s/messages?since=%s&limit=100", l.cfg.Network, url.QueryEscape(cursor))
		resp, err := l.signedGetCtx(ctx, path)
		if err != nil {
			return total, fmt.Errorf("leaf: fetch messages: %w", err)
		}

		body, err := readBody(resp.Body, maxPollRespBytes)
		_ = resp.Body.Close() // read side

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
			imported, err := l.importMessage(msg)
			if err != nil {
				return total, err
			}
			if imported {
				total++
			}
		}
		if len(messages) > 0 {
			cursor = messages[len(messages)-1].MsgUUID
		}

		hasMore := resp.Header.Get("X-V3Net-Has-More") == "true"
		if !hasMore || len(messages) == 0 {
			break
		}
	}
	return total, nil
}

// importMessage checks one polled message and writes it to JAM unless it is
// invalid, fails its author check, belongs to another network, or has been
// seen. A rejected or unwritable message is logged and skipped, not marked
// seen, so a genuine copy under the same UUID can still be imported later.
// It returns an error only for a dedup index failure, which ends the poll.
func (l *Leaf) importMessage(msg protocol.Message) (bool, error) {
	if err := msg.Validate(); err != nil {
		slog.Warn("leaf: invalid message from hub, skipping", "uuid", msg.MsgUUID, "error", err)
		return false, nil
	}
	if msg.Network != l.cfg.Network {
		// A signature binds the network, but a hub could still relay a
		// message signed for another network into this one's poll.
		slog.Warn("leaf: message for another network, skipping", "uuid", msg.MsgUUID,
			"network", l.cfg.Network, "message_network", msg.Network)
		return false, nil
	}
	if !verifyAuthor(&msg) {
		return false, nil
	}
	if msg.NeedsTruncation() {
		if msg.IsSigned() {
			// The author truncates before signing and the hub refuses
			// oversize signed posts, so this was not sent as signed; cutting
			// it would import text the signature does not cover.
			slog.Warn("leaf: signed message over the body limit, dropping", "uuid", msg.MsgUUID,
				"origin_node", msg.OriginNode, "bytes", len(msg.Body))
			return false, nil
		}
		msg.Truncate()
	}

	seen, err := l.cfg.DedupIndex.Seen(msg.MsgUUID)
	if err != nil {
		return false, fmt.Errorf("leaf: dedup check: %w", err)
	}
	if seen {
		return false, nil
	}

	localNum, err := l.cfg.JAMWriter.WriteMessage(msg)
	if err != nil {
		slog.Error("leaf: write message to JAM", "uuid", msg.MsgUUID, "error", err)
		return false, nil
	}
	if err := l.cfg.DedupIndex.MarkSeen(msg.MsgUUID, l.cfg.Network, &localNum); err != nil {
		slog.Error("leaf: mark seen", "uuid", msg.MsgUUID, "error", err)
	}
	return true, nil
}

// verifyAuthor checks a polled message's author signature independently of
// the hub: the signature must verify and origin_node must be derived from
// origin_pubkey_b64. It logs and returns false for a message that claims a
// signature which does not check out, so it is never imported. Legacy
// unsigned messages are accepted while networks migrate to signing.
func verifyAuthor(msg *protocol.Message) bool {
	if !msg.IsSigned() {
		slog.Debug("leaf: accepting unsigned message from hub", "uuid", msg.MsgUUID, "origin_node", msg.OriginNode)
		return true
	}
	if err := msg.VerifySignature(); err != nil {
		slog.Warn("leaf: message failed author signature check, dropping",
			"uuid", msg.MsgUUID, "origin_node", msg.OriginNode, "error", err)
		return false
	}
	return true
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
