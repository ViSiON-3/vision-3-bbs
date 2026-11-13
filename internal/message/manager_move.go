package message

import (
	"errors"
	"fmt"
	"log/slog"
)

// MoveMessageIf moves message msgNum of area fromArea to area toArea, when
// check accepts it, and returns its number there.
//
// The message is copied first, with its sender, recipient, subject, date,
// sender handle and private flag; its text is what body makes of it. It
// loses its reply links, which name messages in the area it left. Then the
// original is deleted, under its base's lock, only if it is still the
// message copied (SameMessage) and check still accepts it, and that base is
// packed. If another session deleted, edited or moved it meanwhile, or check
// now refuses, the copy is removed again and ErrMessageChanged is returned,
// so the message is never lost and never left in both areas. A message that
// is gone, or that check refuses from the start, gives ErrMessageChanged.
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
	})
	if err != nil {
		return 0, fmt.Errorf("move message %d from area %d: copy to area %d: %w", msgNum, fromArea, toArea, err)
	}

	err = mm.DeleteMessageIf(fromArea, msgNum, func(cur *DisplayMessage) bool {
		return SameMessage(cur, m) && check(cur)
	})
	if err != nil {
		mm.removeCopy(toArea, newNum, m, text)
		if errors.Is(err, ErrMessageChanged) {
			return 0, ErrMessageChanged
		}
		return 0, fmt.Errorf("move message %d from area %d: %w", msgNum, fromArea, err)
	}
	if err := mm.PackAndLinkArea(fromArea); err != nil {
		slog.Error("pack area after a move", "area", fromArea, "error", err)
	}
	return newNum, nil
}

// removeCopy takes back the copy of m a failed move wrote to area as message
// num with text, and packs the area.
//
// The area isn't locked between the copy and its take-back, so another
// session may have deleted earlier messages there and packed it meanwhile,
// moving the copy to a lower number. A pack only ever moves a message down,
// so the copy is looked for at num first and then at each number below it,
// each one checked under the base's lock (DeleteMessageIf) to be the copy.
// Local posts carry no MSGID, so the copy is known by its sender,
// recipient, subject, date and text.
func (mm *MessageManager) removeCopy(area, num int, m *DisplayMessage, text string) {
	isCopy := func(c *DisplayMessage) bool {
		return c.From == m.From && c.To == m.To && c.Subject == m.Subject &&
			c.DateTime.Equal(m.DateTime) && c.Body == normalizeLineEndings(text)
	}
	for i := num; i >= 1; i-- {
		err := mm.DeleteMessageIf(area, i, isCopy)
		if errors.Is(err, ErrMessageChanged) {
			continue
		}
		if err != nil {
			slog.Error("remove the copy of a message whose move failed", "area", area, "msg", i, "error", err)
			return
		}
		if err := mm.PackAndLinkArea(area); err != nil {
			slog.Error("pack area after taking back a move", "area", area, "error", err)
		}
		return
	}
	slog.Error("remove the copy of a message whose move failed: copy not found", "area", area, "msg", num)
}
