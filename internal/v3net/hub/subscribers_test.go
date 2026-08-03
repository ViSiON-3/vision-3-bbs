package hub

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *SubscriberStore {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "subs.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ss, err := NewSubscriberStore(db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return ss
}

func addTestSub(t *testing.T, ss *SubscriberStore, nodeID, status string) {
	t.Helper()
	_, err := ss.Add(Subscriber{
		NodeID: nodeID, Network: "testnet",
		// 32 zero bytes, base64 — decodes to ed25519.PublicKeySize.
		PubKeyB64: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		BBSName:   "BBS " + nodeID, Status: status,
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
}

func TestSubscriberStore_ListReturnsAllForNetwork(t *testing.T) {
	ss := newTestStore(t)
	addTestSub(t, ss, "aaaa000000000001", "active")
	addTestSub(t, ss, "aaaa000000000002", "pending")

	subs, err := ss.List("testnet")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("got %d subscribers, want 2", len(subs))
	}
	if subs[0].CreatedAt == "" {
		t.Error("CreatedAt should be populated by List")
	}
	if empty, err := ss.List("othernet"); err != nil || len(empty) != 0 {
		t.Errorf("othernet: got %d, %v; want 0, nil", len(empty), err)
	}
}

func TestSubscriberStore_SetStatusUpdatesDBAndCache(t *testing.T) {
	ss := newTestStore(t)
	addTestSub(t, ss, "aaaa000000000001", "pending")

	if ss.GetPubKey("aaaa000000000001", "testnet") != nil {
		t.Fatal("pending node should have no auth key")
	}
	if err := ss.SetStatus("aaaa000000000001", "testnet", "active"); err != nil {
		t.Fatalf("set status: %v", err)
	}
	// Cache consistency: auth works immediately, no reload.
	if ss.GetPubKey("aaaa000000000001", "testnet") == nil {
		t.Error("active node should have auth key without store reload")
	}
	// DB consistency: a fresh cache load sees the change.
	if err := ss.loadCache(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := ss.Get("aaaa000000000001", "testnet").Status; got != "active" {
		t.Errorf("persisted status = %q, want active", got)
	}
}

func TestSubscriberStore_SetStatusRejectsUnknownNodeAndBadStatus(t *testing.T) {
	ss := newTestStore(t)
	if err := ss.SetStatus("ffff000000000000", "testnet", "active"); !errors.Is(err, ErrUnknownNode) {
		t.Errorf("unknown node: got %v, want ErrUnknownNode", err)
	}
	addTestSub(t, ss, "aaaa000000000001", "pending")
	if err := ss.SetStatus("aaaa000000000001", "testnet", "sideways"); err == nil {
		t.Error("invalid status should error")
	}
}

func TestSubscriberStore_DeleteRemovesRowAndCache(t *testing.T) {
	ss := newTestStore(t)
	addTestSub(t, ss, "aaaa000000000001", "active")
	if err := ss.Delete("aaaa000000000001", "testnet"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if ss.Get("aaaa000000000001", "testnet") != nil {
		t.Error("cache entry should be gone")
	}
	if err := ss.loadCache(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if ss.Get("aaaa000000000001", "testnet") != nil {
		t.Error("db row should be gone")
	}
	if err := ss.Delete("aaaa000000000001", "testnet"); !errors.Is(err, ErrUnknownNode) {
		t.Errorf("second delete: got %v, want ErrUnknownNode", err)
	}
}
