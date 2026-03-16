package v3net

import (
	"fmt"
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
// It prepends a V3NETUUID kludge line to the body for UUID recovery.
func (a *JAMAdapter) WriteMessage(msg protocol.Message) (int64, error) {
	// Prepend UUID kludge for recovery if dedup index is lost.
	body := "\x01V3NETUUID: " + msg.MsgUUID + "\n" + msg.Body

	// Parse the date for the message.
	dateUTC, err := time.Parse(time.RFC3339, msg.DateUTC)
	if err != nil {
		return 0, fmt.Errorf("v3net jam: parse date: %w", err)
	}
	_ = dateUTC // AddMessage uses current time; the wire date is in the kludge

	msgNum, err := a.mgr.AddMessage(a.areaID, msg.From, msg.To, msg.Subject, body, "")
	if err != nil {
		return 0, fmt.Errorf("v3net jam: write message: %w", err)
	}

	return int64(msgNum), nil
}
