package v3net

import (
	"crypto/rand"
	"fmt"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
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
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
