package message

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
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
	return displayMessage(msg, areaID, msgNum), nil
}

// displayMessage is msg, message msgNum of area areaID, as GetMessage
// returns it.
func displayMessage(msg *jam.Message, areaID, msgNum int) *DisplayMessage {
	replyToNum := 0
	if msg.Header != nil && msg.Header.ReplyTo > 0 {
		replyToNum = int(msg.Header.ReplyTo)
	}

	return &DisplayMessage{
		MsgNum: msgNum,
		// The base may hold a control character in any of these from before
		// they were cleaned on the way in, or from another tool. Everything
		// that shows a message or passes it on reads it here.
		From:       ansi.HeaderText(msg.From),
		To:         ansi.HeaderText(msg.To),
		Subject:    ansi.HeaderText(msg.Subject),
		DateTime:   msg.DateTime,
		Body:       stripKludgeLines(normalizeLineEndings(msg.Text)),
		MsgID:      msg.MsgID,
		ReplyID:    msg.ReplyID,
		ReplyToNum: replyToNum,
		OrigAddr:   ansi.HeaderText(msg.OrigAddr),
		DestAddr:   ansi.HeaderText(msg.DestAddr),
		Attributes: msg.GetAttribute(),
		IsPrivate:  msg.IsPrivate(),
		IsDeleted:  msg.IsDeleted(),
		AreaID:     areaID,
		Charset:    messageCharset(msg),

		SenderHandle: msg.SenderHandle,
		MailGroup:    mailGroupName(msg.MailGroup),
		MailGroupID:  mailGroupID(msg.MailGroup),
		SenderID:     msg.SenderID,
		RecipientID:  msg.RecipientID,
	}
}

// mailGroupName and mailGroupID split a SfldV3MailGroup value, "name id".
func mailGroupName(v string) string {
	name, _, _ := strings.Cut(strings.TrimSpace(v), " ")
	return ansi.HeaderText(name)
}

func mailGroupID(v string) string {
	_, id, _ := strings.Cut(strings.TrimSpace(v), " ")
	return strings.TrimSpace(id)
}

// EachMessage reads every message of an area in order, from message 1, and
// calls fn with each until fn returns false. The base is opened once for the
// whole walk, so this is the way to look at every message of a large area.
// Messages flagged deleted are passed too (DisplayMessage.IsDeleted); a
// message that can't be read is skipped and logged. It returns
// ErrAreaNotFound for an unknown area, and an error if the base can't be
// opened or counted. The base stays open while fn runs.
//
// When header is not nil it sees each message first with only its header
// read: the body is empty, and an echomail origin address that only the
// origin line gives is missing. A message it returns false for is skipped
// without reading its text, so a walk that leaves out deleted messages or
// other people's private mail doesn't pay for their bodies.
func (mm *MessageManager) EachMessage(areaID int, header, fn func(*DisplayMessage) bool) error {
	b, _, err := mm.openBase(areaID)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := b.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()
	total, err := b.GetMessageCount()
	if err != nil {
		return err
	}
	for n := 1; n <= total; n++ {
		hdr, err := b.ReadMessageHeader(n)
		if err != nil {
			slog.Warn("skipping unreadable message", "area", areaID, "msg", n, "error", err)
			continue
		}
		if header != nil && !header(displayMessage(jam.MessageFrom(hdr, ""), areaID, n)) {
			continue
		}
		text, err := b.ReadMessageText(hdr)
		if err != nil {
			slog.Warn("skipping unreadable message", "area", areaID, "msg", n, "error", err)
			continue
		}
		if !fn(displayMessage(jam.MessageFrom(hdr, text), areaID, n)) {
			return nil
		}
	}
	return nil
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

// AdvanceLastRead moves a user's lastread pointer in an area forward to
// msgNum, leaving it alone when it already stands at or past it. The message
// reader uses this so viewing an older message does not rewind the pointer
// that new-mail and newscan counts are measured from.
func (mm *MessageManager) AdvanceLastRead(areaID int, username string, msgNum int) error {
	b, _, err := mm.openBase(areaID)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := b.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()

	return b.AdvanceLastRead(username, msgNum)
}

// MarkMessageSent sets the MSG_SENT attribute on a message header, and in a
// local or v3net area stamps DateProcessed, so the V3Net outbound sweep does
// not send it again. Used by
// V3Net once a locally-posted message has been transmitted to the hub.
func (mm *MessageManager) MarkMessageSent(areaID, msgNum int) error {
	b, area, err := mm.openBase(areaID)
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
	// In echomail and netmail areas DateProcessed belongs to the FTN
	// tosser's export; MSG_SENT alone keeps the sweep from resending.
	if jam.DetermineMessageType(area.AreaType, area.EchoTag).IsLocal() {
		hdr.DateProcessed = uint32(time.Now().Unix())
	}
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

// ErrMessageChanged is DeleteMessageIf's answer when the message is gone or
// no longer the one its check expects.
var ErrMessageChanged = jam.ErrMessageChanged

// ErrEditUncertain is EditMessageIf's answer when the edit failed partway and
// couldn't be undone: the message may be as it was or as edited.
var ErrEditUncertain = jam.ErrEditUncertain

// DeleteMessageIf deletes message msgNum of an area only when check accepts
// the message as it is at that moment, read under the same lock as the
// delete (jam.Base.DeleteMessageIf). Use it when the message was shown to
// someone before they confirmed: another session may have deleted it, or
// packed the base and moved another message into its number, meanwhile. It
// returns ErrMessageChanged, deleting nothing, when check refuses.
func (mm *MessageManager) DeleteMessageIf(areaID, msgNum int, check func(*DisplayMessage) bool) error {
	b, _, err := mm.openBase(areaID)
	if err != nil {
		return fmt.Errorf("open base for area %d: %w", areaID, err)
	}
	defer func() {
		if cerr := b.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()
	err = b.DeleteMessageIf(msgNum, func(m *jam.Message) bool {
		return check(displayMessage(m, areaID, msgNum))
	})
	if err != nil {
		if errors.Is(err, ErrMessageChanged) {
			return ErrMessageChanged
		}
		return fmt.Errorf("delete message %d in area %d: %w", msgNum, areaID, err)
	}
	mm.invalidateThreadIndex(areaID)
	return nil
}

// SameMessage reports whether a and b are the same message: the same
// sender, recipient, subject, date, MSGID and text. A message's number
// isn't enough, as a pack renumbers the base.
func SameMessage(a, b *DisplayMessage) bool {
	return a.From == b.From && a.To == b.To && a.Subject == b.Subject &&
		a.DateTime.Equal(b.DateTime) && a.MsgID == b.MsgID && a.Body == b.Body &&
		a.SenderHandle == b.SenderHandle
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
