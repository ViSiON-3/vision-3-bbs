package message

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// V3NetOutboundScanner is the JAM lastread user whose pointer records how far
// the V3Net outbound sweep has scanned an area: every message up to it is
// processed. Keeping it as a lastread record means a pack, which moves
// lastread pointers onto the new numbering, moves it too, and it survives
// restarts.
const V3NetOutboundScanner = "v3net-outbound"

// PendingOutbound returns the numbers of the area's messages waiting for the
// V3Net outbound sweep: not yet processed (DateProcessed 0), not sent, not
// private, not deleted, and not imported from V3Net (body starting with
// V3NetUUIDPrefix). In local and v3net areas only local V3Net posts are
// written unprocessed (see IsV3NetArea), so older history never matches.
//
// It scans from the area's V3NetOutboundScanner pointer and moves the pointer
// up to just before the first pending message (or to the last message when
// none is pending), so later calls skip what is already done. A pointer past
// the end of the base is treated as 0. A header that cannot be read is
// logged and skipped, and the pointer stays before it so the next call
// tries it again; the messages after it are still returned.
func (mm *MessageManager) PendingOutbound(areaID int) ([]int, error) {
	b, area, err := mm.openBase(areaID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := b.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()

	count, err := b.GetMessageCount()
	if err != nil {
		return nil, err
	}
	done := 0
	if lr, err := b.GetLastRead(V3NetOutboundScanner); err == nil {
		done = int(lr.LastReadMsg)
	} else if !errors.Is(err, jam.ErrNotFound) {
		slog.Warn("v3net outbound: cannot read scan pointer; rescanning area", "area", area.Tag, "error", err)
	}
	if done < 0 || done > count {
		done = 0
	}

	var pending []int
	newDone := count
	stopped := false // newDone is held before a pending or unreadable message
	for n := done + 1; n <= count; n++ {
		hdr, err := b.ReadMessageHeader(n)
		if err != nil {
			// One bad header must not hold up the posts after it: skip it,
			// but keep the pointer before it so it is looked at again.
			slog.Warn("v3net outbound: cannot read message header; will retry", "area", area.Tag, "msg", n, "error", err)
			if !stopped {
				newDone, stopped = n-1, true
			}
			continue
		}
		const skip = jam.MsgSent | jam.MsgPrivate | jam.MsgDeleted
		if hdr.DateProcessed != 0 || hdr.Attribute&skip != 0 {
			continue
		}
		// In an echomail area bound to V3Net, imports from V3Net are also
		// left unprocessed (for the FTN tosser); they carry the UUID kludge
		// and must never be sent back to the hub.
		msg, err := b.ReadMessage(n)
		if err != nil {
			slog.Warn("v3net outbound: cannot read message; will retry", "area", area.Tag, "msg", n, "error", err)
			if !stopped {
				newDone, stopped = n-1, true
			}
			continue
		}
		if strings.HasPrefix(msg.Text, V3NetUUIDPrefix) {
			continue
		}
		if !stopped {
			newDone, stopped = n-1, true
		}
		pending = append(pending, n)
	}
	if newDone != done {
		if err := b.SetLastRead(V3NetOutboundScanner, uint32(newDone), uint32(newDone)); err != nil {
			slog.Warn("v3net outbound: cannot save scan pointer", "area", area.Tag, "error", err)
		}
	}
	return pending, nil
}
