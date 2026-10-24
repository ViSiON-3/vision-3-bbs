package message

import (
	"errors"
	"log/slog"

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
// private and not deleted. Only local V3Net posts are written unprocessed
// (see IsV3NetArea), so imported messages and older history never match.
//
// It scans from the area's V3NetOutboundScanner pointer and moves the pointer
// up to just before the first pending message (or to the last message when
// none is pending), so later calls skip what is already done. A pointer past
// the end of the base is treated as 0.
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
	for n := done + 1; n <= count; n++ {
		hdr, err := b.ReadMessageHeader(n)
		if err != nil {
			return pending, err
		}
		const skip = jam.MsgSent | jam.MsgPrivate | jam.MsgDeleted
		if hdr.DateProcessed != 0 || hdr.Attribute&skip != 0 {
			continue
		}
		if len(pending) == 0 {
			newDone = n - 1
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
