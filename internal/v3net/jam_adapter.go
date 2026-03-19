package v3net

import (
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
// It prepends a V3NETUUID kludge line to the body for UUID recovery and
// appends the tearline + origin line (matching FTN echomail convention).
func (a *JAMAdapter) WriteMessage(msg protocol.Message) (int64, error) {
	// Prepend UUID kludge for recovery if dedup index is lost.
	body := "\x01V3NETUUID: " + msg.MsgUUID + "\n" + msg.Body

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

// AppendV3NetOrigin appends a tearline and origin line to the message body,
// matching the FTN convention:
//
//	--- ViSiON/3 0.1.0/linux
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
