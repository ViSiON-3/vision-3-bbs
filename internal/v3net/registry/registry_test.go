package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testRegistryJSON = `{
	"v3net_registry": "1.0",
	"updated": "2026-03-16",
	"networks": [
		{
			"name": "felonynet",
			"description": "General discussion.",
			"hub_url": "https://bbs.felonynet.org",
			"hub_node_id": "a3f9e1b2c4d5e6f7",
			"tags": ["general", "tech"]
		}
	]
}`

func TestFetch_Success(t *testing.T) {
	ClearCache()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(testRegistryJSON))
	}))
	defer ts.Close()

	networks, err := Fetch(context.Background(), ts.URL)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(networks) != 1 {
		t.Fatalf("expected 1 network, got %d", len(networks))
	}
	if networks[0].Name != "felonynet" {
		t.Errorf("expected felonynet, got %q", networks[0].Name)
	}
	if networks[0].HubURL != "https://bbs.felonynet.org" {
		t.Errorf("unexpected hub URL: %q", networks[0].HubURL)
	}
}

func TestFetch_CachesResult(t *testing.T) {
	ClearCache()

	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(testRegistryJSON))
	}))
	defer ts.Close()

	ctx := context.Background()

	// First fetch.
	_, err := Fetch(ctx, ts.URL)
	if err != nil {
		t.Fatalf("first Fetch: %v", err)
	}

	// Second fetch should use cache.
	_, err = Fetch(ctx, ts.URL)
	if err != nil {
		t.Fatalf("second Fetch: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 HTTP call (cached), got %d", callCount)
	}
}

func TestFetch_FallsBackToCache(t *testing.T) {
	ClearCache()

	// Populate cache with a working server.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(testRegistryJSON))
	}))

	_, err := Fetch(context.Background(), ts.URL)
	if err != nil {
		t.Fatalf("initial Fetch: %v", err)
	}
	ts.Close()

	// Expire the cache by resetting fetchedAt.
	cacheMu.Lock()
	if entry := cache[ts.URL]; entry != nil {
		entry.fetchedAt = entry.fetchedAt.Add(-2 * cacheTTL)
	}
	cacheMu.Unlock()

	// Fetch from a server that returns an error — should return cached data
	// from the original URL.
	deadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusInternalServerError)
	}))
	defer deadServer.Close()

	// The dead server's URL is different, so we need to fetch from the original (now expired) URL
	// to test cache fallback. Point a new dead handler at the original URL's cache.
	// Instead, re-fetch from the original ts.URL (now closed) to trigger fallback.
	networks, err := Fetch(context.Background(), ts.URL)
	if err != nil {
		t.Fatalf("expected fallback to cache, got error: %v", err)
	}
	if len(networks) != 1 {
		t.Errorf("expected 1 cached network, got %d", len(networks))
	}
}

func TestFetch_NoCache_ReturnsError(t *testing.T) {
	ClearCache()

	// Use a server that returns an error.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusInternalServerError)
	}))
	defer ts.Close()

	_, err := Fetch(context.Background(), ts.URL)
	if err == nil {
		t.Error("expected error with no cache and failing server")
	}
}
