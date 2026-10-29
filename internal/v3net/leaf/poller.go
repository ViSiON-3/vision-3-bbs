package leaf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
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
// Each cycle starts at the last message marked seen, so a refused message
// after it is fetched again, and a genuine copy the hub serves in its place
// is imported. Once a newer message is imported the cursor moves past it.
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
		l.refuse(&msg, "leaf: invalid message from hub, skipping", "error", err)
		return false, nil
	}
	if msg.Network != l.cfg.Network {
		// A signature binds the network, but a hub could still relay a
		// message signed for another network into this one's poll.
		l.refuse(&msg, "leaf: message for another network, skipping",
			"network", l.cfg.Network, "message_network", msg.Network)
		return false, nil
	}
	if err := verifyAuthor(&msg, l.cfg.RequireSignedMessages); errors.Is(err, protocol.ErrMessageUnsigned) {
		l.refuse(&msg, "leaf: unsigned message from hub, dropping", "origin_node", msg.OriginNode)
		return false, nil
	} else if err != nil {
		l.refuse(&msg, "leaf: message failed author signature check, dropping",
			"origin_node", msg.OriginNode, "error", err)
		return false, nil
	}
	if msg.NeedsTruncation() {
		if msg.IsSigned() {
			// The author truncates before signing and the hub refuses
			// oversize signed posts, so this was not sent as signed; cutting
			// it would import text the signature does not cover.
			l.refuse(&msg, "leaf: signed message over the body limit, dropping",
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
// origin_pubkey_b64. A message that claims a signature which does not check
// out gets its VerifySignature error, so it is never imported. Legacy
// unsigned messages are accepted while networks migrate to signing, unless
// requireSigned is set; then they get protocol.ErrMessageUnsigned.
func verifyAuthor(msg *protocol.Message, requireSigned bool) error {
	if !msg.IsSigned() {
		if requireSigned {
			return protocol.ErrMessageUnsigned
		}
		slog.Debug("leaf: accepting unsigned message from hub", "uuid", msg.MsgUUID, "origin_node", msg.OriginNode)
		return nil
	}
	return msg.VerifySignature()
}

// refuse logs a message the leaf will not import: at warn the first time its
// UUID is refused, and at debug when the same UUID is refused again.
func (l *Leaf) refuse(msg *protocol.Message, text string, args ...any) {
	args = append([]any{"uuid", msg.MsgUUID}, args...)
	if l.refused.add(msg.MsgUUID) {
		slog.Warn(text, args...)
	} else {
		slog.Debug(text, args...)
	}
}

// refusedCap bounds how many refused UUIDs refusedLog remembers.
const refusedCap = 1024

// refusedLog remembers the UUIDs of messages the leaf refused. A refused
// message is not marked seen, so one at the end of the hub's list is fetched
// on every poll until a newer message is imported; the log keeps that from
// repeating the warning each time. Once full it forgets its oldest entry.
// The zero value is ready to use.
type refusedLog struct {
	mu    sync.Mutex
	uuids map[string]struct{}
	order []string // oldest first
}

// add records uuid and reports whether it was new.
func (r *refusedLog) add(uuid string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.uuids[uuid]; ok {
		return false
	}
	if r.uuids == nil {
		r.uuids = make(map[string]struct{})
	}
	if len(r.order) >= refusedCap {
		delete(r.uuids, r.order[0])
		r.order = r.order[1:]
	}
	r.uuids[uuid] = struct{}{}
	r.order = append(r.order, uuid)
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
