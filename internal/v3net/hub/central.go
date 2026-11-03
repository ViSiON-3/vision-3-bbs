package hub

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// The Central directory on a hub: one row per node and network holding
// the node's latest signed listing as it was sent, so it is served back
// byte for byte and a leaf's signature check covers exactly what the
// board signed. The hub adds only what it knows itself: when the node was
// last seen and whether it is connected now (see protocol.CentralDirectoryEntry).

const centralSchema = `
CREATE TABLE IF NOT EXISTS central_listings (
	node_id      TEXT NOT NULL,
	network      TEXT NOT NULL,
	seq          INTEGER NOT NULL,
	listing      TEXT NOT NULL,
	published_at TEXT NOT NULL,
	expires_at   TEXT NOT NULL,
	last_seen_at TEXT NOT NULL,
	PRIMARY KEY (node_id, network)
);
`

// Central publication limits. A node may refresh its listing a few times
// in a row (a restart, then a correction), but not stream them.
const (
	centralPublishInterval = 10 * time.Second
	centralPublishBurst    = 3
	// centralPruneInterval is how often expired listings are removed; they
	// are also left out of every directory served meanwhile.
	centralPruneInterval = time.Hour
)

// Central store errors.
var (
	// ErrCentralStale is returned by CentralStore.Put when the hub already
	// holds a listing from the node with the same or a higher seq.
	ErrCentralStale = errors.New("hub: listing is not newer than the stored one")
	// ErrCentralFull is returned by CentralStore.Put when the network holds
	// protocol.MaxCentralListings listings and the node has none to replace.
	ErrCentralFull = errors.New("hub: directory is full")
)

// CentralStore keeps Central listings in SQLite.
type CentralStore struct {
	db *sql.DB
	mu sync.Mutex // serializes Put's read-check-write
}

// NewCentralStore creates the listings table if needed.
func NewCentralStore(db *sql.DB) (*CentralStore, error) {
	if _, err := db.Exec(centralSchema); err != nil {
		return nil, fmt.Errorf("hub: create central_listings table: %w", err)
	}
	return &CentralStore{db: db}, nil
}

// Put stores listing, already validated and verified, as the node's entry
// on its network, replacing an older one. raw is the listing's JSON as
// received, which is what List serves. It returns ErrCentralStale if the
// stored listing's seq is not lower, and ErrCentralFull if the network is
// at protocol.MaxCentralListings and this node has no row. now is recorded
// as the node's last-seen time.
//
// The published and expiry stamps are stored in UTC, whatever offset the
// listing wrote them with, so the text comparisons List and PruneExpired
// make are comparisons in time.
func (cs *CentralStore) Put(listing *protocol.CentralListing, raw []byte, now time.Time) error {
	published, err := time.Parse(time.RFC3339, listing.PublishedAt)
	if err != nil {
		return fmt.Errorf("hub: listing published_at: %w", err)
	}
	expires, err := time.Parse(time.RFC3339, listing.ExpiresAt)
	if err != nil {
		return fmt.Errorf("hub: listing expires_at: %w", err)
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()

	var stored int64
	err = cs.db.QueryRow(
		"SELECT seq FROM central_listings WHERE node_id = ? AND network = ?",
		listing.NodeID, listing.Network).Scan(&stored)
	switch {
	case err == nil:
		if listing.Seq <= stored {
			return ErrCentralStale
		}
	case errors.Is(err, sql.ErrNoRows):
		var n int
		if err := cs.db.QueryRow(
			"SELECT COUNT(*) FROM central_listings WHERE network = ?", listing.Network).Scan(&n); err != nil {
			return fmt.Errorf("hub: count listings: %w", err)
		}
		if n >= protocol.MaxCentralListings {
			return ErrCentralFull
		}
	default:
		return fmt.Errorf("hub: read listing: %w", err)
	}

	_, err = cs.db.Exec(
		`INSERT INTO central_listings (node_id, network, seq, listing, published_at, expires_at, last_seen_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(node_id, network) DO UPDATE SET
		   seq = excluded.seq, listing = excluded.listing, published_at = excluded.published_at,
		   expires_at = excluded.expires_at, last_seen_at = excluded.last_seen_at`,
		listing.NodeID, listing.Network, listing.Seq, string(raw),
		published.UTC().Format(time.RFC3339), expires.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("hub: store listing: %w", err)
	}
	return nil
}

// Delete removes the node's listing on network. It reports whether there
// was one.
func (cs *CentralStore) Delete(nodeID, network string) (bool, error) {
	res, err := cs.db.Exec("DELETE FROM central_listings WHERE node_id = ? AND network = ?", nodeID, network)
	if err != nil {
		return false, fmt.Errorf("hub: delete listing: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// CentralRow is one stored listing with the hub's own record of the node:
// the listing's JSON as received, its expiry and the node's last-seen time
// (both RFC 3339 UTC).
type CentralRow struct {
	NodeID     string
	Raw        json.RawMessage
	ExpiresAt  string
	LastSeenAt string
}

// List returns the network's listings that have not expired at now, oldest
// published first, as stored. Expired rows are skipped, not removed; see
// PruneExpired.
func (cs *CentralStore) List(network string, now time.Time) ([]CentralRow, error) {
	rows, err := cs.db.Query(
		`SELECT node_id, listing, expires_at, last_seen_at FROM central_listings
		 WHERE network = ? AND expires_at > ? ORDER BY published_at, node_id`,
		network, now.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("hub: list listings: %w", err)
	}
	defer func() { _ = rows.Close() }() // read-only

	var out []CentralRow
	for rows.Next() {
		var r CentralRow
		var raw string
		if err := rows.Scan(&r.NodeID, &raw, &r.ExpiresAt, &r.LastSeenAt); err != nil {
			return nil, fmt.Errorf("hub: scan listing: %w", err)
		}
		r.Raw = json.RawMessage(raw)
		out = append(out, r)
	}
	return out, rows.Err()
}

// TouchSeen records now as the last-seen time of the node's listing on
// network, if it has one.
func (cs *CentralStore) TouchSeen(nodeID, network string, now time.Time) error {
	_, err := cs.db.Exec(
		"UPDATE central_listings SET last_seen_at = ? WHERE node_id = ? AND network = ?",
		now.UTC().Format(time.RFC3339), nodeID, network)
	if err != nil {
		return fmt.Errorf("hub: touch listing: %w", err)
	}
	return nil
}

// PruneExpired removes every listing whose expires_at is at or before now,
// returning how many went.
func (cs *CentralStore) PruneExpired(now time.Time) (int64, error) {
	res, err := cs.db.Exec("DELETE FROM central_listings WHERE expires_at <= ?", now.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("hub: prune listings: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// StartPruner removes expired listings every centralPruneInterval until ctx
// ends. The first pass runs at once.
func (cs *CentralStore) StartPruner(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(centralPruneInterval)
		defer ticker.Stop()
		for {
			if n, err := cs.PruneExpired(time.Now()); err != nil {
				slog.Warn("hub: prune expired central listings", "error", err)
			} else if n > 0 {
				slog.Info("hub: pruned expired central listings", "count", n)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// onlineNodes counts the open event streams per node and network, so the
// directory can say which boards are connected now. A node may hold more
// than one stream briefly across a reconnect.
type onlineNodes struct {
	mu      sync.Mutex
	streams map[string]int // "nodeID:network" → open streams
}

func newOnlineNodes() *onlineNodes {
	return &onlineNodes{streams: make(map[string]int)}
}

// add records an opened stream.
func (o *onlineNodes) add(nodeID, network string) {
	o.mu.Lock()
	o.streams[nodeID+":"+network]++
	o.mu.Unlock()
}

// remove records a closed stream.
func (o *onlineNodes) remove(nodeID, network string) {
	o.mu.Lock()
	k := nodeID + ":" + network
	if o.streams[k] <= 1 {
		delete(o.streams, k)
	} else {
		o.streams[k]--
	}
	o.mu.Unlock()
}

// online reports whether the node has an event stream open on network.
func (o *onlineNodes) online(nodeID, network string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.streams[nodeID+":"+network] > 0
}
