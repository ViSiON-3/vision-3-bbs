package hub

import (
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"fmt"
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

// Subscriber represents a registered leaf node.
type Subscriber struct {
	NodeID    string
	Network   string
	PubKeyB64 string
	BBSName   string
	BBSHost   string
	Status    string // "active", "pending", "banned"
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
	rows, err := ss.db.Query("SELECT node_id, network, pubkey_b64, COALESCE(bbs_name, ''), COALESCE(bbs_host, ''), status FROM subscribers")
	if err != nil {
		return fmt.Errorf("hub: load subscribers: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var s Subscriber
		if err := rows.Scan(&s.NodeID, &s.Network, &s.PubKeyB64, &s.BBSName, &s.BBSHost, &s.Status); err != nil {
			return fmt.Errorf("hub: scan subscriber: %w", err)
		}
		ss.cache[s.NodeID+":"+s.Network] = &s
	}
	return rows.Err()
}

// Add registers a new subscriber. Returns the status assigned.
func (ss *SubscriberStore) Add(s Subscriber) (string, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	result, err := ss.db.Exec(
		`INSERT OR IGNORE INTO subscribers (node_id, network, pubkey_b64, bbs_name, bbs_host, status)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		s.NodeID, s.Network, s.PubKeyB64, s.BBSName, s.BBSHost, s.Status,
	)
	if err != nil {
		return "", fmt.Errorf("hub: add subscriber: %w", err)
	}

	// Only update cache if the row was actually inserted (not ignored).
	if n, err := result.RowsAffected(); err == nil && n > 0 {
		ss.cache[s.NodeID+":"+s.Network] = &s
	}

	// If the insert was ignored, the existing row's status applies.
	if existing := ss.cache[s.NodeID+":"+s.Network]; existing != nil {
		return existing.Status, nil
	}
	return s.Status, nil
}

// Get returns a subscriber by node ID and network, or nil if not found.
func (ss *SubscriberStore) Get(nodeID, network string) *Subscriber {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.cache[nodeID+":"+network]
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
