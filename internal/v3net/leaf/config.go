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

	// RequireSignedMessages drops polled messages without an author
	// signature instead of importing them as legacy messages.
	RequireSignedMessages bool
	// RequireSignedHub refuses unsigned replies to signed requests and
	// unsigned SSE streams, pausing syncing as a bad signature does,
	// instead of taking them as from a hub that predates hub signing.
	RequireSignedHub bool
	// HubSigned is whether this leaf has seen the hub sign before, as the
	// caller stored it through OnHubSigned. Once set, unsigned replies are
	// refused as with RequireSignedHub.
	HubSigned bool
	// OnHubSigned is called when a hub signature first verifies while
	// HubSigned is unset, so the caller can store HubSigned and a restart
	// cannot be used to accept unsigned replies again. It runs on the
	// leaf's goroutine. An error means it was not stored; the leaf keeps
	// enforcing it and calls OnHubSigned again at the next verified reply,
	// unless UpdatePins meanwhile is told the sysop wrote HubSigned and
	// finds it still unset in the store, which drops the record.
	OnHubSigned func() error
	// HubSignedStored reports whether the caller's store holds HubSigned
	// now. When UpdatePins is handed HubSigned false while the leaf
	// enforces it, the leaf asks this before taking it as the sysop's
	// clear, since a copy of the config read before OnHubSigned stored the
	// record looks the same. Nil takes UpdatePins' value as it is.
	HubSignedStored func() (bool, error)
	// PinStored returns what the caller's store holds for kind now, or
	// ErrNoStoredPin when it has no entry for this subscription. A reload is
	// a copy of the store read earlier, and the leaf may have stored a pin
	// since, so UpdatePins and Retire ask this before acting on it. Nil, or
	// a store that cannot be read, takes the copy as the store's.
	PinStored func(KeyKind) (StoredPin, error)

	// CoordPubKeyB64 is the pinned coordinator key (standard base64). Every
	// NAL must be signed by it. Blank means not pinned yet: the first NAL
	// that verifies pins its key and OnKeyPinned is told.
	CoordPubKeyB64 string
	// HubPubKeyB64 is the pinned hub key (standard base64), checked against
	// /info when the leaf starts and used to verify the hub's signed
	// responses and SSE events. Blank pins on first use like
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
	// record. It runs on the leaf's goroutine. An error means the key was not
	// stored; the leaf keeps enforcing it and calls OnKeyPinned again at the
	// next matching fetch.
	OnKeyPinned func(KeyEvent) error
	// OnKeyChanged is called when the hub offers a key that differs from the
	// pinned one, once per distinct offered key. The leaf keeps the pin and
	// rejects the new key; for the hub key it also pauses syncing with the
	// hub until the key matches the pin again. The caller should alert the
	// sysop and record the offered key. It runs on the leaf's goroutine.
	OnKeyChanged func(KeyEvent)
}

// DefaultPollInterval is used when no poll interval is configured.
const DefaultPollInterval = 5 * time.Minute
