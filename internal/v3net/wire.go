package v3net

import (
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
	"github.com/google/uuid"
)

// BuildWireMessage constructs a V3Net protocol message from local post data.
func BuildWireMessage(network, originNode, originBoard, from, to, subject, body string) protocol.Message {
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
		Kludges:     map[string]any{},
	}
}

func newUUID() string {
	return uuid.New().String()
}
