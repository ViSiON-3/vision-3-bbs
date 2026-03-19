package v3net

import (
	"crypto/rand"
	"fmt"
	"runtime"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
	"github.com/ViSiON-3/vision-3-bbs/internal/version"
)

// BuildWireMessage constructs a V3Net protocol message from local post data.
// The areaTag parameter is the V3Net area tag (e.g. "gen.general").
// The origin parameter is the user-defined origin line text (e.g. "My Cool BBS").
// The tearline is always the standard ViSiON/3 software identifier.
func BuildWireMessage(network, areaTag, originNode, originBoard, from, to, subject, body, origin string) protocol.Message {
	uuid := newUUID()
	return protocol.Message{
		V3Net:       protocol.ProtocolVersion,
		Network:     network,
		AreaTag:     areaTag,
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
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
