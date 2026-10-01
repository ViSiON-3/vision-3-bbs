package hub

import (
	"database/sql"
	"fmt"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

const areaSubscriptionSchema = `
CREATE TABLE IF NOT EXISTS area_subscriptions (
	node_id       TEXT NOT NULL,
	network       TEXT NOT NULL,
	area_tag      TEXT NOT NULL,
	status        TEXT DEFAULT 'pending',
	subscribed_at DATETIME DEFAULT (datetime('now')),
	PRIMARY KEY (node_id, network, area_tag)
);
`

// AreaSubscriptionStore manages per-area subscription state.
type AreaSubscriptionStore struct {
	db *sql.DB
}

// NewAreaSubscriptionStore initializes the area_subscriptions table.
func NewAreaSubscriptionStore(db *sql.DB) (*AreaSubscriptionStore, error) {
	if _, err := db.Exec(areaSubscriptionSchema); err != nil {
		return nil, fmt.Errorf("hub: create area_subscriptions table: %w", err)
	}
	return &AreaSubscriptionStore{db: db}, nil
}

// Upsert inserts or updates an area subscription and returns the status now
// stored. An active subscription is never downgraded: re-subscribing must
// not undo a manager's approval. This is decided in the same statement as
// the write, so an approval landing between a caller's read and this write
// is not overwritten.
func (as *AreaSubscriptionStore) Upsert(nodeID, network, areaTag, status string) (string, error) {
	var stored string
	err := as.db.QueryRow(
		`INSERT INTO area_subscriptions (node_id, network, area_tag, status)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(node_id, network, area_tag) DO UPDATE SET status =
		   CASE WHEN area_subscriptions.status = 'active' THEN 'active' ELSE excluded.status END
		 RETURNING status`,
		nodeID, network, areaTag, status,
	).Scan(&stored)
	if err != nil {
		return "", fmt.Errorf("hub: upsert area subscription: %w", err)
	}
	return stored, nil
}

// SetStatus updates the status of a specific subscription.
func (as *AreaSubscriptionStore) SetStatus(nodeID, network, areaTag, status string) error {
	_, err := as.db.Exec(
		`UPDATE area_subscriptions SET status = ? WHERE node_id = ? AND network = ? AND area_tag = ?`,
		status, nodeID, network, areaTag,
	)
	if err != nil {
		return fmt.Errorf("hub: set area subscription status: %w", err)
	}
	return nil
}

// IsActive checks if a node has an active subscription for an area.
func (as *AreaSubscriptionStore) IsActive(nodeID, network, areaTag string) (bool, error) {
	var count int
	err := as.db.QueryRow(
		`SELECT COUNT(*) FROM area_subscriptions
		 WHERE node_id = ? AND network = ? AND area_tag = ? AND status = 'active'`,
		nodeID, network, areaTag,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("hub: check area subscription active: %w", err)
	}
	return count > 0, nil
}

// ListForNode returns all area subscription statuses for a node on a network.
func (as *AreaSubscriptionStore) ListForNode(nodeID, network string) ([]protocol.AreaSubscriptionStatus, error) {
	rows, err := as.db.Query(
		`SELECT area_tag, status FROM area_subscriptions WHERE node_id = ? AND network = ?`,
		nodeID, network,
	)
	if err != nil {
		return nil, fmt.Errorf("hub: list area subscriptions: %w", err)
	}
	defer func() { _ = rows.Close() }() // read-only

	var result []protocol.AreaSubscriptionStatus
	for rows.Next() {
		var s protocol.AreaSubscriptionStatus
		if err := rows.Scan(&s.Tag, &s.Status); err != nil {
			return nil, fmt.Errorf("hub: scan area subscription: %w", err)
		}
		result = append(result, s)
	}
	return result, rows.Err()
}
