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

type cacheEntry struct {
	networks  []protocol.RegistryEntry
	fetchedAt time.Time
}

var (
	cacheMu sync.RWMutex
	cache   *cacheEntry
)

// Fetch retrieves the registry from the given URL. Results are cached in memory
// for 1 hour. If the fetch fails but cached data exists, the cached data is
// returned instead of an error.
func Fetch(ctx context.Context, url string) ([]protocol.RegistryEntry, error) {
	cacheMu.RLock()
	if cache != nil && time.Since(cache.fetchedAt) < cacheTTL {
		networks := cache.networks
		cacheMu.RUnlock()
		return networks, nil
	}
	cacheMu.RUnlock()

	networks, err := fetchRemote(ctx, url)
	if err != nil {
		slog.Warn("registry: fetch failed, using cache if available", "error", err)
		cacheMu.RLock()
		defer cacheMu.RUnlock()
		if cache != nil {
			return cache.networks, nil
		}
		return nil, fmt.Errorf("registry: fetch failed with no cache: %w", err)
	}

	cacheMu.Lock()
	cache = &cacheEntry{networks: networks, fetchedAt: time.Now()}
	cacheMu.Unlock()

	return networks, nil
}

func fetchRemote(ctx context.Context, url string) ([]protocol.RegistryEntry, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
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
	cache = nil
	cacheMu.Unlock()
}
