// Package leaf implements the V3Net leaf client that polls a hub for messages
// and maintains an SSE connection for real-time events.
package leaf

import (
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/dedup"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// Config holds leaf client configuration.
type Config struct {
	HubURL       string
	Network      string
	AreaTags     []string // V3Net area tags to subscribe to
	PollInterval time.Duration
	Keystore     *keystore.Keystore
	DedupIndex   *dedup.Index
	JAMWriter    JAMWriter
	OnEvent      func(protocol.Event)
	// OnNAL is called with each NAL the leaf fetches and verifies: once
	// after subscribing, and again whenever the hub announces a change.
	OnNAL   func(*protocol.NAL)
	BBSName string // Local BBS name for subscribe request
	BBSHost string // Local BBS hostname for subscribe request

	// CoordPubKeyB64 is the pinned coordinator key (standard base64). Every
	// NAL must be signed by it. Blank means not pinned yet: the first NAL
	// that verifies pins its key and OnKeyPinned is told.
	CoordPubKeyB64 string
	// HubPubKeyB64 is the pinned hub key (standard base64), checked against
	// /info when the leaf starts. Blank pins on first use like
	// CoordPubKeyB64.
	HubPubKeyB64 string
	// PendingCoordPubKeyB64 and PendingHubPubKeyB64 are mismatched keys
	// already reported to the sysop. A key change equal to the pending key
	// is still rejected but not reported again.
	PendingCoordPubKeyB64 string
	PendingHubPubKeyB64   string
	// OnKeyPinned is called when the leaf pins a key on first use, so the
	// caller can persist it, and when a key the sysop accepted (the pending
	// key, now pinned) first verifies, so the caller can drop the pending
	// record. It runs on the leaf's goroutine.
	OnKeyPinned func(KeyEvent)
	// OnKeyChanged is called when the hub offers a key that differs from the
	// pinned one, once per distinct offered key. The leaf keeps the pin and
	// rejects the new key; the caller should alert the sysop and record the
	// offered key. It runs on the leaf's goroutine.
	OnKeyChanged func(KeyEvent)
}

// DefaultPollInterval is used when no poll interval is configured.
const DefaultPollInterval = 5 * time.Minute
