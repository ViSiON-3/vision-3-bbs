package protocol

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
)

// V3Net Central board roster (#998). Every Central listing, activity
// snapshot and alert is signed by the board it is about, so a hub cannot
// alter one or forge one in another board's name. But a board is known only
// by its key, and nothing else on a network vouches for keys: a hub that set
// out to deceive could mint a key and invent a board of its own.
//
// The roster closes that gap with the one key on a network a hub does not
// hold: the coordinator's, which every board pins when it first verifies the
// network's NAL. The coordinator signs the list of boards (node ID and
// public key) admitted to the network's Central features. A board that has
// verified a roster shows Central listings, activity and alerts only from
// boards on it, matched on the full public key, and keeps the roster it
// verified, so a hub that later withholds it or serves an older one changes
// nothing. A network whose coordinator never publishes a roster works as
// before.
//
// It is a document of its own rather than part of the NAL, so boards that
// predate it, which verify the NAL's signed fields as they know them, are
// not affected.

// FeatureCentralRoster is the board roster: the hub stores the
// coordinator's roster and serves it to the network's boards.
const FeatureCentralRoster = "central.roster"

// CentralRosterVersion is the value of CentralRoster.V3NetCentral this
// code writes. Another major version is not understood.
const CentralRosterVersion = "1.0"

// CentralRosterSignatureDomain prefixes the canonical JSON a roster
// signature covers (see CentralSignatureDomain).
const CentralRosterSignatureDomain = "V3NET-CENTRAL-ROSTER-v1\n"

// EventCentralRosterUpdated says the coordinator published a new roster.
// Leaves fetch it rather than trusting the event. Its data is a
// CentralRosterEventPayload.
const EventCentralRosterUpdated = "central_roster_updated"

// CentralRosterEventPayload is the data of a central_roster_updated event.
type CentralRosterEventPayload struct {
	EventMeta
	Network string `json:"network"`
	Seq     int64  `json:"seq"`
}

// MaxCentralRosterMembers is the most boards one roster may name: as many
// as a hub holds listings.
const MaxCentralRosterMembers = MaxCentralListings

// Roster errors.
var (
	// ErrCentralRosterUnsigned is returned by CentralRoster.Verify for a
	// roster with no signature.
	ErrCentralRosterUnsigned = errors.New("protocol: central roster is unsigned")
	// ErrCentralRosterSignature is returned when the signature does not
	// decode or does not verify.
	ErrCentralRosterSignature = errors.New("protocol: central roster signature verification failed")
)

// CentralRosterMember is one admitted board: its node ID and the public key
// (standard base64) the node ID derives from.
type CentralRosterMember struct {
	NodeID    string `json:"node_id"`
	PubKeyB64 string `json:"pubkey_b64"`
}

// CentralRoster is a network's board roster, written and signed by its
// coordinator. Seq orders one coordinator's rosters: a higher one replaces a
// lower one, whole; a new coordinator, after a handover, starts its own.
// PrevSeq is the seq of the roster this one was edited from (0 for the
// coordinator's first): the hub takes it only while that is still the
// coordinator's stored roster, so two edits made from the same roster
// cannot silently undo each other. Every field but SignatureB64 is covered.
type CentralRoster struct {
	V3NetCentral   string `json:"v3net_central"`
	Network        string `json:"network"`
	CoordNodeID    string `json:"coordinator_node_id"`
	CoordPubKeyB64 string `json:"coordinator_pubkey_b64"`
	Seq            int64  `json:"seq"`
	PrevSeq        int64  `json:"prev_seq"`
	// Open says the coordinator has opened the network again: boards that
	// take this roster stop filtering by it, and show every board, while
	// keeping it so that no older, filtering roster can be put back.
	// Members stay as the coordinator left them, for closing it again.
	Open         bool                  `json:"open,omitempty"`
	Members      []CentralRosterMember `json:"members"`
	PublishedAt  string                `json:"published_at"`
	SignatureB64 string                `json:"signature_b64,omitempty"`
}

// canonicalCentralRoster and canonicalRosterMember are the signed forms,
// keys in alphabetical order and nothing omitted.
type canonicalRosterMember struct {
	NodeID    string `json:"node_id"`
	PubKeyB64 string `json:"pubkey_b64"`
}

type canonicalCentralRoster struct {
	CoordNodeID    string                  `json:"coordinator_node_id"`
	CoordPubKeyB64 string                  `json:"coordinator_pubkey_b64"`
	Members        []canonicalRosterMember `json:"members"`
	Network        string                  `json:"network"`
	Open           bool                    `json:"open,omitempty"` // only when set, so a closed roster signs as before
	PrevSeq        int64                   `json:"prev_seq"`
	PublishedAt    string                  `json:"published_at"`
	Seq            int64                   `json:"seq"`
	V3NetCentral   string                  `json:"v3net_central"`
}

// SignedPayload returns the exact bytes the roster signature covers:
// CentralRosterSignatureDomain and the compact JSON of every field but
// signature_b64, keys sorted, HTML escaping off.
func (r *CentralRoster) SignedPayload() ([]byte, error) {
	members := make([]canonicalRosterMember, len(r.Members))
	for i, m := range r.Members {
		members[i] = canonicalRosterMember(m)
	}
	canon := canonicalCentralRoster{
		CoordNodeID:    r.CoordNodeID,
		CoordPubKeyB64: r.CoordPubKeyB64,
		Members:        members,
		Network:        r.Network,
		Open:           r.Open,
		PrevSeq:        r.PrevSeq,
		PublishedAt:    r.PublishedAt,
		Seq:            r.Seq,
		V3NetCentral:   r.V3NetCentral,
	}
	var buf bytes.Buffer
	buf.WriteString(CentralRosterSignatureDomain)
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(canon); err != nil {
		return nil, fmt.Errorf("protocol: marshal central roster payload: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// Sign sets the signer, the coordinator, as the roster's author and signs
// it. Members are sorted by node ID first, so the same roster always signs
// the same way.
func (r *CentralRoster) Sign(s MessageSigner) error {
	slices.SortFunc(r.Members, func(a, b CentralRosterMember) int {
		switch {
		case a.NodeID < b.NodeID:
			return -1
		case a.NodeID > b.NodeID:
			return 1
		}
		return 0
	})
	if r.Members == nil {
		r.Members = []CentralRosterMember{}
	}
	r.CoordNodeID = s.NodeID()
	r.CoordPubKeyB64 = s.PubKeyBase64()
	r.SignatureB64 = ""
	payload, err := r.SignedPayload()
	if err != nil {
		return err
	}
	sig, err := s.SignRaw(payload)
	if err != nil {
		return fmt.Errorf("protocol: sign central roster: %w", err)
	}
	r.SignatureB64 = base64.StdEncoding.EncodeToString(sig)
	return nil
}

// Verify checks the coordinator's signature: coordinator_pubkey_b64 is an
// ed25519 key, coordinator_node_id derives from it, and signature_b64
// verifies over SignedPayload. Whether that key is the network's
// coordinator is for the caller to check against the key it trusts.
func (r *CentralRoster) Verify() error {
	if r.CoordPubKeyB64 == "" && r.SignatureB64 == "" {
		return ErrCentralRosterUnsigned
	}
	pub, err := decodeEd25519Key(r.CoordPubKeyB64)
	if err != nil {
		return err
	}
	if keystore.NodeIDFromPubKey(pub) != r.CoordNodeID {
		return ErrCentralNodeMismatch
	}
	sig, err := base64.StdEncoding.DecodeString(r.SignatureB64)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return ErrCentralRosterSignature
	}
	payload, err := r.SignedPayload()
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, payload, sig) {
		return ErrCentralRosterSignature
	}
	return nil
}

// decodeEd25519Key decodes a standard base64 ed25519 public key.
func decodeEd25519Key(b64 string) (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, ErrCentralOriginPubKey
	}
	return ed25519.PublicKey(b), nil
}

// Validate checks the roster's form for network: version, a positive seq, a
// publication time, and at most MaxCentralRosterMembers members, each named
// once, whose node ID derives from its key. It does not check the signature.
func (r *CentralRoster) Validate(network string) error {
	if r.V3NetCentral != CentralRosterVersion {
		return fmt.Errorf("unsupported v3net_central version %q", r.V3NetCentral)
	}
	if r.Network != network {
		return fmt.Errorf("network %q does not match %q", r.Network, network)
	}
	if r.Seq <= 0 {
		return errors.New("seq must be positive")
	}
	if r.PrevSeq < 0 || r.PrevSeq >= r.Seq {
		return errors.New("prev_seq must be from 0 up to below seq")
	}
	if _, err := time.Parse(time.RFC3339, r.PublishedAt); err != nil {
		return errors.New("published_at must be RFC 3339")
	}
	if len(r.Members) > MaxCentralRosterMembers {
		return fmt.Errorf("at most %d members", MaxCentralRosterMembers)
	}
	seen := make(map[string]bool, len(r.Members))
	for _, m := range r.Members {
		pub, err := decodeEd25519Key(m.PubKeyB64)
		if err != nil {
			return fmt.Errorf("member %s: bad public key", m.NodeID)
		}
		if keystore.NodeIDFromPubKey(pub) != m.NodeID {
			return fmt.Errorf("member %s: node ID does not derive from its key", m.NodeID)
		}
		if seen[m.NodeID] {
			return fmt.Errorf("member %s named twice", m.NodeID)
		}
		seen[m.NodeID] = true
	}
	return nil
}

// Admits reports whether the roster admits the board with this public key
// (standard base64) and node ID: it names that board with that key, or it
// is open.
func (r *CentralRoster) Admits(nodeID, pubKeyB64 string) bool {
	if r.Open {
		return true
	}
	for _, m := range r.Members {
		if m.NodeID == nodeID && m.PubKeyB64 == pubKeyB64 {
			return true
		}
	}
	return false
}

// CentralRosterMemberFor returns the roster entry for a public key
// (standard base64), with its node ID derived, or an error for a key that
// is not an ed25519 key.
func CentralRosterMemberFor(pubKeyB64 string) (CentralRosterMember, error) {
	pub, err := decodeEd25519Key(pubKeyB64)
	if err != nil {
		return CentralRosterMember{}, err
	}
	return CentralRosterMember{NodeID: keystore.NodeIDFromPubKey(pub), PubKeyB64: pubKeyB64}, nil
}
