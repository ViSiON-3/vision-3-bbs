package message

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// GetMessage reads a single message by area ID and 1-based message number.
func (mm *MessageManager) GetMessage(areaID, msgNum int) (*DisplayMessage, error) {
	b, _, err := mm.openBase(areaID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := b.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()

	msg, err := b.ReadMessage(msgNum)
	if err != nil {
		return nil, err
	}

	replyToNum := 0
	if msg.Header != nil && msg.Header.ReplyTo > 0 {
		replyToNum = int(msg.Header.ReplyTo)
	}

	return &DisplayMessage{
		MsgNum:     msgNum,
		From:       msg.From,
		To:         msg.To,
		Subject:    msg.Subject,
		DateTime:   msg.DateTime,
		Body:       stripKludgeLines(normalizeLineEndings(msg.Text)),
		MsgID:      msg.MsgID,
		ReplyID:    msg.ReplyID,
		ReplyToNum: replyToNum,
		OrigAddr:   msg.OrigAddr,
		DestAddr:   msg.DestAddr,
		Attributes: msg.GetAttribute(),
		IsPrivate:  msg.IsPrivate(),
		IsDeleted:  msg.IsDeleted(),
		AreaID:     areaID,
	}, nil
}

// GetMessageCountForArea returns the total message count for an area.
func (mm *MessageManager) GetMessageCountForArea(areaID int) (int, error) {
	b, _, err := mm.openBase(areaID)
	if err != nil {
		if errors.Is(err, ErrAreaNotFound) {
			return 0, nil // Return 0 if area doesn't exist
		}
		return 0, err // Propagate I/O and other errors
	}
	defer func() {
		if cerr := b.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()

	return b.GetMessageCount()
}

// GetTotalMessageCount returns the total number of messages across all areas.
func (mm *MessageManager) GetTotalMessageCount() int {
	areas := mm.ListAreas()
	total := 0
	for _, area := range areas {
		count, err := mm.GetMessageCountForArea(area.ID)
		if err != nil {
			continue
		}
		total += count
	}
	return total
}

// GetThreadReplyCount returns the number of other messages in the same thread.
// Threading matches Vision-2/Pascal behavior: subject-based, ignoring "Re:" prefixes
// and " -Re: #N-" suffixes.
func (mm *MessageManager) GetThreadReplyCount(areaID int, msgNum int, subject string) (int, error) {
	b, _, err := mm.openBase(areaID)
	if err != nil {
		if errors.Is(err, ErrAreaNotFound) {
			return 0, nil // Return 0 if area doesn't exist
		}
		return 0, err // Propagate I/O and other errors
	}
	defer func() {
		if cerr := b.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()

	mm.mu.RLock()
	idx := mm.threadIndex[areaID]
	mm.mu.RUnlock()

	total, err := b.GetMessageCount()
	if err != nil {
		return 0, err
	}

	modCounter := uint32(0)
	modCounterErr := false
	if mc, err := b.GetModCounter(); err == nil {
		modCounter = mc
	} else {
		modCounterErr = true
	}

	if idx == nil || idx.total != total || modCounterErr || (modCounter != 0 && idx.modCounter != modCounter) {
		// Acquire write lock so only one goroutine rebuilds the index;
		// others will wait and reuse the result.
		mm.mu.Lock()
		// Re-check after acquiring write lock (another goroutine may have rebuilt it)
		idx = mm.threadIndex[areaID]
		if idx == nil || idx.total != total || modCounterErr || (modCounter != 0 && idx.modCounter != modCounter) {
			idx = mm.buildThreadIndex(b, total, modCounter)
			mm.threadIndex[areaID] = idx
		}
		mm.mu.Unlock()
	}

	key := ThreadKey(subject)
	count := idx.counts[key]
	if count <= 1 {
		return 0, nil
	}
	return count - 1, nil
}

// GetNewMessageCount returns the number of unread messages for a user in an area.
func (mm *MessageManager) GetNewMessageCount(areaID int, username string) (int, error) {
	b, _, err := mm.openBase(areaID)
	if err != nil {
		if errors.Is(err, ErrAreaNotFound) {
			return 0, nil // Return 0 if area doesn't exist
		}
		return 0, err // Propagate I/O and other errors
	}
	defer func() {
		if cerr := b.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()

	return b.GetUnreadCount(username)
}

// GetLastRead returns the last-read message number for a user in an area.
// Returns 0 if the user has no lastread record.
func (mm *MessageManager) GetLastRead(areaID int, username string) (int, error) {
	b, _, err := mm.openBase(areaID)
	if err != nil {
		if errors.Is(err, ErrAreaNotFound) {
			return 0, nil // Return 0 if area doesn't exist
		}
		return 0, err // Propagate I/O and other errors
	}
	defer func() {
		if cerr := b.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()

	lr, err := b.GetLastRead(username)
	if err != nil {
		if err == jam.ErrNotFound {
			return 0, nil
		}
		return 0, err
	}
	return int(lr.LastReadMsg), nil
}

// SetLastRead updates the lastread pointer for a user in an area.
func (mm *MessageManager) SetLastRead(areaID int, username string, msgNum int) error {
	b, _, err := mm.openBase(areaID)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := b.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()

	return b.MarkMessageRead(username, msgNum)
}

// MarkMessageSent sets the MSG_SENT attribute on a message header.
// Used by V3Net to indicate a locally-posted message was transmitted to the hub.
func (mm *MessageManager) MarkMessageSent(areaID, msgNum int) error {
	b, _, err := mm.openBase(areaID)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := b.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()

	hdr, err := b.ReadMessageHeader(msgNum)
	if err != nil {
		return fmt.Errorf("mark sent: read header: %w", err)
	}

	hdr.Attribute |= jam.MsgSent
	return b.UpdateMessageHeader(msgNum, hdr)
}

// GetNextUnreadMessage returns the next unread message number for a user.
// Returns 0, nil if there are no unread messages.
func (mm *MessageManager) GetNextUnreadMessage(areaID int, username string) (int, error) {
	b, _, err := mm.openBase(areaID)
	if err != nil {
		if errors.Is(err, ErrAreaNotFound) {
			return 0, nil // Return 0 if area doesn't exist
		}
		return 0, err // Propagate I/O and other errors
	}
	defer func() {
		if cerr := b.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()

	next, err := b.GetNextUnreadMessage(username)
	if err != nil {
		if err == jam.ErrNotFound {
			return 0, nil
		}
		return 0, err
	}
	return next, nil
}

// DeleteMessage marks a message as deleted in the JAM base.
// The message is flagged MsgDeleted; call PackAndLinkArea afterward to
// physically remove and re-link, or run v3mail pack + link later.
func (mm *MessageManager) DeleteMessage(areaID, msgNum int) error {
	b, _, err := mm.openBase(areaID)
	if err != nil {
		return fmt.Errorf("open base for area %d: %w", areaID, err)
	}
	defer func() {
		if cerr := b.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()
	if err := b.DeleteMessage(msgNum); err != nil {
		return fmt.Errorf("delete message %d in area %d: %w", msgNum, areaID, err)
	}
	// Invalidate caches so subsequent reads reflect the deletion
	mm.invalidateThreadIndex(areaID)
	return nil
}

// AreaCounts holds the per-area message tallies shown in area listings.
type AreaCounts struct {
	Total    int // messages in the base
	New      int // messages past the user's lastread pointer
	Personal int // messages addressed to the user
}

// GetAreaCounts returns the total, unread and personal message counts for an
// area, opening the JAM base once for all three rather than once each. A
// missing base yields zero counts rather than an error, matching
// GetMessageCountForArea.
func (mm *MessageManager) GetAreaCounts(areaID int, username string) (AreaCounts, error) {
	b, _, err := mm.openBase(areaID)
	if err != nil {
		if errors.Is(err, ErrAreaNotFound) {
			return AreaCounts{}, nil
		}
		return AreaCounts{}, err
	}
	defer func() {
		if cerr := b.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()

	var counts AreaCounts
	if counts.Total, err = b.GetMessageCount(); err != nil {
		return AreaCounts{}, err
	}
	if username == "" {
		return counts, nil
	}
	if counts.New, err = b.GetUnreadCount(username); err != nil {
		return AreaCounts{}, err
	}
	if counts.Personal, err = b.CountMessagesToUser(username); err != nil {
		return AreaCounts{}, err
	}
	return counts, nil
}
