package jam

import (
	"fmt"
	"strings"
)

// GenerateMSGID creates a unique FTN-compatible MSGID using the base's
// serial counter. Format: "address hexserial" (e.g., "1:103/705 0012ab34").
// Acquires b.mu internally; do not call while holding b.mu.
func (b *Base) GenerateMSGID(origAddr string) (string, error) {
	serial, err := b.GetNextMsgSerial()
	if err != nil {
		return "", fmt.Errorf("jam: failed to get serial: %w", err)
	}
	return fmt.Sprintf("%s %08x", origAddr, serial), nil
}

// generateMSGIDLocked is for callers that already hold b.mu.
func (b *Base) generateMSGIDLocked(origAddr string) (string, error) {
	serial, err := b.getNextMsgSerialLocked()
	if err != nil {
		return "", fmt.Errorf("jam: failed to get serial: %w", err)
	}
	return fmt.Sprintf("%s %08x", origAddr, serial), nil
}

// CleanReplyID reports whether a stored ReplyID is malformed and returns the
// value a repair should keep. A REPLY names one MSGID, "address serial" (for
// example "21:1/100 00000002"), which is what the tosser stores, so one or two
// tokens are well-formed. More than two means several MSGIDs or trailing junk
// were run together; the repair keeps the first two tokens, since cutting
// back to the address alone would let Link attach the reply to whichever
// message from that address it indexed first.
func CleanReplyID(replyID string) (cleaned string, malformed bool) {
	parts := strings.Fields(replyID)
	if len(parts) <= 2 {
		return replyID, false
	}
	return parts[0] + " " + parts[1], true
}
