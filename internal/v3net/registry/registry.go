// Package registry fetches and caches the central V3Net network registry.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// DefaultURL is the canonical registry location.
const DefaultURL = "https://raw.githubusercontent.com/ViSiON-3/v3net-registry/main/registry.json"

const cacheTTL = 1 * time.Hour

// httpTimeout is the maximum time allowed for a registry fetch.
const httpTimeout = 30 * time.Second

type cacheEntry struct {
	networks  []protocol.RegistryEntry
	fetchedAt time.Time
}

var (
	cacheMu sync.RWMutex
	cache   = make(map[string]*cacheEntry) // keyed by URL
)

// Fetch retrieves the registry from the given URL. Results are cached in memory
// per URL for 1 hour. If the fetch fails but cached data exists for that URL,
// the cached data is returned instead of an error.
func Fetch(ctx context.Context, url string) ([]protocol.RegistryEntry, error) {
	cacheMu.RLock()
	if entry := cache[url]; entry != nil && time.Since(entry.fetchedAt) < cacheTTL {
		networks := entry.networks
		cacheMu.RUnlock()
		return networks, nil
	}
	cacheMu.RUnlock()

	networks, err := fetchRemote(ctx, url)
	if err != nil {
		slog.Warn("registry: fetch failed, using cache if available", "error", err)
		cacheMu.RLock()
		defer cacheMu.RUnlock()
		if entry := cache[url]; entry != nil {
			return entry.networks, nil
		}
		return nil, fmt.Errorf("registry: fetch failed with no cache: %w", err)
	}

	cacheMu.Lock()
	cache[url] = &cacheEntry{networks: networks, fetchedAt: time.Now()}
	cacheMu.Unlock()

	return networks, nil
}

var registryClient = &http.Client{Timeout: httpTimeout}

func fetchRemote(ctx context.Context, url string) ([]protocol.RegistryEntry, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := registryClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned %d", url, resp.StatusCode)
	}

	var reg protocol.Registry
	if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil {
		return nil, fmt.Errorf("decode registry: %w", err)
	}

	return reg.Networks, nil
}

// ClearCache resets the in-memory cache (used in tests).
func ClearCache() {
	cacheMu.Lock()
	cache = make(map[string]*cacheEntry)
	cacheMu.Unlock()
}
