package jam

import (
	"fmt"
	"strings"
)

// LinkResult contains statistics from a Link operation.
type LinkResult struct {
	// TotalMessages is the number of index records in the base, deleted
	// messages included.
	TotalMessages int
	// MessagesScanned is the number of active (non-deleted) messages whose
	// headers were read and considered for linking.
	MessagesScanned int
	// LinksUpdated is the number of message headers rewritten because at
	// least one of their thread pointers changed.
	LinksUpdated int
}

// Link rebuilds reply threading chains (ReplyTo/Reply1st/ReplyNext) by
// matching MSGID and ReplyID subfields across all active messages. This
// should be called after Pack or after deleting messages to keep threading
// consistent.
//
// A ReplyID matches a parent's full MSGID or, failing that, its address
// alone (some tossers store REPLY without the serial). Reply1st and
// ReplyNext are always recomputed, so a stale value is cleared, including on
// a message with no MSGID (nothing can reply to it) or no ReplyID (it has no
// siblings). ReplyTo is only ever set, never cleared, so a reply whose parent
// is not in the base keeps the ReplyTo it had. The whole pass runs under the base's file lock.
func (b *Base) Link() (LinkResult, error) {
	var result LinkResult

	release, err := b.acquireFileLock()
	if err != nil {
		return result, err
	}
	defer release()

	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.isOpen {
		return result, ErrBaseNotOpen
	}

	total, err := b.getMessageCountLocked()
	if err != nil {
		return result, err
	}
	result.TotalMessages = total
	if total == 0 {
		return result, nil
	}

	// Phase 1: Scan all headers and build MSGID → msgNum / ReplyID → []msgNum maps.
	type hdrInfo struct {
		hdr     *MessageHeader
		msgNum  int
		msgID   string
		replyID string
	}

	var headers []hdrInfo
	msgIDToNum := make(map[string]int)      // MSGID string → 1-based message number
	replyIDToNums := make(map[string][]int) // ReplyID string → list of replying message numbers

	for n := 1; n <= total; n++ {
		hdr, readErr := b.readMessageHeaderLocked(n)
		if readErr != nil {
			continue
		}
		if hdr.Attribute&MsgDeleted != 0 {
			continue
		}

		var msgID, replyID string
		for _, sf := range hdr.Subfields {
			switch sf.LoID {
			case SfldMsgID:
				msgID = string(sf.Buffer)
			case SfldReplyID:
				replyID = string(sf.Buffer)
			}
		}

		headers = append(headers, hdrInfo{hdr: hdr, msgNum: n, msgID: msgID, replyID: replyID})
		if msgID != "" {
			msgIDToNum[msgID] = n
			// FTN MSGIDs are "address serial" — some tossers store REPLY
			// kludges without the serial suffix. Index the address part
			// too so prefix-based lookups succeed.
			if idx := strings.LastIndex(msgID, " "); idx > 0 {
				prefix := msgID[:idx]
				if _, exists := msgIDToNum[prefix]; !exists {
					msgIDToNum[prefix] = n
				}
			}
		}
		if replyID != "" {
			replyIDToNums[replyID] = append(replyIDToNums[replyID], n)
		}
	}

	result.MessagesScanned = len(headers)
	if len(headers) == 0 {
		return result, nil
	}

	// Phase 2: Compute desired threading fields and write changes.
	for i := range headers {
		h := &headers[i]
		changed := false

		// ReplyTo: if this message has a ReplyID, find the parent's message number.
		if h.replyID != "" {
			if parentNum, ok := msgIDToNum[h.replyID]; ok {
				if h.hdr.ReplyTo != uint32(parentNum) {
					h.hdr.ReplyTo = uint32(parentNum)
					changed = true
				}
			}
		}

		// Reply1st: point to the first reply to this message's MSGID. Check
		// both the full MSGID and the address-only prefix (without serial)
		// since some tossers may store REPLY kludges without the serial
		// suffix. With no MSGID nothing can reply to it, so it stays 0; any
		// other value is stale and is cleared.
		wantFirst := uint32(0)
		if h.msgID != "" {
			replies := replyIDToNums[h.msgID]
			if len(replies) == 0 {
				if idx := strings.LastIndex(h.msgID, " "); idx > 0 {
					replies = replyIDToNums[h.msgID[:idx]]
				}
			}
			if len(replies) > 0 {
				wantFirst = uint32(replies[0]) // replies are in scan order (ascending)
			}
		}
		if h.hdr.Reply1st != wantFirst {
			h.hdr.Reply1st = wantFirst
			changed = true
		}

		// ReplyNext: chain sibling replies to the same parent. With no
		// ReplyID the message has no siblings, so it stays 0.
		wantNext := uint32(0)
		if h.replyID != "" {
			siblings := replyIDToNums[h.replyID]
			// Find our position and point to the next sibling.
			for j, sn := range siblings {
				if sn == h.msgNum && j+1 < len(siblings) {
					wantNext = uint32(siblings[j+1])
					break
				}
			}
		}
		if h.hdr.ReplyNext != wantNext {
			h.hdr.ReplyNext = wantNext
			changed = true
		}

		if changed {
			if err := b.updateMessageHeaderLocked(h.msgNum, h.hdr); err != nil {
				return result, fmt.Errorf("updating message %d: %w", h.msgNum, err)
			}
			result.LinksUpdated++
		}
	}

	return result, nil
}
