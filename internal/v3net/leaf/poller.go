package leaf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// poll runs a single poll cycle: fetches new messages from the hub, deduplicates,
// and writes to JAM. Returns the number of new messages processed.
//
// Each page's cursor is the last message the hub returned, in the hub's
// order, whether or not that message was imported, so a page of skipped
// messages still moves the poll forward. A message is identified by its
// origin node and UUID, so the cursor is sent as since=<uuid> and
// since_origin=<node>, and every request carries identity=origin so the hub
// serves every copy of a UUID, one per origin node, even on the first poll
// with no cursor. It also sends format=page, so a hub that knows it says
// whether another page follows inside the signed body (see decodePage).
// A hub that predates these parameters ignores them. A hub can
// still hand back a cursor it does not recognise (it then starts again from
// the beginning), so a cursor already requested in this cycle ends the cycle
// instead of looping.
//
// Each cycle starts at the last message marked seen, so a refused message
// after it is fetched again, and a genuine copy the hub serves in its place
// is imported. Once a newer message is imported the cursor moves past it. A
// dedup row recorded before origin nodes were kept has no origin, so the
// first cycle after an upgrade sends since alone.
func (l *Leaf) poll(ctx context.Context) (int, error) {
	var cur pollCursor
	var err error
	cur.uuid, cur.origin, err = l.cfg.DedupIndex.LastSeen(l.cfg.Network)
	if err != nil {
		return 0, fmt.Errorf("leaf: get cursor: %w", err)
	}

	total := 0
	requested := make(map[pollCursor]bool)
	for {
		if ctx.Err() != nil {
			return total, ctx.Err()
		}
		if requested[cur] {
			slog.Warn("leaf: hub returned a page that does not advance the cursor; ending poll",
				"network", l.cfg.Network, "cursor", cur.uuid, "cursor_origin", cur.origin)
			break
		}
		requested[cur] = true

		path := fmt.Sprintf("/v3net/v1/%s/messages?since=%s&limit=100&identity=%s&format=%s",
			l.cfg.Network, url.QueryEscape(cur.uuid), protocol.IdentityOrigin, protocol.PollFormatPage)
		if cur.origin != "" {
			path += "&since_origin=" + url.QueryEscape(cur.origin)
		}
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

		messages, hasMore, err := decodePage(body, resp.Header)
		if err != nil {
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
			last := messages[len(messages)-1]
			cur = pollCursor{uuid: last.MsgUUID, origin: last.OriginNode}
		}

		if !hasMore || len(messages) == 0 {
			break
		}
	}
	return total, nil
}

// decodePage reads a page of polled messages and whether another follows.
// A hub that knows format=page returns a protocol.MessagePage, whose
// has_more its response signature covers. One that predates it returns a
// bare array, and then the unsigned X-V3Net-Has-More header is all there
// is. A signing hub's body cannot be swapped for the other form without
// failing its signature, and the format parameter is part of the signed
// request path, so it cannot be dropped on the way either.
func decodePage(body []byte, h http.Header) ([]protocol.Message, bool, error) {
	if trimmed := bytes.TrimLeft(body, " \t\r\n"); len(trimmed) > 0 && trimmed[0] == '{' {
		var page protocol.MessagePage
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, false, err
		}
		return page.Messages, page.HasMore, nil
	}
	var messages []protocol.Message
	if err := json.Unmarshal(body, &messages); err != nil {
		return nil, false, err
	}
	return messages, h.Get("X-V3Net-Has-More") == "true", nil
}

// pollCursor names the message a poll page starts after.
type pollCursor struct {
	uuid   string
	origin string // "" when unknown, for a legacy dedup row
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

	seen, err := l.cfg.DedupIndex.Seen(l.cfg.Network, msg.OriginNode, msg.MsgUUID)
	if err != nil {
		return false, fmt.Errorf("leaf: dedup check: %w", err)
	}
	if seen {
		return false, nil
	}

	localNum, err := l.cfg.JAMWriter.WriteMessage(msg)
	if err != nil {
		slog.Error("leaf: write message to JAM", "uuid", msg.MsgUUID, "origin_node", msg.OriginNode, "error", err)
		return false, nil
	}
	if err := l.cfg.DedupIndex.MarkSeen(l.cfg.Network, msg.OriginNode, msg.MsgUUID, &localNum); err != nil {
		slog.Error("leaf: mark seen", "uuid", msg.MsgUUID, "origin_node", msg.OriginNode, "error", err)
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
// message is refused, and at debug when the same message is refused again.
// A message is its origin node and UUID, so another node's message under the
// same UUID still gets its own warning.
func (l *Leaf) refuse(msg *protocol.Message, text string, args ...any) {
	args = append([]any{"uuid", msg.MsgUUID}, args...)
	if l.refused.add(msg.OriginNode + "/" + msg.MsgUUID) {
		slog.Warn(text, args...)
	} else {
		slog.Debug(text, args...)
	}
}

// refusedCap bounds how many refused messages refusedLog remembers.
const refusedCap = 1024

// refusedLog remembers the messages the leaf refused, keyed on origin node
// and UUID. A refused message is not marked seen, so one at the end of the
// hub's list is fetched on every poll until a newer message is imported; the
// log keeps that from repeating the warning each time. Once full it forgets
// its oldest entry. The zero value is ready to use.
type refusedLog struct {
	mu    sync.Mutex
	keys  map[string]struct{}
	order []string // oldest first
}

// add records key and reports whether it was new.
func (r *refusedLog) add(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.keys[key]; ok {
		return false
	}
	if r.keys == nil {
		r.keys = make(map[string]struct{})
	}
	if len(r.order) >= refusedCap {
		delete(r.keys, r.order[0])
		r.order = r.order[1:]
	}
	r.keys[key] = struct{}{}
	r.order = append(r.order, key)
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
