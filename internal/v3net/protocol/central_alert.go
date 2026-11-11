package protocol

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
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

// V3Net Central activity alerts: short live notices of what callers do on
// the boards of a network. A board sends one when a caller who agreed to
// it logs on, logs off, joins a network chat room, or posts in a network
// message area. The board writes and signs the alert, as it does its
// listing and activity snapshots, so a hub relays it but cannot alter it
// or make one up; the hub sends it on to every board on the network as a
// central_alert event, and each board shows it to its own callers who
// asked for that kind of alert.
//
// An alert is news, not a record. It lives at most MaxCentralAlertTTL and
// is never stored: a hub keeps only the IDs it has relayed, until they
// expire, so a board that retries a send, or one whose send is replayed,
// does not alert anyone twice; a board that reconnects is sent nothing it
// missed. Each alert carries a random ID unique to the one event, and a
// board that sends the same event on more than one network sends the same
// ID on each, so a board on both networks shows it once.
//
// An alert carries only the caller's handle, the board's name, and for a
// chat join the room's name or for a post the area's tag: never a message
// body, a subject, a private message, or anything from a private room.

// FeatureCentralAlerts is network activity alerts: the hub relays signed
// alerts between the boards of a network. A board sends none, and shows
// none, on a network whose hub does not name it.
const FeatureCentralAlerts = "central.alerts"

// Alert kinds, the value of CentralAlert.Kind.
const (
	CentralAlertLogon    = "logon"
	CentralAlertLogoff   = "logoff"
	CentralAlertChatJoin = "chat_join"
	CentralAlertPost     = "post"
)

// CentralAlertKinds are the alert kinds, in the order screens list them.
var CentralAlertKinds = []string{CentralAlertLogon, CentralAlertLogoff, CentralAlertChatJoin, CentralAlertPost}

// ValidCentralAlertKind reports whether kind is one of CentralAlertKinds.
func ValidCentralAlertKind(kind string) bool {
	switch kind {
	case CentralAlertLogon, CentralAlertLogoff, CentralAlertChatJoin, CentralAlertPost:
		return true
	}
	return false
}

// CentralAlertVersion is the value of CentralAlert.V3NetCentral this code
// writes. Another major version is not understood.
const CentralAlertVersion = "1.0"

// CentralAlertSignatureDomain prefixes the canonical JSON an alert
// signature covers (see CentralSignatureDomain).
const CentralAlertSignatureDomain = "V3NET-CENTRAL-ALERT-v1\n"

// EventCentralAlert carries one alert from the hub to the boards of a
// network. Its data is the CentralAlert as its board signed it.
const EventCentralAlert = "central_alert"

// Alert limits.
const (
	// MaxCentralAlertTTL is the longest an alert may declare itself valid
	// for, published_at to expires_at. A hub refuses a longer one and a
	// board ignores one past its expiry, so an alert held up or replayed
	// is never shown as new.
	MaxCentralAlertTTL = 5 * time.Minute
	// MaxCentralRoomLen is the longest chat room name, as network chat
	// allows them.
	MaxCentralRoomLen = 32
)

// centralAlertIDRe is the form of an alert ID: unpadded base64url, 16 to
// 32 characters (a 16-byte random ID is 22).
var centralAlertIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{16,32}$`)

// centralRoomRe is the form of a network chat room name.
var centralRoomRe = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

// Alert errors.
var (
	// ErrCentralAlertUnsigned is returned by CentralAlert.Verify for an
	// alert with no signature.
	ErrCentralAlertUnsigned = errors.New("protocol: central alert is unsigned")
	// ErrCentralAlertSignature is returned when the signature does not
	// decode or does not verify.
	ErrCentralAlertSignature = errors.New("protocol: central alert signature verification failed")
)

// CentralAlert is one alert. The board writes and signs it; every field
// but SignatureB64 is covered. ID identifies the event (see the package
// notes above); BBS is the board's name as it shows its callers; Handle the
// caller's. Room is set for a chat join only, the room joined; Area for a
// post only, the network area's tag.
type CentralAlert struct {
	V3NetCentral    string `json:"v3net_central"`
	Kind            string `json:"kind"`
	ID              string `json:"id"`
	Network         string `json:"network"`
	NodeID          string `json:"node_id"`
	OriginPubKeyB64 string `json:"origin_pubkey_b64"`
	BBS             string `json:"bbs"`
	Handle          string `json:"handle"`
	Room            string `json:"room,omitempty"`
	Area            string `json:"area,omitempty"`
	PublishedAt     string `json:"published_at"`
	ExpiresAt       string `json:"expires_at"`
	SignatureB64    string `json:"signature_b64,omitempty"`
}

// canonicalCentralAlert is the signed form, keys in alphabetical order and
// nothing omitted, as for a listing.
type canonicalCentralAlert struct {
	Area            string `json:"area"`
	BBS             string `json:"bbs"`
	ExpiresAt       string `json:"expires_at"`
	Handle          string `json:"handle"`
	ID              string `json:"id"`
	Kind            string `json:"kind"`
	Network         string `json:"network"`
	NodeID          string `json:"node_id"`
	OriginPubKeyB64 string `json:"origin_pubkey_b64"`
	PublishedAt     string `json:"published_at"`
	Room            string `json:"room"`
	V3NetCentral    string `json:"v3net_central"`
}

// NewCentralAlertID returns a fresh random alert ID: 16 bytes, unpadded
// base64url.
func NewCentralAlertID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// SignedPayload returns the exact bytes the alert signature covers:
// CentralAlertSignatureDomain and the compact JSON of every field but
// signature_b64, keys sorted, HTML escaping off.
func (a *CentralAlert) SignedPayload() ([]byte, error) {
	canon := canonicalCentralAlert{
		Area:            a.Area,
		BBS:             a.BBS,
		ExpiresAt:       a.ExpiresAt,
		Handle:          a.Handle,
		ID:              a.ID,
		Kind:            a.Kind,
		Network:         a.Network,
		NodeID:          a.NodeID,
		OriginPubKeyB64: a.OriginPubKeyB64,
		PublishedAt:     a.PublishedAt,
		Room:            a.Room,
		V3NetCentral:    a.V3NetCentral,
	}
	var buf bytes.Buffer
	buf.WriteString(CentralAlertSignatureDomain)
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(canon); err != nil {
		return nil, fmt.Errorf("protocol: marshal central alert payload: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// Sign sets the signer's key and node ID on the alert and signs it, after
// normalising its text to valid UTF-8.
func (a *CentralAlert) Sign(s MessageSigner) error {
	for _, f := range []*string{&a.V3NetCentral, &a.Kind, &a.ID, &a.Network, &a.BBS, &a.Handle, &a.Room, &a.Area, &a.PublishedAt, &a.ExpiresAt} {
		*f = wireUTF8(*f)
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
		return fmt.Errorf("protocol: sign central alert: %w", err)
	}
	a.SignatureB64 = base64.StdEncoding.EncodeToString(sig)
	return nil
}

// Verify checks the author signature: origin_pubkey_b64 is an ed25519 key,
// node_id derives from it, and signature_b64 verifies over SignedPayload.
func (a *CentralAlert) Verify() error {
	if a.OriginPubKeyB64 == "" && a.SignatureB64 == "" {
		return ErrCentralAlertUnsigned
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
		return ErrCentralAlertSignature
	}
	payload, err := a.SignedPayload()
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, payload, sig) {
		return ErrCentralAlertSignature
	}
	return nil
}

// OriginPubKey decodes origin_pubkey_b64.
func (a *CentralAlert) OriginPubKey() (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(a.OriginPubKeyB64)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, ErrCentralOriginPubKey
	}
	return ed25519.PublicKey(b), nil
}

// Validate checks the alert's form and limits for network. It does not
// check the signature (Verify) or expiry (Expired). Text with pipes or
// control characters is refused in every field, as in a listing.
func (a *CentralAlert) Validate(network string) error {
	if a.V3NetCentral != CentralAlertVersion {
		return fmt.Errorf("unsupported v3net_central version %q", a.V3NetCentral)
	}
	if !ValidCentralAlertKind(a.Kind) {
		return fmt.Errorf("unknown alert kind %q", a.Kind)
	}
	if !centralAlertIDRe.MatchString(a.ID) {
		return errors.New("id must be 16 to 32 base64url characters")
	}
	if a.Network != network {
		return fmt.Errorf("network %q does not match %q", a.Network, network)
	}
	if a.NodeID == "" {
		return errors.New("node_id is required")
	}
	if strings.TrimSpace(a.BBS) == "" {
		return errors.New("bbs is required")
	}
	if utf8.RuneCountInString(a.BBS) > MaxBBSNameLen || hasScreenCodes(a.BBS) {
		return fmt.Errorf("bbs must be at most %d characters without pipes or control characters", MaxBBSNameLen)
	}
	if strings.TrimSpace(a.Handle) == "" {
		return errors.New("handle is required")
	}
	if utf8.RuneCountInString(a.Handle) > MaxCentralHandleLen || hasScreenCodes(a.Handle) {
		return fmt.Errorf("handle must be at most %d characters without pipes or control characters", MaxCentralHandleLen)
	}
	switch a.Kind {
	case CentralAlertChatJoin:
		if !centralRoomRe.MatchString(a.Room) {
			return fmt.Errorf("a chat join needs a room of 1 to %d lowercase letters, digits and hyphens", MaxCentralRoomLen)
		}
		if a.Area != "" {
			return errors.New("a chat join carries no area")
		}
	case CentralAlertPost:
		if !AreaTagRegexp.MatchString(a.Area) {
			return errors.New("a post needs a valid area tag")
		}
		if a.Room != "" {
			return errors.New("a post carries no room")
		}
	default:
		if a.Room != "" || a.Area != "" {
			return errors.New("a logon or logoff carries no room or area")
		}
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
	if expires.Sub(published) > MaxCentralAlertTTL {
		return fmt.Errorf("expires_at must be within %s of published_at", MaxCentralAlertTTL)
	}
	return nil
}

// Expired reports whether the alert's expires_at is at or before now. An
// alert whose expiry does not parse counts as expired.
func (a *CentralAlert) Expired(now time.Time) bool {
	t, err := time.Parse(time.RFC3339, a.ExpiresAt)
	return err != nil || !t.After(now)
}

// Published returns published_at, or the zero time.
func (a *CentralAlert) Published() time.Time {
	t, _ := time.Parse(time.RFC3339, a.PublishedAt)
	return t
}

// Expires returns expires_at, or the zero time.
func (a *CentralAlert) Expires() time.Time {
	t, _ := time.Parse(time.RFC3339, a.ExpiresAt)
	return t
}

// CentralAlertResponse is the reply to POST /v3net/v1/{network}/central/alerts.
// Duplicate is set when the hub had already relayed an alert with that ID
// from the board: a retry, which it acknowledges without sending it again.
type CentralAlertResponse struct {
	OK        bool `json:"ok"`
	Duplicate bool `json:"duplicate,omitempty"`
}
