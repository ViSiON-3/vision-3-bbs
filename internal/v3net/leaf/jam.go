package leaf

import "github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"

// JAMWriter writes a V3Net message to the local JAM message base.
// The concrete implementation is injected from the main BBS package.
type JAMWriter interface {
	WriteMessage(msg protocol.Message) (localMsgNum int64, err error)
}

// JAMFinder is a JAMWriter that can tell whether it already holds a message.
// An import the BBS was stopped in the middle of, or whose dedup record
// could not be confirmed, may or may not have been written; the leaf asks a
// JAMFinder before writing it again. A writer that is not one gets the
// message again, which may duplicate it.
type JAMFinder interface {
	FindMessage(msg protocol.Message) (localMsgNum int64, found bool, err error)
}
