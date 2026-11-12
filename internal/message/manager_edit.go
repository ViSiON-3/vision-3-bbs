package message

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// EditMessageIf replaces the subject and body of message msgNum of an area.
// edit is given the message as it is at that moment, read under the base's
// lock (jam.Base.ReplaceMessageIf), and returns the new subject and body, or
// ok false to leave it alone. Use it when the message was shown to someone
// before they edited it: another session may have deleted or edited it, or
// packed the base and moved another message into its number, meanwhile. It
// returns ErrMessageChanged, changing nothing, when edit refuses or the
// message is gone.
//
// The body is the text as DisplayMessage.Body has it; kludge lines in the
// stored text, which Body leaves out, are kept ahead of it. The message
// keeps its number, sender, recipient, date and links.
func (mm *MessageManager) EditMessageIf(areaID, msgNum int, edit func(*DisplayMessage) (subject, body string, ok bool)) error {
	b, _, err := mm.openBase(areaID)
	if err != nil {
		return fmt.Errorf("open base for area %d: %w", areaID, err)
	}
	defer func() {
		if cerr := b.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()
	err = b.ReplaceMessageIf(msgNum, func(m *jam.Message) (string, string, bool) {
		subject, body, ok := edit(displayMessage(m, areaID, msgNum))
		if !ok {
			return "", "", false
		}
		return subject, kludgeLines(m.Text) + normalizeLineEndings(body), true
	})
	if err != nil {
		if errors.Is(err, ErrMessageChanged) {
			return ErrMessageChanged
		}
		return fmt.Errorf("edit message %d in area %d: %w", msgNum, areaID, err)
	}
	mm.invalidateThreadIndex(areaID)
	return nil
}

// kludgeLines is the kludge lines of a stored message text, each ended by a
// line break, in their order: what stripKludgeLines takes out.
func kludgeLines(text string) string {
	var out strings.Builder
	for _, line := range strings.Split(normalizeLineEndings(text), "\n") {
		if len(line) > 0 && line[0] == '\x01' {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	return out.String()
}
