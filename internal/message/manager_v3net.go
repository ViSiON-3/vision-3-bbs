package message

import (
	"log/slog"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// PendingOutbound scans an area's headers from message number from (1-based)
// to the end and returns the numbers of messages waiting for the V3Net
// outbound sweep: not yet processed (DateProcessed 0), not sent, not private
// and not deleted. Only local V3Net posts are written unprocessed (see
// IsV3NetArea), so imported messages and older history never match.
//
// next is the first message number the caller needs to scan from next time:
// the first pending message, or one past the last message when none is
// pending. It drops back to 1 when the base has fewer than from-1 messages,
// as after a pack renumbers it. A from below 1 scans from the start.
func (mm *MessageManager) PendingOutbound(areaID, from int) (pending []int, next int, err error) {
	b, _, err := mm.openBase(areaID)
	if err != nil {
		return nil, from, err
	}
	defer func() {
		if cerr := b.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()

	count, err := b.GetMessageCount()
	if err != nil {
		return nil, from, err
	}
	if from < 1 || from > count+1 {
		from = 1
	}
	next = count + 1
	for n := from; n <= count; n++ {
		hdr, err := b.ReadMessageHeader(n)
		if err != nil {
			return pending, from, err
		}
		const skip = jam.MsgSent | jam.MsgPrivate | jam.MsgDeleted
		if hdr.DateProcessed != 0 || hdr.Attribute&skip != 0 {
			continue
		}
		if len(pending) == 0 {
			next = n
		}
		pending = append(pending, n)
	}
	return pending, next, nil
}
