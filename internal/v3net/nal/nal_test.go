package nal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

func testKeystore(t *testing.T) *keystore.Keystore {
	t.Helper()
	dir := t.TempDir()
	ks, _, err := keystore.Load(filepath.Join(dir, "test.key"))
	if err != nil {
		t.Fatalf("load keystore: %v", err)
	}
	return ks
}

func testNAL(ks *keystore.Keystore) *protocol.NAL {
	return &protocol.NAL{
		V3NetNAL:    "1.0",
		Network:     "testnet",
		CoordNodeID: ks.NodeID(),
		Areas: []protocol.Area{
			{
				Tag:              "test.general",
				Name:             "General",
				Description:      "General discussion",
				Language:         "en",
				ManagerNodeID:    ks.NodeID(),
				ManagerPubKeyB64: ks.PubKeyBase64(),
				Access: protocol.AreaAccess{
					Mode: protocol.AccessModeOpen,
				},
				Policy: protocol.AreaPolicy{
					MaxBodyBytes: 32768,
					AllowANSI:    true,
				},
			},
		},
	}
}

func TestSignVerifyRoundTrip(t *testing.T) {
	ks := testKeystore(t)
	n := testNAL(ks)

	if err := Sign(n, ks); err != nil {
		t.Fatalf("sign: %v", err)
	}

	if n.SignatureB64 == "" {
		t.Fatal("signature should be non-empty after signing")
	}
	if n.CoordPubKeyB64 == "" {
		t.Fatal("coord pubkey should be set after signing")
	}
	if n.Updated == "" {
		t.Fatal("updated date should be set after signing")
	}

	if err := Verify(n); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestVerifyFailsOnModification(t *testing.T) {
	ks := testKeystore(t)
	n := testNAL(ks)

	if err := Sign(n, ks); err != nil {
		t.Fatalf("sign: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(n *protocol.NAL)
	}{
		{"modify network", func(n *protocol.NAL) { n.Network = "hacked" }},
		{"modify area name", func(n *protocol.NAL) { n.Areas[0].Name = "Hacked" }},
		{"modify area tag", func(n *protocol.NAL) { n.Areas[0].Tag = "test.hacked" }},
		{"modify access mode", func(n *protocol.NAL) { n.Areas[0].Access.Mode = "closed" }},
		{"modify updated", func(n *protocol.NAL) { n.Updated = "2099-01-01" }},
		{"add to deny list", func(n *protocol.NAL) { n.Areas[0].Access.DenyList = []string{"evil"} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a deep copy via JSON round-trip.
			data, _ := json.Marshal(n)
			var copy protocol.NAL
			json.Unmarshal(data, &copy)

			tt.mutate(&copy)
			if err := Verify(&copy); err == nil {
				t.Error("expected verification to fail after modification")
			}
		})
	}
}

func TestVerifyFailsWithDifferentKeypair(t *testing.T) {
	ks1 := testKeystore(t)
	ks2 := testKeystore(t)

	n := testNAL(ks1)
	if err := Sign(n, ks1); err != nil {
		t.Fatalf("sign: %v", err)
	}

	// Replace pubkey with a different one but keep the signature.
	n.CoordPubKeyB64 = ks2.PubKeyBase64()
	if err := Verify(n); err == nil {
		t.Error("expected verification to fail with different keypair")
	}
}

func TestNodeAllowed_OpenArea(t *testing.T) {
	area := &protocol.Area{
		Access: protocol.AreaAccess{Mode: protocol.AccessModeOpen},
	}

	if !NodeAllowed(area, "anynode") {
		t.Error("open area should allow any node")
	}
}

func TestNodeAllowed_OpenAreaDenyList(t *testing.T) {
	area := &protocol.Area{
		Access: protocol.AreaAccess{
			Mode:     protocol.AccessModeOpen,
			DenyList: []string{"banned"},
		},
	}

	if NodeAllowed(area, "banned") {
		t.Error("deny list should block even in open mode")
	}
	if !NodeAllowed(area, "allowed") {
		t.Error("non-denied node should be allowed in open mode")
	}
}

func TestNodeAllowed_ApprovalAreaDeniesUnlisted(t *testing.T) {
	area := &protocol.Area{
		Access: protocol.AreaAccess{
			Mode:      protocol.AccessModeApproval,
			AllowList: []string{"approved1"},
		},
	}

	if NodeAllowed(area, "random") {
		t.Error("approval area should deny node not on allow list")
	}
	if !NodeAllowed(area, "approved1") {
		t.Error("approval area should allow node on allow list")
	}
}

func TestNodeAllowed_DenyListOverridesAllowList(t *testing.T) {
	area := &protocol.Area{
		Access: protocol.AreaAccess{
			Mode:      protocol.AccessModeApproval,
			AllowList: []string{"node1"},
			DenyList:  []string{"node1"},
		},
	}

	if NodeAllowed(area, "node1") {
		t.Error("deny list should override allow list")
	}
}

func TestNodeAllowed_ClosedArea(t *testing.T) {
	area := &protocol.Area{
		Access: protocol.AreaAccess{
			Mode:      protocol.AccessModeClosed,
			AllowList: []string{"explicit"},
		},
	}

	if NodeAllowed(area, "random") {
		t.Error("closed area should deny unlisted node")
	}
	if !NodeAllowed(area, "explicit") {
		t.Error("closed area should allow explicitly listed node")
	}
}

func TestCacheFetchAndVerify(t *testing.T) {
	ks := testKeystore(t)
	n := testNAL(ks)
	if err := Sign(n, ks); err != nil {
		t.Fatalf("sign: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(n)
	}))
	defer srv.Close()

	cache := NewCache(1 * time.Hour)
	ctx := context.Background()

	got, err := cache.FetchAndVerify(ctx, srv.URL, "testnet")
	if err != nil {
		t.Fatalf("FetchAndVerify: %v", err)
	}
	if got.Network != "testnet" {
		t.Errorf("expected network testnet, got %s", got.Network)
	}
}

func TestCacheReturnsStaleOnFetchFailure(t *testing.T) {
	ks := testKeystore(t)
	n := testNAL(ks)
	if err := Sign(n, ks); err != nil {
		t.Fatalf("sign: %v", err)
	}

	// Pre-populate cache.
	cache := NewCache(0) // TTL=0 so it's always "expired"
	cache.Put("testnet", n)

	// Use a server that returns 500 to trigger fetch failure quickly.
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer failSrv.Close()

	got, err := cache.FetchAndVerify(context.Background(), failSrv.URL, "testnet")
	if err != nil {
		t.Fatalf("expected stale cache, got error: %v", err)
	}
	if got.Network != "testnet" {
		t.Errorf("expected stale testnet NAL, got %s", got.Network)
	}
}

func TestCacheAreaAndNodeAllowed(t *testing.T) {
	ks := testKeystore(t)
	n := testNAL(ks)
	if err := Sign(n, ks); err != nil {
		t.Fatalf("sign: %v", err)
	}

	cache := NewCache(1 * time.Hour)
	cache.Put("testnet", n)

	// Area lookup.
	area, err := cache.Area("testnet", "test.general")
	if err != nil {
		t.Fatalf("Area: %v", err)
	}
	if area.Name != "General" {
		t.Errorf("expected General, got %s", area.Name)
	}

	_, err = cache.Area("testnet", "nonexistent")
	if err != ErrAreaNotFound {
		t.Errorf("expected ErrAreaNotFound, got %v", err)
	}

	_, err = cache.Area("unknown", "test.general")
	if err != ErrNetworkNotCached {
		t.Errorf("expected ErrNetworkNotCached, got %v", err)
	}

	// NodeAllowed via cache.
	allowed, err := cache.NodeAllowed("testnet", "test.general", "anynode")
	if err != nil {
		t.Fatalf("NodeAllowed: %v", err)
	}
	if !allowed {
		t.Error("open area should allow any node")
	}
}

func TestFetchFromServer(t *testing.T) {
	ks := testKeystore(t)
	n := testNAL(ks)
	if err := Sign(n, ks); err != nil {
		t.Fatalf("sign: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(n)
	}))
	defer srv.Close()

	got, err := Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if err := Verify(got); err != nil {
		t.Fatalf("Verify fetched NAL: %v", err)
	}

	_ = os.TempDir() // silence unused import if needed
}
