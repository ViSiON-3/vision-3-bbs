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
//
// The next cycle starts where this one ended (see pollCursor), not at the
// last message marked seen, so a refused message at the end of the hub's
// list is not fetched and logged again on every poll.
func (l *Leaf) poll(ctx context.Context) (int, error) {
	l.pollMu.Lock()
	defer l.pollMu.Unlock()

	cursor := l.pollCursor
	if cursor == "" {
		var err error
		if cursor, err = l.cfg.DedupIndex.LastSeen(l.cfg.Network); err != nil {
			return 0, fmt.Errorf("leaf: get cursor: %w", err)
		}
	}

	total := 0
	writeFailed := false
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
			outcome, err := l.importMessage(msg)
			if err != nil {
				return total, err
			}
			switch outcome {
			case importWritten:
				total++
			case importFailed:
				writeFailed = true
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
	// A message JAM could not take is retried from the last seen message,
	// as before; otherwise the next cycle skips what this one refused.
	if writeFailed {
		l.pollCursor = ""
	} else {
		l.pollCursor = cursor
	}
	return total, nil
}

// importOutcome is what importMessage did with one polled message.
type importOutcome int

const (
	importSkipped importOutcome = iota // refused, for another network, or already seen
	importWritten                      // written to JAM and marked seen
	importFailed                       // JAM refused it; not marked seen
)

// importMessage checks one polled message and writes it to JAM unless it is
// invalid, fails its author check, belongs to another network, or has been
// seen. A rejected or unwritable message is logged and skipped, not marked
// seen, so a genuine copy under the same UUID can still be imported later.
// It returns an error only for a dedup index failure, which ends the poll.
func (l *Leaf) importMessage(msg protocol.Message) (importOutcome, error) {
	if err := msg.Validate(); err != nil {
		slog.Warn("leaf: invalid message from hub, skipping", "uuid", msg.MsgUUID, "error", err)
		return importSkipped, nil
	}
	if msg.Network != l.cfg.Network {
		// A signature binds the network, but a hub could still relay a
		// message signed for another network into this one's poll.
		slog.Warn("leaf: message for another network, skipping", "uuid", msg.MsgUUID,
			"network", l.cfg.Network, "message_network", msg.Network)
		return importSkipped, nil
	}
	if !verifyAuthor(&msg, l.cfg.RequireSignedMessages) {
		return importSkipped, nil
	}
	if msg.NeedsTruncation() {
		if msg.IsSigned() {
			// The author truncates before signing and the hub refuses
			// oversize signed posts, so this was not sent as signed; cutting
			// it would import text the signature does not cover.
			slog.Warn("leaf: signed message over the body limit, dropping", "uuid", msg.MsgUUID,
				"origin_node", msg.OriginNode, "bytes", len(msg.Body))
			return importSkipped, nil
		}
		msg.Truncate()
	}

	seen, err := l.cfg.DedupIndex.Seen(msg.MsgUUID)
	if err != nil {
		return importSkipped, fmt.Errorf("leaf: dedup check: %w", err)
	}
	if seen {
		return importSkipped, nil
	}

	localNum, err := l.cfg.JAMWriter.WriteMessage(msg)
	if err != nil {
		slog.Error("leaf: write message to JAM", "uuid", msg.MsgUUID, "error", err)
		return importFailed, nil
	}
	if err := l.cfg.DedupIndex.MarkSeen(msg.MsgUUID, l.cfg.Network, &localNum); err != nil {
		slog.Error("leaf: mark seen", "uuid", msg.MsgUUID, "error", err)
	}
	return importWritten, nil
}

// verifyAuthor checks a polled message's author signature independently of
// the hub: the signature must verify and origin_node must be derived from
// origin_pubkey_b64. It logs and returns false for a message that claims a
// signature which does not check out, so it is never imported. Legacy
// unsigned messages are accepted while networks migrate to signing, unless
// requireSigned is set.
func verifyAuthor(msg *protocol.Message, requireSigned bool) bool {
	if !msg.IsSigned() {
		if requireSigned {
			slog.Warn("leaf: unsigned message from hub, dropping", "uuid", msg.MsgUUID, "origin_node", msg.OriginNode)
			return false
		}
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
