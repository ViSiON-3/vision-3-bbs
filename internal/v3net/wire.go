package v3net

import (
	"fmt"
	"runtime"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
	"github.com/ViSiON-3/vision-3-bbs/internal/version"
	"github.com/google/uuid"
)

// BuildWireMessage constructs a V3Net protocol message from local post data.
// The origin parameter is the user-defined origin line text (e.g. "My Cool BBS").
// The tearline is always the standard ViSiON/3 software identifier.
func BuildWireMessage(network, originNode, originBoard, from, to, subject, body, origin string) protocol.Message {
	uuid := newUUID()
	return protocol.Message{
		V3Net:       protocol.ProtocolVersion,
		Network:     network,
		MsgUUID:     uuid,
		ThreadUUID:  uuid,
		ParentUUID:  nil,
		OriginNode:  originNode,
		OriginBoard: originBoard,
		From:        from,
		To:          to,
		Subject:     subject,
		DateUTC:     time.Now().UTC().Format(time.RFC3339),
		Body:        body,
		Tearline:    DefaultTearline(),
		Origin:      origin,
		Kludges:     map[string]any{},
	}
}

// DefaultTearline returns the standard ViSiON/3 software tearline.
func DefaultTearline() string {
	return fmt.Sprintf("--- ViSiON/3 %s/%s", version.Number, runtime.GOOS)
}

func newUUID() string {
	return uuid.New().String()
}
