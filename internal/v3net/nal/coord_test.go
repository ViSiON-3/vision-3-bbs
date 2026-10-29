package nal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// A NAL can carry a valid signature over a coordinator node ID that does not
// belong to its key; Verify accepts it, VerifyCoordNodeID does not.
func TestVerifyCoordNodeID(t *testing.T) {
	ks := testKeystore(t)
	n := testNAL(ks)
	if err := Sign(n, ks); err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := VerifyCoordNodeID(n); err != nil {
		t.Fatalf("VerifyCoordNodeID on a correctly signed NAL: %v", err)
	}

	// Re-sign with a node ID that is not derived from the key.
	n.CoordNodeID = "0123456789abcdef"
	n.SignatureB64 = ""
	payload, err := canonicalBytes(n)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := ks.SignRaw(payload)
	if err != nil {
		t.Fatal(err)
	}
	n.SignatureB64 = base64.StdEncoding.EncodeToString(sig)
	if err := Verify(n); err != nil {
		t.Fatalf("test setup: signature should verify: %v", err)
	}
	if err := VerifyCoordNodeID(n); !errors.Is(err, ErrCoordNodeIDMismatch) {
		t.Errorf("VerifyCoordNodeID = %v, want ErrCoordNodeIDMismatch", err)
	}

	n.CoordPubKeyB64 = "not-a-key"
	if err := VerifyCoordNodeID(n); !errors.Is(err, ErrInvalidPubKey) {
		t.Errorf("VerifyCoordNodeID with a bad key = %v, want ErrInvalidPubKey", err)
	}
}

// A cache check that fails rejects the fetched NAL and keeps the cached one.
func TestCacheCheckRejectsAndKeepsPrevious(t *testing.T) {
	ks := testKeystore(t)
	good := testNAL(ks)
	if err := Sign(good, ks); err != nil {
		t.Fatalf("sign: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(good)
	}))
	defer srv.Close()

	cache := NewCache(time.Hour, nil)
	ctx := context.Background()
	if _, err := cache.Refresh(ctx, srv.URL, "testnet"); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	sentinel := errors.New("rejected by check")
	cache.SetCheck(func(*protocol.NAL) error { return sentinel })
	if _, err := cache.Refresh(ctx, srv.URL, "testnet"); !errors.Is(err, sentinel) {
		t.Errorf("Refresh = %v, want the check's error", err)
	}
	if cache.Get("testnet") == nil {
		t.Error("the cached NAL was dropped by a rejected fetch")
	}
}
