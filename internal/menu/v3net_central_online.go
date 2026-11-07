package menu

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// The @UN@ AT-code is how many callers are online on other V3Net boards:
// those who turned on network Who's Online, counted once per caller per
// board, across every network this board shows Who's Online for. This
// board's own callers are left out; @U@ counts them.
//
// A menu is drawn often and must never wait on a hub, so the count is
// kept for netOnlineTTL and refreshed in the background once it is older.
// Only the first draw after start-up waits, and for netOnlineFirstWait at
// most; a hub that cannot be reached leaves the last count, or 0.

// netOnlineTTL is how long a count is shown before it is refreshed. It
// matches the leaf's own activity cache, so a refresh is usually served
// from that cache, or brought forward by its change notices.
const netOnlineTTL = 30 * time.Second

// netOnlineFirstWait bounds the one fetch a menu draw waits for.
const netOnlineFirstWait = 1500 * time.Millisecond

// netOnlineRefreshTimeout bounds a background refresh.
const netOnlineRefreshTimeout = 10 * time.Second

// netOnlineCache is the last @UN@ count and when it was taken.
type netOnlineCache struct {
	mu         sync.Mutex
	count      int
	at         time.Time
	refreshing bool
}

// netOnlineCounts maps each executor to its count. A side table keeps the
// executor's own fields as they are, so its tests' struct literals need no
// cache.
var netOnlineCounts sync.Map // *MenuExecutor -> *netOnlineCache

// hasNetOnlineCode reports whether content uses @UN@ in any form, so a
// screen without it costs nothing.
func hasNetOnlineCode(content []byte) bool {
	return strings.Contains(string(content), "@UN")
}

// v3netOnlineCount returns the @UN@ count: from the cache while it is
// fresh, otherwise the last count while a refresh runs behind it. With
// V3Net off, or Who's Online on no network, it is 0.
func (e *MenuExecutor) v3netOnlineCount() int {
	p := e.V3NetCentral
	if p == nil || len(centralActivityNetworks(p, protocol.CentralKindPresence)) == 0 {
		return 0
	}
	v, _ := netOnlineCounts.LoadOrStore(e, &netOnlineCache{})
	c := v.(*netOnlineCache)
	c.mu.Lock()
	if !c.at.IsZero() && time.Since(c.at) < netOnlineTTL {
		n := c.count
		c.mu.Unlock()
		return n
	}
	first := c.at.IsZero()
	if c.refreshing {
		n := c.count
		c.mu.Unlock()
		return n
	}
	c.refreshing = true
	c.mu.Unlock()

	if first {
		// Nothing to show yet: wait a moment for the first count.
		ctx, cancel := context.WithTimeout(context.Background(), netOnlineFirstWait)
		defer cancel()
		return c.refresh(ctx, p)
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), netOnlineRefreshTimeout)
		defer cancel()
		c.refresh(ctx, p)
	}()
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}

// refresh counts the callers online on other boards and stores the count.
// A network that cannot be fetched adds nothing; when none can, the last
// count is kept until the next refresh.
func (c *netOnlineCache) refresh(ctx context.Context, p V3NetCentralProvider) int {
	self := p.NodeID()
	seen := make(map[string]struct{})
	fetched := false
	for _, n := range centralActivityNetworks(p, protocol.CentralKindPresence) {
		boards, err := p.CentralActivity(ctx, n.Network, protocol.CentralKindPresence, false)
		if err != nil {
			continue
		}
		fetched = true
		for _, b := range boards {
			if b.Activity.NodeID == self {
				continue
			}
			for _, en := range b.Activity.Entries {
				// One caller on one board counts once, whichever networks
				// and however many nodes they are on.
				seen[b.Activity.NodeID+"\x00"+strings.ToLower(en.Handle)] = struct{}{}
			}
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refreshing = false
	if fetched {
		c.count = len(seen)
	}
	// Taken either way: a hub that is down is tried again behind a later
	// draw, not waited on by every one.
	c.at = time.Now()
	return c.count
}
