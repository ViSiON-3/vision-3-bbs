// Package nal provides NAL (Network Area List) signing, verification, fetching,
// and caching for the V3Net protocol.
package nal

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// Sentinel errors.
var (
	ErrNetworkNotCached = errors.New("nal: network not in cache")
	ErrAreaNotFound     = errors.New("nal: area not found")
	ErrInvalidSignature = errors.New("nal: signature verification failed")
	ErrInvalidPubKey    = errors.New("nal: invalid coordinator public key")
)

// Canonical field order for NAL signing. The NAL JSON is marshalled with
// signature_b64 set to "" and fields in this fixed alphabetical order:
//
//	areas, coordinator_node_id, coordinator_pubkey_b64, network, updated, v3net_nal
//
// Within each Area, fields are ordered alphabetically:
//
//	access, description, language, manager_node_id, manager_pubkey_b64, moderated, name, policy, tag
//
// AreaAccess: allow_list, deny_list, mode
// AreaPolicy: allow_ansi, max_body_bytes, require_tearline

// canonicalNAL is the struct used for deterministic JSON marshalling during
// signing. It has signature_b64 always set to empty string and uses the same
// JSON tags as protocol.NAL but with a fixed field order enforced by the
// struct definition order matching alphabetical key sort.
type canonicalNAL struct {
	Areas          []canonicalArea `json:"areas"`
	CoordNodeID    string          `json:"coordinator_node_id"`
	CoordPubKeyB64 string          `json:"coordinator_pubkey_b64"`
	Network        string          `json:"network"`
	SignatureB64   string          `json:"signature_b64"`
	Updated        string          `json:"updated"`
	V3NetNAL       string          `json:"v3net_nal"`
}

type canonicalArea struct {
	Access           canonicalAccess `json:"access"`
	Description      string          `json:"description"`
	Language         string          `json:"language"`
	ManagerNodeID    string          `json:"manager_node_id"`
	ManagerPubKeyB64 string          `json:"manager_pubkey_b64"`
	Moderated        bool            `json:"moderated"`
	Name             string          `json:"name"`
	Policy           canonicalPolicy `json:"policy"`
	Tag              string          `json:"tag"`
}

type canonicalAccess struct {
	AllowList []string `json:"allow_list"`
	DenyList  []string `json:"deny_list"`
	Mode      string   `json:"mode"`
}

type canonicalPolicy struct {
	AllowANSI       bool `json:"allow_ansi"`
	MaxBodyBytes    int  `json:"max_body_bytes"`
	RequireTearline bool `json:"require_tearline"`
}

// toCanonical converts a protocol.NAL to the canonical form for signing.
func toCanonical(n *protocol.NAL) canonicalNAL {
	areas := make([]canonicalArea, len(n.Areas))
	for i, a := range n.Areas {
		allowList := a.Access.AllowList
		if allowList == nil {
			allowList = []string{}
		}
		denyList := a.Access.DenyList
		if denyList == nil {
			denyList = []string{}
		}
		areas[i] = canonicalArea{
			Access: canonicalAccess{
				AllowList: allowList,
				DenyList:  denyList,
				Mode:      a.Access.Mode,
			},
			Description:      a.Description,
			Language:         a.Language,
			ManagerNodeID:    a.ManagerNodeID,
			ManagerPubKeyB64: a.ManagerPubKeyB64,
			Moderated:        a.Moderated,
			Name:             a.Name,
			Policy: canonicalPolicy{
				AllowANSI:       a.Policy.AllowANSI,
				MaxBodyBytes:    a.Policy.MaxBodyBytes,
				RequireTearline: a.Policy.RequireTearline,
			},
			Tag: a.Tag,
		}
	}

	return canonicalNAL{
		Areas:          areas,
		CoordNodeID:    n.CoordNodeID,
		CoordPubKeyB64: n.CoordPubKeyB64,
		Network:        n.Network,
		SignatureB64:   "",
		Updated:        n.Updated,
		V3NetNAL:       n.V3NetNAL,
	}
}

// canonicalBytes returns the deterministic JSON encoding for signing.
func canonicalBytes(n *protocol.NAL) ([]byte, error) {
	c := toCanonical(n)
	return json.Marshal(c)
}

// Sign populates CoordPubKeyB64 and Updated on the NAL, then computes and
// sets SignatureB64 using the provided keystore.
func Sign(n *protocol.NAL, ks *keystore.Keystore) error {
	n.CoordPubKeyB64 = ks.PubKeyBase64()
	n.CoordNodeID = ks.NodeID()
	n.Updated = time.Now().UTC().Format("2006-01-02")
	n.SignatureB64 = ""

	payload, err := canonicalBytes(n)
	if err != nil {
		return fmt.Errorf("nal: marshal canonical: %w", err)
	}

	sigBytes, err := ks.SignRaw(payload)
	if err != nil {
		return fmt.Errorf("nal: sign: %w", err)
	}

	n.SignatureB64 = base64.StdEncoding.EncodeToString(sigBytes)
	return nil
}

// Verify checks that the NAL's SignatureB64 is a valid ed25519 signature
// over the canonical payload using CoordPubKeyB64.
func Verify(n *protocol.NAL) error {
	pubKeyBytes, err := base64.StdEncoding.DecodeString(n.CoordPubKeyB64)
	if err != nil || len(pubKeyBytes) != ed25519.PublicKeySize {
		return ErrInvalidPubKey
	}

	sigBytes, err := base64.StdEncoding.DecodeString(n.SignatureB64)
	if err != nil {
		return fmt.Errorf("nal: decode signature: %w", err)
	}

	// Build canonical with signature cleared.
	saved := n.SignatureB64
	n.SignatureB64 = ""
	payload, err := canonicalBytes(n)
	n.SignatureB64 = saved
	if err != nil {
		return fmt.Errorf("nal: marshal canonical: %w", err)
	}

	if !ed25519.Verify(pubKeyBytes, payload, sigBytes) {
		return ErrInvalidSignature
	}
	return nil
}

// Fetch retrieves a NAL from a hub's /nal endpoint. Does not verify.
func Fetch(ctx context.Context, url string) (*protocol.NAL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("nal: create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nal: fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nal: fetch status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return nil, fmt.Errorf("nal: read body: %w", err)
	}

	var n protocol.NAL
	if err := json.Unmarshal(body, &n); err != nil {
		return nil, fmt.Errorf("nal: decode: %w", err)
	}
	return &n, nil
}

// Cache provides TTL-based caching of verified NALs.
type Cache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry // network → entry
	ttl     time.Duration
}

type cacheEntry struct {
	nal       *protocol.NAL
	fetchedAt time.Time
}

// NewCache creates a new NAL cache with the given TTL.
func NewCache(ttl time.Duration) *Cache {
	return &Cache{
		entries: make(map[string]*cacheEntry),
		ttl:     ttl,
	}
}

// Put stores a verified NAL in the cache.
func (c *Cache) Put(network string, n *protocol.NAL) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[network] = &cacheEntry{nal: n, fetchedAt: time.Now()}
}

// Get returns a cached NAL if present and not expired.
func (c *Cache) Get(network string) *protocol.NAL {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e := c.entries[network]
	if e == nil {
		return nil
	}
	return e.nal
}

// FetchAndVerify fetches a NAL from the given URL, verifies it, and caches it.
// On fetch failure with a warm cache, returns the stale entry with a logged warning.
func (c *Cache) FetchAndVerify(ctx context.Context, url, network string) (*protocol.NAL, error) {
	// Check if cache is fresh.
	c.mu.RLock()
	e := c.entries[network]
	c.mu.RUnlock()

	if e != nil && time.Since(e.fetchedAt) < c.ttl {
		return e.nal, nil
	}

	// Fetch and verify.
	n, err := Fetch(ctx, url)
	if err != nil {
		if e != nil {
			slog.Warn("nal: fetch failed, returning stale cache", "network", network, "error", err)
			return e.nal, nil
		}
		return nil, fmt.Errorf("nal: fetch %s: %w", network, err)
	}

	if err := Verify(n); err != nil {
		if e != nil {
			slog.Warn("nal: verification failed, returning stale cache", "network", network, "error", err)
			return e.nal, nil
		}
		return nil, fmt.Errorf("nal: verify %s: %w", network, err)
	}

	c.Put(network, n)
	return n, nil
}

// Area returns the area with the given tag from the cached NAL.
func (c *Cache) Area(network, tag string) (*protocol.Area, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	e := c.entries[network]
	if e == nil {
		return nil, ErrNetworkNotCached
	}

	a := e.nal.FindArea(tag)
	if a == nil {
		return nil, ErrAreaNotFound
	}
	return a, nil
}

// NodeAllowed evaluates whether a node is permitted to access an area.
// DenyList is checked first (always enforced), then Mode + AllowList.
func (c *Cache) NodeAllowed(network, tag, nodeID string) (bool, error) {
	area, err := c.Area(network, tag)
	if err != nil {
		return false, err
	}
	return NodeAllowed(area, nodeID), nil
}

// NodeAllowed checks if a node is permitted to access an area based on its
// access configuration. DenyList is checked first, then Mode + AllowList.
func NodeAllowed(area *protocol.Area, nodeID string) bool {
	// DenyList always takes precedence.
	for _, id := range area.Access.DenyList {
		if id == nodeID {
			return false
		}
	}

	switch area.Access.Mode {
	case protocol.AccessModeOpen:
		return true
	case protocol.AccessModeApproval, protocol.AccessModeClosed:
		for _, id := range area.Access.AllowList {
			if id == nodeID {
				return true
			}
		}
		return false
	default:
		return false
	}
}
