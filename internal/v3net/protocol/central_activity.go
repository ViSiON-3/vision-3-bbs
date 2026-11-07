package protocol

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
)

// V3Net Central phase 2: network Who's Online and Last Callers.
//
// Each participating board publishes its activity as one signed snapshot
// per kind: the presence snapshot lists the sessions on the board now
// whose callers chose to be shown, and the last-callers snapshot the
// recent calls of callers who chose to have their logins shown. A new
// snapshot replaces the board's previous one whole, so withdrawing a
// caller is publishing a snapshot without them, and a higher seq always
// wins: the hub refuses a snapshot whose seq is not above the highest it
// has seen from the board, so replaying an older one, captured before a
// caller withdrew, cannot put them back.
//
// The board authors and signs its snapshots, as it does its listing; a hub
// stores and serves them byte for byte and cannot alter them. A snapshot
// carries only what the caller agreed to publish: a handle, the board it
// is on, and (for presence) an optional availability. A presence entry is
// a session, not an account: its session ID is a random token for that one
// call, so two calls by one caller cannot be linked through it, and two
// sessions by the same caller show as two entries that end separately.
//
// Presence is temporary. A presence snapshot is valid for at most
// MaxCentralPresenceTTL; the board refreshes it while it runs, and the hub
// drops it as soon as the board's event stream closes, so a board that
// goes away abruptly takes its callers' presence with it within minutes at
// most. Last calls are retained for at most MaxCentralLastCallRetention:
// a last-callers snapshot expires no later than that after its oldest
// entry began, so no hub serves a call older than the limit, even for a
// board that has gone offline and stopped refreshing.

// Feature names for phase 2. A hub lists them in NetworkInfo.Features when
// it serves the endpoints; a leaf hides a feature its hub does not name.
const (
	// FeatureCentralPresence is network Who's Online.
	FeatureCentralPresence = "central.presence"
	// FeatureCentralLastCallers is network Last Callers.
	FeatureCentralLastCallers = "central.lastcallers"
)

// Activity kinds, the value of CentralActivity.Kind and the last element
// of the endpoint path (/v3net/v1/{network}/central/{kind}).
const (
	CentralKindPresence    = "presence"
	CentralKindLastCallers = "lastcallers"
)

// CentralActivityFeature returns the feature name for an activity kind, or
// "" for an unknown kind.
func CentralActivityFeature(kind string) string {
	switch kind {
	case CentralKindPresence:
		return FeatureCentralPresence
	case CentralKindLastCallers:
		return FeatureCentralLastCallers
	}
	return ""
}

// CentralActivityVersion is the value of CentralActivity.V3NetCentral this
// code writes. Another major version is not understood.
const CentralActivityVersion = "1.0"

// CentralActivitySignatureDomain prefixes the canonical JSON an activity
// snapshot signature covers (see CentralSignatureDomain).
const CentralActivitySignatureDomain = "V3NET-CENTRAL-ACTIVITY-v1\n"

// Availability values a caller may publish with their presence. Empty
// means the caller did not choose to share one.
const (
	CentralAvailable = "available"
	CentralBusy      = "busy"
)

// Limits on a snapshot. A hub refuses a snapshot over any of them; a leaf
// drops one it is sent.
const (
	// MaxCentralHandleLen is the longest handle, in characters.
	MaxCentralHandleLen = 30
	// MaxCentralPresenceEntries is the most sessions one presence snapshot
	// may carry.
	MaxCentralPresenceEntries = 64
	// MaxCentralLastCallEntries is the most calls one last-callers
	// snapshot may carry.
	MaxCentralLastCallEntries = 50
	// MaxCentralPresenceTTL is the longest a presence snapshot may declare
	// itself valid for, published_at to expires_at. A board that stops
	// refreshing drops off Who's Online within it.
	MaxCentralPresenceTTL = 5 * time.Minute
	// MaxCentralLastCallRetention is the longest a call is published for:
	// a last-callers snapshot may not include a call that began more than
	// this before it was published, and expires no later than this after
	// the oldest call it includes began, or after it was published.
	MaxCentralLastCallRetention = 24 * time.Hour
	// maxCentralSessionLen is the longest session ID.
	maxCentralSessionLen = 32
)

// centralSessionRe is the form of a session ID: unpadded base64url.
var centralSessionRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8,32}$`)

// Activity errors.
var (
	// ErrCentralActivityUnsigned is returned by CentralActivity.Verify for
	// a snapshot with no signature.
	ErrCentralActivityUnsigned = errors.New("protocol: central activity is unsigned")
	// ErrCentralActivitySignature is returned when the signature does not
	// decode or does not verify.
	ErrCentralActivitySignature = errors.New("protocol: central activity signature verification failed")
)

// CentralActivityEntry is one session (presence) or one call (last
// callers). Handle is required in both. For presence, Session is the
// session's random ID and Availability the caller's chosen availability,
// if any; At and Until are unused. For a call, At is when it began and
// Until when it ended (RFC 3339 UTC); Session and Availability are unused.
type CentralActivityEntry struct {
	Handle       string `json:"handle"`
	Session      string `json:"session,omitempty"`
	Availability string `json:"availability,omitempty"`
	At           string `json:"at,omitempty"`
	Until        string `json:"until,omitempty"`
}

// Began returns the entry's at, or the zero time if it is absent or does
// not parse.
func (e *CentralActivityEntry) Began() time.Time {
	t, _ := time.Parse(time.RFC3339, e.At)
	return t
}

// Ended returns the entry's until, or the zero time.
func (e *CentralActivityEntry) Ended() time.Time {
	t, _ := time.Parse(time.RFC3339, e.Until)
	return t
}

// CentralActivity is one board's snapshot of one kind of activity. The
// board writes and signs it; every field but SignatureB64 is covered. BBS
// is the board's name as it shows its callers. Seq orders the board's
// snapshots of this kind as CentralListing.Seq does.
type CentralActivity struct {
	V3NetCentral    string                 `json:"v3net_central"`
	Kind            string                 `json:"kind"`
	Network         string                 `json:"network"`
	NodeID          string                 `json:"node_id"`
	OriginPubKeyB64 string                 `json:"origin_pubkey_b64"`
	Seq             int64                  `json:"seq"`
	BBS             string                 `json:"bbs"`
	Entries         []CentralActivityEntry `json:"entries"`
	PublishedAt     string                 `json:"published_at"`
	ExpiresAt       string                 `json:"expires_at"`
	SignatureB64    string                 `json:"signature_b64,omitempty"`
}

// canonicalCentralEntry and canonicalCentralActivity are the signed forms,
// keys in alphabetical order and nothing omitted, as for a listing.
type canonicalCentralEntry struct {
	At           string `json:"at"`
	Availability string `json:"availability"`
	Handle       string `json:"handle"`
	Session      string `json:"session"`
	Until        string `json:"until"`
}

type canonicalCentralActivity struct {
	BBS             string                  `json:"bbs"`
	Entries         []canonicalCentralEntry `json:"entries"`
	ExpiresAt       string                  `json:"expires_at"`
	Kind            string                  `json:"kind"`
	Network         string                  `json:"network"`
	NodeID          string                  `json:"node_id"`
	OriginPubKeyB64 string                  `json:"origin_pubkey_b64"`
	PublishedAt     string                  `json:"published_at"`
	Seq             int64                   `json:"seq"`
	V3NetCentral    string                  `json:"v3net_central"`
}

// SignedPayload returns the exact bytes the snapshot signature covers:
// CentralActivitySignatureDomain and the compact JSON of every field but
// signature_b64, keys sorted, HTML escaping off.
func (a *CentralActivity) SignedPayload() ([]byte, error) {
	entries := make([]canonicalCentralEntry, len(a.Entries))
	for i, e := range a.Entries {
		entries[i] = canonicalCentralEntry{At: e.At, Availability: e.Availability, Handle: e.Handle, Session: e.Session, Until: e.Until}
	}
	canon := canonicalCentralActivity{
		BBS:             a.BBS,
		Entries:         entries,
		ExpiresAt:       a.ExpiresAt,
		Kind:            a.Kind,
		Network:         a.Network,
		NodeID:          a.NodeID,
		OriginPubKeyB64: a.OriginPubKeyB64,
		PublishedAt:     a.PublishedAt,
		Seq:             a.Seq,
		V3NetCentral:    a.V3NetCentral,
	}
	var buf bytes.Buffer
	buf.WriteString(CentralActivitySignatureDomain)
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(canon); err != nil {
		return nil, fmt.Errorf("protocol: marshal central activity payload: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// Sign sets the signer's key and node ID on the snapshot and signs it,
// after normalising its text to valid UTF-8.
func (a *CentralActivity) Sign(s MessageSigner) error {
	for _, f := range []*string{&a.V3NetCentral, &a.Kind, &a.Network, &a.BBS, &a.PublishedAt, &a.ExpiresAt} {
		*f = wireUTF8(*f)
	}
	for i := range a.Entries {
		e := &a.Entries[i]
		for _, f := range []*string{&e.Handle, &e.Session, &e.Availability, &e.At, &e.Until} {
			*f = wireUTF8(*f)
		}
	}
	a.NodeID = s.NodeID()
	a.OriginPubKeyB64 = s.PubKeyBase64()
	a.SignatureB64 = ""
	payload, err := a.SignedPayload()
	if err != nil {
		return err
	}
	sig, err := s.SignRaw(payload)
	if err != nil {
		return fmt.Errorf("protocol: sign central activity: %w", err)
	}
	a.SignatureB64 = base64.StdEncoding.EncodeToString(sig)
	return nil
}

// Verify checks the author signature: origin_pubkey_b64 is an ed25519 key,
// node_id derives from it, and signature_b64 verifies over SignedPayload.
func (a *CentralActivity) Verify() error {
	if a.OriginPubKeyB64 == "" && a.SignatureB64 == "" {
		return ErrCentralActivityUnsigned
	}
	pub, err := a.OriginPubKey()
	if err != nil {
		return err
	}
	if keystore.NodeIDFromPubKey(pub) != a.NodeID {
		return ErrCentralNodeMismatch
	}
	sig, err := base64.StdEncoding.DecodeString(a.SignatureB64)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return ErrCentralActivitySignature
	}
	payload, err := a.SignedPayload()
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, payload, sig) {
		return ErrCentralActivitySignature
	}
	return nil
}

// OriginPubKey decodes origin_pubkey_b64.
func (a *CentralActivity) OriginPubKey() (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(a.OriginPubKeyB64)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, ErrCentralOriginPubKey
	}
	return ed25519.PublicKey(b), nil
}

// Validate checks the snapshot's form and limits for network and kind. It
// does not check the signature (Verify) or expiry (Expired). Text with
// pipes or control characters is refused in every field, as in a listing.
func (a *CentralActivity) Validate(network, kind string) error {
	if a.V3NetCentral != CentralActivityVersion {
		return fmt.Errorf("unsupported v3net_central version %q", a.V3NetCentral)
	}
	if a.Kind != kind || CentralActivityFeature(kind) == "" {
		return fmt.Errorf("kind %q does not match %q", a.Kind, kind)
	}
	if a.Network != network {
		return fmt.Errorf("network %q does not match %q", a.Network, network)
	}
	if a.NodeID == "" {
		return errors.New("node_id is required")
	}
	if a.Seq <= 0 {
		return errors.New("seq must be positive")
	}
	if strings.TrimSpace(a.BBS) == "" {
		return errors.New("bbs is required")
	}
	if utf8.RuneCountInString(a.BBS) > MaxBBSNameLen || hasScreenCodes(a.BBS) {
		return fmt.Errorf("bbs must be at most %d characters without pipes or control characters", MaxBBSNameLen)
	}
	published, err := time.Parse(time.RFC3339, a.PublishedAt)
	if err != nil {
		return errors.New("published_at must be RFC 3339")
	}
	expires, err := time.Parse(time.RFC3339, a.ExpiresAt)
	if err != nil {
		return errors.New("expires_at must be RFC 3339")
	}
	if !expires.After(published) {
		return errors.New("expires_at must be after published_at")
	}

	limit, ttl := MaxCentralPresenceEntries, MaxCentralPresenceTTL
	if kind == CentralKindLastCallers {
		limit, ttl = MaxCentralLastCallEntries, MaxCentralLastCallRetention
	}
	if len(a.Entries) > limit {
		return fmt.Errorf("at most %d entries", limit)
	}
	if expires.Sub(published) > ttl {
		return fmt.Errorf("expires_at must be within %s of published_at", ttl)
	}
	sessions := make(map[string]bool, len(a.Entries))
	for i := range a.Entries {
		e := &a.Entries[i]
		if strings.TrimSpace(e.Handle) == "" {
			return errors.New("entry handle is required")
		}
		if utf8.RuneCountInString(e.Handle) > MaxCentralHandleLen || hasScreenCodes(e.Handle) {
			return fmt.Errorf("entry handle must be at most %d characters without pipes or control characters", MaxCentralHandleLen)
		}
		switch kind {
		case CentralKindPresence:
			if !centralSessionRe.MatchString(e.Session) || len(e.Session) > maxCentralSessionLen {
				return errors.New("presence entry needs a session ID of 8 to 32 base64url characters")
			}
			if sessions[e.Session] {
				return errors.New("duplicate session ID")
			}
			sessions[e.Session] = true
			if e.Availability != "" && e.Availability != CentralAvailable && e.Availability != CentralBusy {
				return fmt.Errorf("availability must be %q, %q or empty", CentralAvailable, CentralBusy)
			}
			if e.At != "" || e.Until != "" {
				return errors.New("presence entries carry no times")
			}
		case CentralKindLastCallers:
			if e.Session != "" || e.Availability != "" {
				return errors.New("call entries carry no session or availability")
			}
			began, err := time.Parse(time.RFC3339, e.At)
			if err != nil {
				return errors.New("call entry at must be RFC 3339")
			}
			if began.After(published.Add(MaxCentralClockSkew)) {
				return errors.New("call entry begins after the snapshot was published")
			}
			if published.Sub(began) > MaxCentralLastCallRetention {
				return fmt.Errorf("call entry is older than the %s retention", MaxCentralLastCallRetention)
			}
			if expires.Sub(began) > MaxCentralLastCallRetention {
				return fmt.Errorf("expires_at must be within %s of the oldest call", MaxCentralLastCallRetention)
			}
			if e.Until != "" {
				ended, err := time.Parse(time.RFC3339, e.Until)
				if err != nil {
					return errors.New("call entry until must be RFC 3339")
				}
				if ended.Before(began) {
					return errors.New("call entry ends before it begins")
				}
			}
		}
	}
	return nil
}

// Expired reports whether the snapshot's expires_at is at or before now. A
// snapshot whose expiry does not parse counts as expired.
func (a *CentralActivity) Expired(now time.Time) bool {
	t, err := time.Parse(time.RFC3339, a.ExpiresAt)
	return err != nil || !t.After(now)
}

// Published returns published_at, or the zero time.
func (a *CentralActivity) Published() time.Time {
	t, _ := time.Parse(time.RFC3339, a.PublishedAt)
	return t
}

// CleanCentralText returns s fit for a snapshot's text fields: control
// characters become spaces, pipes slashes, surrounding space is trimmed,
// and the result is cut to max characters.
func CleanCentralText(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '|':
			return '/'
		case r < 0x20 || (r >= 0x7F && r <= 0x9F):
			return ' '
		}
		return r
	}, wireUTF8(s))
	return strings.TrimSpace(TrimField(strings.TrimSpace(s), max))
}

// CentralActivityBoard is one board's snapshot as the hub serves it, with
// whether the board's event stream is open now (the hub's word, covered by
// the hub's signature on the reply, not the board's).
type CentralActivityBoard struct {
	Activity CentralActivity `json:"activity"`
	Online   bool            `json:"online"`
}

// CentralActivityFeed is the body of GET /v3net/v1/{network}/central/{kind}:
// every unexpired snapshot of that kind on the network. For presence, only
// boards whose event stream is open are included.
type CentralActivityFeed struct {
	Network     string                 `json:"network"`
	Kind        string                 `json:"kind"`
	GeneratedAt string                 `json:"generated_at"`
	Boards      []CentralActivityBoard `json:"boards"`
}

// EventCentralActivityUpdated says a board's snapshot of one kind changed:
// published, withdrawn, or dropped because its stream closed. Leaves fetch
// the feed again rather than trusting the event. Its data is a
// CentralActivityEventPayload.
const EventCentralActivityUpdated = "central_activity_updated"

// CentralActivityEventPayload is the data of a central_activity_updated
// event. Seq is the new snapshot's seq, or 0 when the snapshot went.
type CentralActivityEventPayload struct {
	EventMeta
	Network string `json:"network"`
	NodeID  string `json:"node_id"`
	Kind    string `json:"kind"`
	Seq     int64  `json:"seq,omitempty"`
}
