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
	BBSName      string // Local BBS name for subscribe request
	BBSHost      string // Local BBS hostname for subscribe request
}

// DefaultPollInterval is used when no poll interval is configured.
const DefaultPollInterval = 5 * time.Minute
