package message

import (
	"errors"
	"fmt"
	"log/slog"
)

// ErrMoveIncomplete is returned by MoveMessageIf when a move failed and the
// copy it wrote to the destination couldn't be taken back, or when it can't
// tell whether the original was deleted, so the message may now be in both
// areas. It is never lost: the copy is kept whenever the original may be
// gone.
var ErrMoveIncomplete = errors.New("move left the message in both areas")

// MoveMessageIf moves message msgNum of area fromArea to area toArea, when
// check accepts it, and returns its number there.
//
// The message is copied first, with its sender, recipient, subject, date,
// sender handle and private flag; its text is what body makes of it. It
// loses its reply links, which name messages in the area it left. Then the
// original is deleted, under its base's lock, only if it is still the
// message copied (SameMessage) and check still accepts it, and that base is
// packed. If another session deleted, edited or moved it meanwhile, or check
// now refuses, the copy is removed again and ErrMessageChanged is returned.
// A message that is gone, or that check refuses from the start, gives
// ErrMessageChanged.
//
// A message is never lost. If writing the copy fails after it reached the
// base, the copy is taken back. If deleting the original fails, the original
// is read again: the copy is taken back only if the original is still there,
// unchanged; if it was deleted after all, the move stands; if it can't be
// read, the copy is kept and ErrMoveIncomplete returned. ErrMoveIncomplete
// is also returned, wrapping the cause, when a copy that should be taken
// back can't be.
func (mm *MessageManager) MoveMessageIf(fromArea, msgNum, toArea int, check func(*DisplayMessage) bool, body func(*DisplayMessage) string) (int, error) {
	if fromArea == toArea {
		return 0, fmt.Errorf("move message %d: area %d to itself", msgNum, fromArea)
	}
	m, err := mm.GetMessage(fromArea, msgNum)
	if err != nil {
		return 0, ErrMessageChanged
	}
	if m.IsDeleted || !check(m) {
		return 0, ErrMessageChanged
	}

	text := body(m)
	newNum, err := mm.Post(toArea, m.From, m.To, m.Subject, text, PostOptions{
		Private:      m.IsPrivate,
		Date:         m.DateTime,
		SenderHandle: m.SenderHandle,
		SenderID:     m.SenderID,
		RecipientID:  m.RecipientID,
	})
	if err == nil {
		err = mm.fault("copy")
	}
	if err != nil {
		err = fmt.Errorf("move message %d from area %d: copy to area %d: %w", msgNum, fromArea, toArea, err)
		// The copy may have been written, with only a later step failing.
		// newNum can name a slot the write never reached, so only that slot
		// is looked at: an earlier message there may be an identical copy
		// from an earlier move, whose original is gone.
		if newNum > 0 {
			if rerr := mm.removeCopyAt(toArea, newNum, m, text); rerr != nil {
				return 0, fmt.Errorf("%w: %w", ErrMoveIncomplete, err)
			}
		}
		return 0, err
	}

	if err = mm.fault("nodelete"); err == nil {
		err = mm.DeleteMessageIf(fromArea, msgNum, func(cur *DisplayMessage) bool {
			return SameMessage(cur, m) && check(cur)
		})
		if err == nil {
			err = mm.fault("delete")
		}
	}
	if err != nil && !errors.Is(err, ErrMessageChanged) {
		// The delete may have taken effect before it failed. Take the copy
		// back only if the original is certainly still there.
		cur, gerr := mm.GetMessage(fromArea, msgNum)
		switch {
		case gerr != nil:
			slog.Error("move: original's state unknown after a failed delete; copy kept", "area", fromArea, "msg", msgNum, "error", err)
			return 0, fmt.Errorf("%w: delete message %d from area %d: %w", ErrMoveIncomplete, msgNum, fromArea, err)
		case cur.IsDeleted && sameHeader(cur, m):
			slog.Warn("move: original was deleted despite an error", "area", fromArea, "msg", msgNum, "error", err)
			err = nil
		}
	}
	if err != nil {
		cause := err
		if errors.Is(err, ErrMessageChanged) {
			cause = ErrMessageChanged
		} else {
			cause = fmt.Errorf("move message %d from area %d: %w", msgNum, fromArea, err)
		}
		if rerr := mm.removeCopy(toArea, newNum, m, text); rerr != nil {
			return 0, fmt.Errorf("%w: %w", ErrMoveIncomplete, cause)
		}
		return 0, cause
	}
	if err := mm.PackAndLinkArea(fromArea); err != nil {
		slog.Error("pack area after a move", "area", fromArea, "error", err)
	}
	return newNum, nil
}

// sameHeader reports whether a and b have the same sender, recipient,
// subject, date, MSGID and sender handle. A deleted message reads back with
// its text blanked, so its header is all there is to know it by.
func sameHeader(a, b *DisplayMessage) bool {
	return a.From == b.From && a.To == b.To && a.Subject == b.Subject &&
		a.DateTime.Equal(b.DateTime) && a.MsgID == b.MsgID && a.SenderHandle == b.SenderHandle
}

// fault is the test hook moveFault for step, nil outside tests.
func (mm *MessageManager) fault(step string) error {
	if mm.moveFault == nil {
		return nil
	}
	return mm.moveFault(step)
}

// copyMatch reports whether c is the copy of m a move wrote with text: the
// same sender, recipient, subject, date and text.
func copyMatch(c, m *DisplayMessage, text string) bool {
	return c.From == m.From && c.To == m.To && c.Subject == m.Subject &&
		c.DateTime.Equal(m.DateTime) && c.Body == normalizeLineEndings(text)
}

// removeCopyAt takes back the copy of m written to area as message num, if
// that message is it, and packs the area. It returns an error only if the
// copy may still be there: a slot past the end of the area, which the write
// never reached, or one holding something else, reads as changed and is no
// error.
func (mm *MessageManager) removeCopyAt(area, num int, m *DisplayMessage, text string) error {
	err := mm.fault("takeback")
	if err == nil {
		err = mm.DeleteMessageIf(area, num, func(c *DisplayMessage) bool { return copyMatch(c, m, text) })
	}
	if errors.Is(err, ErrMessageChanged) {
		return nil
	}
	if err != nil {
		slog.Error("remove the copy of a message whose move failed", "area", area, "msg", num, "error", err)
		return err
	}
	if err := mm.PackAndLinkArea(area); err != nil {
		slog.Error("pack area after taking back a move", "area", area, "error", err)
	}
	return nil
}

// removeCopy takes back the copy of m a failed move wrote to area as message
// num with text, and packs the area. It returns an error if the copy is
// still there; a copy already gone (deleted elsewhere) is no error.
//
// The area isn't locked between the copy and its take-back, so another
// session may have deleted earlier messages there and packed it meanwhile,
// moving the copy to a lower number. A pack only ever moves a message down,
// so the copy is looked for at num first and then at each number below it,
// each one checked under the base's lock (DeleteMessageIf) to be the copy.
// Local posts carry no MSGID, so the copy is known by its sender,
// recipient, subject, date and text.
func (mm *MessageManager) removeCopy(area, num int, m *DisplayMessage, text string) error {
	isCopy := func(c *DisplayMessage) bool { return copyMatch(c, m, text) }
	for i := num; i >= 1; i-- {
		err := mm.fault("takeback")
		if err == nil {
			err = mm.DeleteMessageIf(area, i, isCopy)
		}
		if errors.Is(err, ErrMessageChanged) {
			continue
		}
		if err != nil {
			slog.Error("remove the copy of a message whose move failed", "area", area, "msg", i, "error", err)
			return err
		}
		if err := mm.PackAndLinkArea(area); err != nil {
			slog.Error("pack area after taking back a move", "area", area, "error", err)
		}
		return nil
	}
	slog.Warn("the copy of a message whose move failed was already gone", "area", area, "msg", num)
	return nil
}
