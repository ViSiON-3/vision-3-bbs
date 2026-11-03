package protocol

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
)

// V3Net Central is the opt-in community layer over a V3Net network. Its first
// feature is the board directory: each participating board publishes one
// signed listing (its name, description, connection addresses and
// capabilities) to the hub, and every subscriber may read the directory.
//
// A listing is authored and signed by the board it describes, so a hub can
// store and relay it but not alter it: a leaf verifies every listing it
// shows against the key inside it and the node ID that key derives, on top
// of the hub's own signature over the reply. Transport authentication proves
// which node sent a request to the hub; the listing signature proves who
// wrote the listing, whoever delivered it.

// Feature names a hub lists in NetworkInfo.Features and a board lists in
// CentralListing.Capabilities. A leaf that does not find a feature here
// treats it as unsupported and hides it, so a hub or board from before the
// feature existed takes part in message networking as before. Later Central
// phases add their own names.
const (
	// FeatureCentralDirectory is the board directory: the hub serves the
	// Central listing endpoints, or the board publishes a listing.
	FeatureCentralDirectory = "central.directory"
)

// CentralListingVersion is the value of CentralListing.V3NetCentral this
// code writes. A listing with another major version is not understood.
const CentralListingVersion = "1.0"

// CentralSignatureDomain prefixes the canonical JSON a listing signature
// covers, keeping it apart from every other signature kind in the protocol
// (see MessageSignatureDomain). The suffix changes if the signed fields do.
const CentralSignatureDomain = "V3NET-CENTRAL-LISTING-v1\n"

// Limits on a listing. A hub refuses a listing over any of them; a leaf
// drops one it is sent. They bound what a directory costs to store, send
// and draw: the largest listing is a few kilobytes, and a directory of
// MaxCentralListings of them well under a megabyte.
const (
	// MaxCentralNameLen is the longest listing name, in characters: the same
	// as a BBS name on the network.
	MaxCentralNameLen = MaxBBSNameLen
	// MaxCentralDescriptionLen is the longest description, in characters.
	MaxCentralDescriptionLen = 240
	// MaxCentralAddresses is the most connection addresses one listing may
	// carry, and MaxCentralAddressLen the longest of them, in characters.
	MaxCentralAddresses  = 8
	MaxCentralAddressLen = 128
	// MaxCentralCapabilities is the most capability names one listing may
	// carry, and MaxCentralCapabilityLen the longest, in characters.
	MaxCentralCapabilities  = 16
	MaxCentralCapabilityLen = 32
	// MaxCentralSoftwareLen is the longest software field, in characters.
	MaxCentralSoftwareLen = 64
	// MaxCentralListingTTL is the longest a listing may declare itself valid
	// for, published_at to expires_at. A board that stays away longer than
	// this drops out of the directory on its own.
	MaxCentralListingTTL = 30 * 24 * time.Hour
	// MaxCentralListings is the most listings a hub holds per network.
	MaxCentralListings = 500
	// MaxCentralClockSkew is how far ahead of the hub's clock a listing's
	// published_at may be before the hub refuses it.
	MaxCentralClockSkew = 5 * time.Minute
)

// centralAddressSchemes are the URL schemes a connection address may use.
var centralAddressSchemes = []string{"telnet", "ssh", "rlogin", "http", "https"}

// centralCapabilityRe is the form of a capability name.
var centralCapabilityRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,31}$`)

// Listing errors.
var (
	// ErrCentralUnsigned is returned by CentralListing.Verify for a listing
	// that carries no signature. Listings are always signed; a hub and a
	// leaf refuse one without.
	ErrCentralUnsigned = errors.New("protocol: central listing is unsigned")
	// ErrCentralOriginPubKey is returned when origin_pubkey_b64 is not a
	// valid base64 ed25519 public key.
	ErrCentralOriginPubKey = errors.New("protocol: invalid origin_pubkey_b64")
	// ErrCentralNodeMismatch is returned when node_id is not the node ID
	// derived from origin_pubkey_b64.
	ErrCentralNodeMismatch = errors.New("protocol: node_id does not match origin_pubkey_b64")
	// ErrCentralSignature is returned when signature_b64 is not valid base64
	// or does not verify over the canonical payload.
	ErrCentralSignature = errors.New("protocol: central listing signature verification failed")
)

// CentralListing is one board's entry in a network's directory. The board
// itself writes and signs it; see Sign and Verify. Every field but
// SignatureB64 is covered by the signature.
//
// Seq orders a board's own listings: a hub keeps the listing with the
// highest seq it has seen from a node and refuses a lower or equal one, so
// a captured older listing cannot be replayed over a newer one. Boards use
// the publication time in nanoseconds.
//
// PublishedAt and ExpiresAt are RFC 3339 UTC. A listing is served until
// ExpiresAt, at most MaxCentralListingTTL after PublishedAt; a board that
// keeps publishing refreshes both, so a listing that reaches its expiry is
// one whose board has been gone that long.
type CentralListing struct {
	V3NetCentral    string   `json:"v3net_central"`
	Network         string   `json:"network"`
	NodeID          string   `json:"node_id"`
	OriginPubKeyB64 string   `json:"origin_pubkey_b64"`
	Seq             int64    `json:"seq"`
	Name            string   `json:"name"`
	Description     string   `json:"description,omitempty"`
	Addresses       []string `json:"addresses,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
	Software        string   `json:"software,omitempty"`
	PublishedAt     string   `json:"published_at"`
	ExpiresAt       string   `json:"expires_at"`
	SignatureB64    string   `json:"signature_b64,omitempty"`
}

// canonicalCentralListing is the signed form of a listing: the same fields
// less the signature, declared in alphabetical order of their JSON keys so
// encoding/json emits sorted keys (as canonicalMessage does). Slices are
// written as empty arrays rather than omitted, so a nil and an empty slice
// sign the same.
type canonicalCentralListing struct {
	Addresses       []string `json:"addresses"`
	Capabilities    []string `json:"capabilities"`
	Description     string   `json:"description"`
	ExpiresAt       string   `json:"expires_at"`
	Name            string   `json:"name"`
	Network         string   `json:"network"`
	NodeID          string   `json:"node_id"`
	OriginPubKeyB64 string   `json:"origin_pubkey_b64"`
	PublishedAt     string   `json:"published_at"`
	Seq             int64    `json:"seq"`
	Software        string   `json:"software"`
	V3NetCentral    string   `json:"v3net_central"`
}

// SignedPayload returns the exact bytes a listing signature covers:
// CentralSignatureDomain followed by the compact JSON of every field but
// signature_b64, keys sorted, HTML escaping off.
func (c *CentralListing) SignedPayload() ([]byte, error) {
	addrs, caps := c.Addresses, c.Capabilities
	if addrs == nil {
		addrs = []string{}
	}
	if caps == nil {
		caps = []string{}
	}
	canon := canonicalCentralListing{
		Addresses:       addrs,
		Capabilities:    caps,
		Description:     c.Description,
		ExpiresAt:       c.ExpiresAt,
		Name:            c.Name,
		Network:         c.Network,
		NodeID:          c.NodeID,
		OriginPubKeyB64: c.OriginPubKeyB64,
		PublishedAt:     c.PublishedAt,
		Seq:             c.Seq,
		Software:        c.Software,
		V3NetCentral:    c.V3NetCentral,
	}
	var buf bytes.Buffer
	buf.WriteString(CentralSignatureDomain)
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(canon); err != nil {
		return nil, fmt.Errorf("protocol: marshal central listing payload: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// Sign sets OriginPubKeyB64 to the signer's key, NodeID to its node ID, and
// SignatureB64 to its signature over SignedPayload. Text fields are
// normalised to valid UTF-8 first, as a message's are, so the signature
// covers the text as it travels. The listing must validate (Validate) once
// signed; Sign does not check that, since a hub or leaf will.
func (c *CentralListing) Sign(s MessageSigner) error {
	c.normalizeUTF8()
	c.NodeID = s.NodeID()
	c.OriginPubKeyB64 = s.PubKeyBase64()
	c.SignatureB64 = ""
	payload, err := c.SignedPayload()
	if err != nil {
		return err
	}
	sig, err := s.SignRaw(payload)
	if err != nil {
		return fmt.Errorf("protocol: sign central listing: %w", err)
	}
	c.SignatureB64 = base64.StdEncoding.EncodeToString(sig)
	return nil
}

// normalizeUTF8 replaces invalid UTF-8 in the listing's text as wireUTF8
// does for a message.
func (c *CentralListing) normalizeUTF8() {
	for _, f := range []*string{&c.V3NetCentral, &c.Network, &c.NodeID, &c.Name, &c.Description, &c.Software, &c.PublishedAt, &c.ExpiresAt} {
		*f = wireUTF8(*f)
	}
	for i := range c.Addresses {
		c.Addresses[i] = wireUTF8(c.Addresses[i])
	}
	for i := range c.Capabilities {
		c.Capabilities[i] = wireUTF8(c.Capabilities[i])
	}
}

// Verify checks the author signature: origin_pubkey_b64 is a valid ed25519
// key, node_id is the node ID derived from it, and signature_b64 verifies
// over SignedPayload. It does not check that the key belongs to any
// particular subscriber; a hub does that against its registry, and a leaf
// against the node ID the directory entry claims. It returns
// ErrCentralUnsigned for a listing with no signature.
func (c *CentralListing) Verify() error {
	if c.OriginPubKeyB64 == "" && c.SignatureB64 == "" {
		return ErrCentralUnsigned
	}
	pub, err := c.OriginPubKey()
	if err != nil {
		return err
	}
	if keystore.NodeIDFromPubKey(pub) != c.NodeID {
		return ErrCentralNodeMismatch
	}
	sig, err := base64.StdEncoding.DecodeString(c.SignatureB64)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return ErrCentralSignature
	}
	payload, err := c.SignedPayload()
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, payload, sig) {
		return ErrCentralSignature
	}
	return nil
}

// OriginPubKey decodes origin_pubkey_b64. It returns ErrCentralOriginPubKey
// if the field is empty, not base64, or not an ed25519 public key.
func (c *CentralListing) OriginPubKey() (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(c.OriginPubKeyB64)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, ErrCentralOriginPubKey
	}
	return ed25519.PublicKey(b), nil
}

// Validate checks the listing's content against the limits above and the
// form of each field, for network. It does not check the signature (see
// Verify) or whether the listing has expired (see Expired). Text that would
// run on a terminal (pipes, control characters) is refused in every field:
// a listing is drawn on other boards' screens as written.
func (c *CentralListing) Validate(network string) error {
	if c.V3NetCentral != CentralListingVersion {
		return fmt.Errorf("unsupported v3net_central version %q", c.V3NetCentral)
	}
	if c.Network != network {
		return fmt.Errorf("network %q does not match %q", c.Network, network)
	}
	if c.NodeID == "" {
		return errors.New("node_id is required")
	}
	if c.Seq <= 0 {
		return errors.New("seq must be positive")
	}
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("name is required")
	}
	for _, f := range []struct {
		name  string
		value string
		max   int
	}{
		{"name", c.Name, MaxCentralNameLen},
		{"description", c.Description, MaxCentralDescriptionLen},
		{"software", c.Software, MaxCentralSoftwareLen},
	} {
		if utf8.RuneCountInString(f.value) > f.max {
			return fmt.Errorf("%s must be at most %d characters", f.name, f.max)
		}
		if hasScreenCodes(f.value) {
			return fmt.Errorf("%s cannot contain pipes or control characters", f.name)
		}
	}
	if len(c.Addresses) > MaxCentralAddresses {
		return fmt.Errorf("at most %d addresses", MaxCentralAddresses)
	}
	for _, a := range c.Addresses {
		if err := ValidateCentralAddress(a); err != nil {
			return err
		}
	}
	if len(c.Capabilities) > MaxCentralCapabilities {
		return fmt.Errorf("at most %d capabilities", MaxCentralCapabilities)
	}
	for _, cap := range c.Capabilities {
		if !centralCapabilityRe.MatchString(cap) {
			return fmt.Errorf("invalid capability %q", cap)
		}
	}
	published, err := time.Parse(time.RFC3339, c.PublishedAt)
	if err != nil {
		return errors.New("published_at must be RFC 3339")
	}
	expires, err := time.Parse(time.RFC3339, c.ExpiresAt)
	if err != nil {
		return errors.New("expires_at must be RFC 3339")
	}
	if !expires.After(published) {
		return errors.New("expires_at must be after published_at")
	}
	if expires.Sub(published) > MaxCentralListingTTL {
		return fmt.Errorf("expires_at must be within %s of published_at", MaxCentralListingTTL)
	}
	return nil
}

// ValidateCentralAddress checks one connection address: an absolute URL of
// at most MaxCentralAddressLen characters with a host, whose scheme is one
// of telnet, ssh, rlogin, http or https, and with no screen codes.
func ValidateCentralAddress(a string) error {
	if utf8.RuneCountInString(a) > MaxCentralAddressLen {
		return fmt.Errorf("address must be at most %d characters", MaxCentralAddressLen)
	}
	if hasScreenCodes(a) || strings.ContainsAny(a, " \t") {
		return errors.New("address cannot contain spaces, pipes or control characters")
	}
	u, err := url.Parse(a)
	if err != nil || u.Host == "" || u.Hostname() == "" || u.User != nil {
		return fmt.Errorf("invalid address %q: want scheme://host[:port]", a)
	}
	if !slices.Contains(centralAddressSchemes, strings.ToLower(u.Scheme)) {
		return fmt.Errorf("invalid address %q: scheme must be one of %s", a, strings.Join(centralAddressSchemes, ", "))
	}
	return nil
}

// Expired reports whether the listing's expires_at is at or before now. A
// listing whose expires_at does not parse counts as expired.
func (c *CentralListing) Expired(now time.Time) bool {
	t, err := time.Parse(time.RFC3339, c.ExpiresAt)
	return err != nil || !t.After(now)
}

// Published returns the listing's published_at, or the zero time if it
// does not parse.
func (c *CentralListing) Published() time.Time {
	t, _ := time.Parse(time.RFC3339, c.PublishedAt)
	return t
}

// HasCapability reports whether the listing names feature among its
// capabilities.
func (c *CentralListing) HasCapability(feature string) bool {
	return slices.Contains(c.Capabilities, feature)
}

// CentralDirectoryEntry is one listing as the hub serves it, with what the
// hub itself knows about the board. LastSeenAt (RFC 3339 UTC) is when the
// node last opened or closed its event stream or published, and Online
// whether it has an event stream open now. Neither is signed by the board:
// they are the hub's word, covered by the hub's signature on the reply.
type CentralDirectoryEntry struct {
	Listing    CentralListing `json:"listing"`
	LastSeenAt string         `json:"last_seen_at,omitempty"`
	Online     bool           `json:"online"`
}

// LastSeen returns the entry's last_seen_at, or the zero time if it is
// absent or does not parse.
func (e *CentralDirectoryEntry) LastSeen() time.Time {
	t, _ := time.Parse(time.RFC3339, e.LastSeenAt)
	return t
}

// CentralDirectory is the body of GET /v3net/v1/{network}/central/listings:
// every unexpired listing on the network. GeneratedAt is the hub's clock
// when it answered, RFC 3339 UTC, for leaves to judge freshness against.
type CentralDirectory struct {
	Network     string                  `json:"network"`
	GeneratedAt string                  `json:"generated_at"`
	Listings    []CentralDirectoryEntry `json:"listings"`
}

// CentralPublishResponse is the body of a successful PUT of a listing.
type CentralPublishResponse struct {
	OK  bool  `json:"ok"`
	Seq int64 `json:"seq"`
}

// SSE event types for Central. Each carries a CentralListingEventPayload.
const (
	// EventCentralListingUpdated says a node published a listing. Leaves
	// fetch the directory again rather than trusting the event's content.
	EventCentralListingUpdated = "central_listing_updated"
	// EventCentralListingWithdrawn says a node withdrew its listing.
	EventCentralListingWithdrawn = "central_listing_withdrawn"
)

// CentralEventTTL is how long a Central event is acted on after it was
// issued. One that arrives later, replayed or held up, is dropped: the
// directory fetch it would prompt has happened since.
const CentralEventTTL = 10 * time.Minute

// EventMeta identifies one event and bounds its life, for events whose
// handling must not repeat. EventID is unique per event; a leaf remembers
// the IDs it has acted on and ignores a repeat. IssuedAt and ExpiresAt are
// RFC 3339 UTC; a leaf ignores an event past its expiry. Events that
// predate it carry none, and a leaf treats those as fresh and unique.
type EventMeta struct {
	EventID   string `json:"event_id,omitempty"`
	IssuedAt  string `json:"issued_at,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// NewEventMeta returns metadata for an event issued now that expires after
// ttl, with a fresh random ID (16 bytes, unpadded base64url).
func NewEventMeta(ttl time.Duration) (EventMeta, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return EventMeta{}, err
	}
	now := time.Now().UTC()
	return EventMeta{
		EventID:   base64.RawURLEncoding.EncodeToString(b[:]),
		IssuedAt:  now.Format(time.RFC3339),
		ExpiresAt: now.Add(ttl).Format(time.RFC3339),
	}, nil
}

// Expired reports whether the event is past its expires_at at now. An
// event with no expiry, or one that does not parse, is not expired.
func (m EventMeta) Expired(now time.Time) bool {
	if m.ExpiresAt == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, m.ExpiresAt)
	return err == nil && !t.After(now)
}

// CentralListingEventPayload is the data of a central_listing_updated or
// central_listing_withdrawn event. Seq is the published listing's seq, or
// 0 for a withdrawal.
type CentralListingEventPayload struct {
	EventMeta
	Network string `json:"network"`
	NodeID  string `json:"node_id"`
	Seq     int64  `json:"seq,omitempty"`
}
