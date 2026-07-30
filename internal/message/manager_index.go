package message

import (
	"log/slog"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

func (mm *MessageManager) buildThreadIndex(b *jam.Base, total int, modCounter uint32) *threadIndex {
	counts := make(map[string]int)
	for i := 1; i <= total; i++ {
		hdr, err := b.ReadMessageHeader(i)
		if err != nil {
			slog.Warn("failed to read message header", "index", i, "error", err)
			continue
		}
		if hdr.Attribute&jam.MsgDeleted != 0 {
			continue
		}
		subject := subjectFromHeader(hdr)
		key := ThreadKey(subject)
		counts[key]++
	}
	return &threadIndex{
		total:      total,
		modCounter: modCounter,
		counts:     counts,
	}
}

func subjectFromHeader(hdr *jam.MessageHeader) string {
	for _, sf := range hdr.Subfields {
		if sf.LoID == jam.SfldSubject {
			return string(sf.Buffer)
		}
	}
	return ""
}

func (mm *MessageManager) invalidateThreadIndex(areaID int) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	delete(mm.threadIndex, areaID)
	delete(mm.msgidIndex, areaID)
}

// FindMessageByMSGID searches for a message in the given area whose MSGID
// matches the supplied value. Returns the 1-based message number, or 0 if
// not found.  Uses a cached index that is rebuilt only when the message
// base has changed (same invalidation strategy as threadIndex).
func (mm *MessageManager) FindMessageByMSGID(areaID int, msgID string) int {
	if msgID == "" {
		return 0
	}

	b, _, err := mm.openBase(areaID)
	if err != nil {
		return 0
	}
	defer func() {
		if cerr := b.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()

	total, err := b.GetMessageCount()
	if err != nil || total == 0 {
		return 0
	}

	modCounter := uint32(0)
	if mc, mcErr := b.GetModCounter(); mcErr == nil {
		modCounter = mc
	}

	// Fast path: check existing index under read lock
	mm.mu.RLock()
	idx := mm.msgidIndex[areaID]
	mm.mu.RUnlock()

	if idx == nil || idx.total != total || (modCounter != 0 && idx.modCounter != modCounter) {
		mm.mu.Lock()
		// Re-check after acquiring write lock
		idx = mm.msgidIndex[areaID]
		if idx == nil || idx.total != total || (modCounter != 0 && idx.modCounter != modCounter) {
			idx = mm.buildMSGIDIndex(b, total, modCounter)
			mm.msgidIndex[areaID] = idx
		}
		mm.mu.Unlock()
	}

	if n, ok := idx.msgIDs[msgID]; ok {
		return n
	}
	return 0
}

// buildMSGIDIndex scans all messages and builds a MSGID -> message number map.
func (mm *MessageManager) buildMSGIDIndex(b *jam.Base, total int, modCounter uint32) *msgidIndex {
	ids := make(map[string]int, total)
	for i := 1; i <= total; i++ {
		hdr, err := b.ReadMessageHeader(i)
		if err != nil {
			slog.Warn("failed to read message header for MSGID index", "index", i, "error", err)
			continue
		}
		if hdr.Attribute&jam.MsgDeleted != 0 {
			continue
		}
		for _, sf := range hdr.Subfields {
			if sf.LoID == jam.SfldMsgID {
				full := string(sf.Buffer)
				ids[full] = i
				// FTN MSGIDs are "address serial" — some tossers store REPLY
				// kludges without the serial suffix.  Index the address part
				// too so prefix-based lookups succeed.
				if idx := strings.LastIndex(full, " "); idx > 0 {
					prefix := full[:idx]
					if _, exists := ids[prefix]; !exists {
						ids[prefix] = i
					}
				}
				break
			}
		}
	}
	return &msgidIndex{
		total:      total,
		modCounter: modCounter,
		msgIDs:     ids,
	}
}
