package message

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
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

// V3NetBoundAreas returns an IsV3NetArea hook that reports the areas the
// given v3net.json leaves bind by board tag, resolved against mm now. It
// does not look at whether V3Net is enabled: a post made while it is off
// still waits for the outbound sweep, which sends it once V3Net is on. Tags
// that name no area are ignored.
func V3NetBoundAreas(mm *MessageManager, leaves []config.V3NetLeafConfig) func(areaID int) bool {
	bound := make(map[int]bool)
	for _, leaf := range leaves {
		for _, tag := range leaf.Boards {
			if area, ok := mm.GetAreaByTag(tag); ok {
				bound[area.ID] = true
			}
		}
	}
	return func(areaID int) bool { return bound[areaID] }
}

// v3NetFindWindow is how many of an area's newest messages FindV3NetMessage
// looks through. It looks for a message an import may have written just
// before the BBS stopped, so only the end of the area matters.
const v3NetFindWindow = 1000

// FindV3NetMessage looks through the newest messages of the area for one
// imported from V3Net as msgUUID from originNode, and returns its number and
// true if there is one. The import's body starts with its V3NetUUIDPrefix
// line, followed by a V3NetOriginPrefix line naming originNode; an import
// from before origin lines were written does not match unless originNode is
// empty. A message that cannot be read might be the one wanted, so if none
// matches, the first read error is returned rather than a plain "not there".
func (mm *MessageManager) FindV3NetMessage(areaID int, msgUUID, originNode string) (int, bool, error) {
	b, area, err := mm.openBase(areaID)
	if err != nil {
		return 0, false, err
	}
	defer func() {
		if cerr := b.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()

	count, err := b.GetMessageCount()
	if err != nil {
		return 0, false, err
	}
	var readErr error
	for n := count; n >= 1 && n > count-v3NetFindWindow; n-- {
		msg, err := b.ReadMessage(n)
		if err != nil {
			if !errors.Is(err, jam.ErrNotFound) {
				slog.Warn("v3net: cannot read message while looking for an import", "area", area.Tag, "msg", n, "error", err)
				if readErr == nil {
					readErr = fmt.Errorf("v3net: read message %d of %s: %w", n, area.Tag, err)
				}
			}
			continue
		}
		if isV3NetImport(msg.Text, msgUUID, originNode) {
			return n, true, nil
		}
	}
	if readErr != nil {
		return 0, false, readErr
	}
	return 0, false, nil
}

// isV3NetImport reports whether text is the body of the V3Net import
// msgUUID from originNode: it starts with the UUID line, and one of the
// kludge lines at its start names the origin (or originNode is empty). Lines
// end in CR or LF, as the base stores them.
func isV3NetImport(text, msgUUID, originNode string) bool {
	lines := strings.FieldsFunc(text, func(r rune) bool { return r == '\r' || r == '\n' })
	if len(lines) == 0 || lines[0] != V3NetUUIDPrefix+msgUUID {
		return false
	}
	if originNode == "" {
		return true
	}
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, "\x01") {
			break
		}
		if line == V3NetOriginPrefix+originNode {
			return true
		}
	}
	return false
}
