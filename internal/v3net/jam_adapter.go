package v3net

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// JAMAdapter writes V3Net messages to the local JAM message base via MessageManager.
type JAMAdapter struct {
	mgr    *message.MessageManager
	areaID int
}

// NewJAMAdapter creates a JAM writer for the given message area.
func NewJAMAdapter(mgr *message.MessageManager, areaID int) *JAMAdapter {
	return &JAMAdapter{mgr: mgr, areaID: areaID}
}

// WriteMessage writes a V3Net protocol message to the local JAM base.
// It prepends the V3NETUUID and V3NETORIGIN kludge lines, the message's
// identity, to the body for recovery (see FindMessage) and appends the
// tearline + origin line (matching FTN echomail convention).
func (a *JAMAdapter) WriteMessage(msg protocol.Message) (int64, error) {
	// Prepend the identity kludges for recovery if the dedup index is lost.
	body := message.V3NetUUIDPrefix + msg.MsgUUID + "\n"
	if msg.OriginNode != "" {
		body += message.V3NetOriginPrefix + msg.OriginNode + "\n"
	}
	body += msg.Body

	// Append tearline and origin so users can see where the message came from.
	body = AppendV3NetOrigin(body, msg.Tearline, msg.Origin, msg.OriginNode)

	// Parse the wire date so JAM stores the original authored timestamp.
	dateUTC, err := time.Parse(time.RFC3339, msg.DateUTC)
	if err != nil {
		return 0, fmt.Errorf("v3net jam: parse date: %w", err)
	}

	msgNum, err := a.mgr.AddMessageWithDate(a.areaID, msg.From, msg.To, msg.Subject, body, "", dateUTC)
	if err != nil {
		return 0, fmt.Errorf("v3net jam: write message: %w", err)
	}

	return int64(msgNum), nil
}

// FindMessage reports whether the area already holds msg, imported earlier
// by WriteMessage, and its local message number. See leaf.JAMFinder.
func (a *JAMAdapter) FindMessage(msg protocol.Message) (int64, bool, error) {
	n, found, err := a.mgr.FindV3NetMessage(a.areaID, msg.MsgUUID, msg.OriginNode)
	if errors.Is(err, message.ErrAreaNotFound) {
		// The area is gone (a reload removed it): there is nowhere to
		// write the message, so nothing is left to settle. Reported found,
		// as message 0, as the router reports a tag with no area.
		return 0, true, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("v3net jam: find message: %w", err)
	}
	return int64(n), found, nil
}

// AppendV3NetOrigin appends a tearline and origin line to the message body,
// matching the FTN convention:
//
//	--- ViSiON/3 v0.8.0/Linux
//	 * Origin: My Cool BBS (a1b2c3d4e5f6a7b8)
func AppendV3NetOrigin(body, tearline, origin, nodeID string) string {
	// Nothing to append if both are empty.
	if tearline == "" && origin == "" {
		return body
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	if tearline != "" {
		body += tearline + "\n"
	}
	if origin != "" {
		if nodeID != "" {
			body += fmt.Sprintf(" * Origin: %s (%s)\n", origin, nodeID)
		} else {
			body += fmt.Sprintf(" * Origin: %s\n", origin)
		}
	}
	return body
}
