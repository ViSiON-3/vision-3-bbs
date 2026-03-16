package leaf

import "github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"

// JAMWriter writes a V3Net message to the local JAM message base.
// The concrete implementation is injected from the main BBS package.
type JAMWriter interface {
	WriteMessage(msg protocol.Message) (localMsgNum int64, err error)
}
