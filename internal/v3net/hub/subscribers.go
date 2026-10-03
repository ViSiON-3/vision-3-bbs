package hub

import (
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

const subscribersSchema = `
CREATE TABLE IF NOT EXISTS subscribers (
	node_id    TEXT NOT NULL,
	network    TEXT NOT NULL,
	pubkey_b64 TEXT NOT NULL,
	bbs_name   TEXT,
	bbs_host   TEXT,
	status     TEXT NOT NULL DEFAULT 'pending',
	created_at DATETIME DEFAULT (datetime('now')),
	PRIMARY KEY (node_id, network)
);
`

// requestedAreasMigration adds the column holding the area tags a node asked
// for when it first registered (see Subscriber.RequestedAreas).
const requestedAreasMigration = `ALTER TABLE subscribers ADD COLUMN requested_area_tags TEXT NOT NULL DEFAULT '[]'`

// ErrUnknownNode is returned by SetStatus and Delete when no subscriber
// row matches the node/network pair.
var ErrUnknownNode = errors.New("hub: unknown node")

// Subscriber represents a registered leaf node.
type Subscriber struct {
	NodeID    string
	Network   string
	PubKeyB64 string
	BBSName   string
	BBSHost   string
	Status    string // "active", "pending", "banned"
	CreatedAt string // populated by List only

	// RequestedAreas preserves the first registration's area tags for database
	// compatibility with older hubs. Current hubs require signed re-subscription.
	RequestedAreas []string
}

// SubscriberStore manages leaf node subscriptions with SQLite persistence
// and an in-memory cache for fast auth lookups.
type SubscriberStore struct {
	db    *sql.DB
	mu    sync.RWMutex
	cache map[string]*Subscriber // key: "nodeID:network"
}

// NewSubscriberStore initializes the subscribers table and loads existing
// records into an in-memory cache.
func NewSubscriberStore(db *sql.DB) (*SubscriberStore, error) {
	if _, err := db.Exec(subscribersSchema); err != nil {
		return nil, fmt.Errorf("hub: create subscribers table: %w", err)
	}
	if _, err := db.Exec(requestedAreasMigration); err != nil {
		// Ignore "duplicate column" — migration already applied.
		if !strings.Contains(err.Error(), "duplicate column") {
			return nil, fmt.Errorf("hub: migrate add requested_area_tags column: %w", err)
		}
	}

	ss := &SubscriberStore{
		db:    db,
		cache: make(map[string]*Subscriber),
	}
	if err := ss.loadCache(); err != nil {
		return nil, err
	}
	return ss, nil
}

func (ss *SubscriberStore) loadCache() error {
	ss.cache = make(map[string]*Subscriber)
	rows, err := ss.db.Query("SELECT node_id, network, pubkey_b64, COALESCE(bbs_name, ''), COALESCE(bbs_host, ''), status, requested_area_tags FROM subscribers")
	if err != nil {
		return fmt.Errorf("hub: load subscribers: %w", err)
	}
	defer func() { _ = rows.Close() }() // read-only

	for rows.Next() {
		var s Subscriber
		var requested string
		if err := rows.Scan(&s.NodeID, &s.Network, &s.PubKeyB64, &s.BBSName, &s.BBSHost, &s.Status, &requested); err != nil {
			return fmt.Errorf("hub: scan subscriber: %w", err)
		}
		// A corrupt value only loses the stored tags, so load the node anyway.
		_ = json.Unmarshal([]byte(requested), &s.RequestedAreas)
		ss.cache[s.NodeID+":"+s.Network] = &s
	}
	return rows.Err()
}

// Add registers a new subscriber. Returns the status assigned.
func (ss *SubscriberStore) Add(s Subscriber) (string, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	requested, err := json.Marshal(s.RequestedAreas)
	if err != nil {
		return "", fmt.Errorf("hub: encode requested areas: %w", err)
	}
	if s.RequestedAreas == nil {
		requested = []byte("[]")
	}
	result, err := ss.db.Exec(
		`INSERT OR IGNORE INTO subscribers (node_id, network, pubkey_b64, bbs_name, bbs_host, status, requested_area_tags)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		s.NodeID, s.Network, s.PubKeyB64, s.BBSName, s.BBSHost, s.Status, string(requested),
	)
	if err != nil {
		return "", fmt.Errorf("hub: add subscriber: %w", err)
	}

	// Only update cache if the row was actually inserted (not ignored).
	if n, err := result.RowsAffected(); err == nil && n > 0 {
		s.RequestedAreas = slices.Clone(s.RequestedAreas)
		ss.cache[s.NodeID+":"+s.Network] = &s
	}

	// If the insert was ignored, the existing row's status applies.
	if existing := ss.cache[s.NodeID+":"+s.Network]; existing != nil {
		return existing.Status, nil
	}
	return s.Status, nil
}

// Get returns a copy of the cached subscriber by node ID and network, or nil
// if not found. A copy is returned (rather than the cache pointer) so that
// SetStatus mutating the cached entry under the write lock can never race
// with a caller reading the returned value after this call returns.
func (ss *SubscriberStore) Get(nodeID, network string) *Subscriber {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	s, ok := ss.cache[nodeID+":"+network]
	if !ok {
		return nil
	}
	cp := *s
	return &cp
}

// GetPubKey returns the decoded ed25519 public key for an active subscriber,
// or nil if the subscriber is not found or not active.
func (ss *SubscriberStore) GetPubKey(nodeID, network string) ed25519.PublicKey {
	s := ss.Get(nodeID, network)
	if s == nil || s.Status != "active" {
		return nil
	}
	key, err := base64.StdEncoding.DecodeString(s.PubKeyB64)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil
	}
	return key
}

// ActiveCount returns the number of active subscribers for a network.
func (ss *SubscriberStore) ActiveCount(network string) int {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	count := 0
	for _, s := range ss.cache {
		if s.Network == network && s.Status == "active" {
			count++
		}
	}
	return count
}

// List returns all subscribers for a network ordered by registration time.
// Rows are read from the DB (created_at is not cached) and returned as
// copies, never cache pointers.
func (ss *SubscriberStore) List(network string) ([]Subscriber, error) {
	rows, err := ss.db.Query(
		`SELECT node_id, network, pubkey_b64, COALESCE(bbs_name, ''),
		        COALESCE(bbs_host, ''), status, created_at
		 FROM subscribers WHERE network = ? ORDER BY created_at`, network)
	if err != nil {
		return nil, fmt.Errorf("hub: list subscribers: %w", err)
	}
	defer func() { _ = rows.Close() }() // read-only

	var subs []Subscriber
	for rows.Next() {
		var s Subscriber
		if err := rows.Scan(&s.NodeID, &s.Network, &s.PubKeyB64,
			&s.BBSName, &s.BBSHost, &s.Status, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("hub: scan subscriber: %w", err)
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

// SetStatus updates a subscriber's status in the DB and cache together.
func (ss *SubscriberStore) SetStatus(nodeID, network, status string) error {
	switch status {
	case "active", "pending", "banned":
	default:
		return fmt.Errorf("hub: invalid status %q", status)
	}

	ss.mu.Lock()
	defer ss.mu.Unlock()

	res, err := ss.db.Exec(
		"UPDATE subscribers SET status = ? WHERE node_id = ? AND network = ?",
		status, nodeID, network)
	if err != nil {
		return fmt.Errorf("hub: set status: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("%w: %s on %s", ErrUnknownNode, nodeID, network)
	}
	if s := ss.cache[nodeID+":"+network]; s != nil {
		s.Status = status
	}
	return nil
}

// SetProfile updates a subscriber's BBS name and host in the DB and cache
// together. Add never changes an existing row, so this is how a row's
// details are corrected; callers must only pass a node ID they have
// authenticated.
func (ss *SubscriberStore) SetProfile(nodeID, network, bbsName, bbsHost string) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	res, err := ss.db.Exec(
		"UPDATE subscribers SET bbs_name = ?, bbs_host = ? WHERE node_id = ? AND network = ?",
		bbsName, bbsHost, nodeID, network)
	if err != nil {
		return fmt.Errorf("hub: set profile: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("%w: %s on %s", ErrUnknownNode, nodeID, network)
	}
	if s := ss.cache[nodeID+":"+network]; s != nil {
		s.BBSName = bbsName
		s.BBSHost = bbsHost
	}
	return nil
}

// SetProfileUnlessBanned is SetProfile for a node updating its own
// details: it leaves a banned node's row unchanged, checking the status in
// the same statement as the update so a ban landing concurrently is never
// overridden. It reports whether the row was updated.
func (ss *SubscriberStore) SetProfileUnlessBanned(nodeID, network, bbsName, bbsHost string) (bool, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	res, err := ss.db.Exec(
		"UPDATE subscribers SET bbs_name = ?, bbs_host = ? WHERE node_id = ? AND network = ? AND status != 'banned'",
		bbsName, bbsHost, nodeID, network)
	if err != nil {
		return false, fmt.Errorf("hub: set profile: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("hub: set profile: %w", err)
	}
	if n == 0 {
		return false, nil
	}
	if s := ss.cache[nodeID+":"+network]; s != nil {
		s.BBSName = bbsName
		s.BBSHost = bbsHost
	}
	return true, nil
}

// Delete removes a subscriber registration from the DB and cache.
func (ss *SubscriberStore) Delete(nodeID, network string) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	res, err := ss.db.Exec(
		"DELETE FROM subscribers WHERE node_id = ? AND network = ?", nodeID, network)
	if err != nil {
		return fmt.Errorf("hub: delete subscriber: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("%w: %s on %s", ErrUnknownNode, nodeID, network)
	}
	delete(ss.cache, nodeID+":"+network)
	return nil
}
