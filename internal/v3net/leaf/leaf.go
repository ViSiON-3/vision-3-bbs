package leaf

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// Leaf is a V3Net leaf client that polls a hub for messages and maintains
// an SSE connection for real-time events.
type Leaf struct {
	cfg          Config
	client       *http.Client // short-timeout client for polling/subscribe
	sseClient    *http.Client // no timeout — SSE streams are long-lived; context handles cancellation
	eventCb      atomic.Value // stores func(protocol.Event)
	nalCache     *nal.Cache
	chatSessions *chatSessionRegistry

	// nalMu runs NAL refreshes one at a time, so a slow fetch of an older
	// NAL cannot land after a newer one and replace it.
	nalMu sync.Mutex
	// refetchPending is set while an nal_updated re-fetch is waiting to run;
	// further events are folded into it.
	refetchPending atomic.Bool
	// reconnectFetching is set while the post-reconnect fetch is retrying.
	reconnectFetching atomic.Bool

	// hubMu guards hub, which pauses syncing while the hub's key or
	// signatures do not match the pinned hub key (see hubsig.go).
	hubMu sync.Mutex
	hub   hubGate
	// unsignedRefusedLogged is set once a refused unsigned reply is logged,
	// and cleared when a hub signature next verifies.
	unsignedRefusedLogged atomic.Bool

	// pinMu guards pins, the pinned and pending coordinator and hub keys.
	pinMu sync.Mutex
	pins  keyPins
	// coordSaveMu and hubSaveMu serialize savePin for each key kind.
	coordSaveMu, hubSaveMu sync.Mutex
	// pinReadHook, if set (tests only), runs between applyFilePin's read of
	// the store and its update of the pin.
	pinReadHook func(KeyKind)

	// refused remembers which refused messages were already logged at warn.
	refused refusedLog

	// central is the leaf's V3Net Central state (central.go).
	central centralState
	// alerts is the leaf's activity alert state (central_alert.go).
	alerts alertState
	// rosterState is the network's board roster (central_roster.go).
	rosterState rosterState
	// sseConnected is set while the event stream is open.
	sseConnected atomic.Bool
	// pollNow wakes the poll loop for a poll ahead of its ticker, when the
	// hub reports a new message (see dispatchMessageEvent). It holds one
	// token, so a burst of events while a poll runs adds one poll after it.
	pollNow chan struct{}
}

// maxHubRedirects is how many redirects a hub request follows, as Go's own
// default.
const maxHubRedirects = 10

// sameHostRedirect is the CheckRedirect of the leaf's HTTP clients: a
// redirect is followed only to the scheme, host and port the request was
// first sent to (sameDialTarget). Go's default follows a redirect anywhere,
// so a hub, or anyone between it and the leaf on plain HTTP, could send the
// leaf's requests, with their bodies on a 307 or 308, to any address,
// including ones on the leaf's own network. A refused redirect fails the
// request.
func sameHostRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxHubRedirects {
		return fmt.Errorf("leaf: stopped after %d redirects", maxHubRedirects)
	}
	first := via[0].URL
	if !sameDialTarget(first, req.URL) {
		return fmt.Errorf("leaf: refusing redirect from %s://%s to %s://%s", first.Scheme, first.Host, req.URL.Scheme, req.URL.Host)
	}
	return nil
}

// sameDialTarget reports whether a and b have the same scheme, host and
// port, the port being the scheme's default when the URL gives none. An
// ASCII hostname is compared ignoring case, as DNS does. A hostname with
// anything else in it must match byte for byte: Go's transport dials its
// IDNA ASCII form, and Unicode case folding (strings.EqualFold) can call two
// hostnames equal that IDNA turns into different names. Identical strings
// always dial the same name.
func sameDialTarget(a, b *url.URL) bool {
	// A scheme url.Parse accepts is ASCII, so folding it is plain ASCII case.
	if !strings.EqualFold(a.Scheme, b.Scheme) {
		return false
	}
	if dialPort(a) != dialPort(b) {
		return false
	}
	ha, hb := a.Hostname(), b.Hostname()
	if isASCII(ha) && isASCII(hb) {
		return strings.EqualFold(ha, hb)
	}
	return ha == hb
}

// dialPort is u's port, or its scheme's default when it gives none.
func dialPort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	}
	return ""
}

// isASCII reports whether s is all ASCII.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// New creates a new Leaf with the given configuration.
func New(cfg Config) *Leaf {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = DefaultPollInterval
	}

	l := &Leaf{
		cfg:       cfg,
		client:    &http.Client{Timeout: 10 * time.Second, CheckRedirect: sameHostRedirect},
		sseClient: &http.Client{CheckRedirect: sameHostRedirect},
		nalCache:  nal.NewCache(1*time.Hour, &http.Client{Timeout: 30 * time.Second, CheckRedirect: sameHostRedirect}),
		pins: keyPins{
			coord:        normalizeKey(cfg.CoordPubKeyB64),
			hub:          normalizeKey(cfg.HubPubKeyB64),
			pendingCoord: normalizeKey(cfg.PendingCoordPubKeyB64),
			pendingHub:   normalizeKey(cfg.PendingHubPubKeyB64),
		},
		hub:     newHubGate(),
		pollNow: make(chan struct{}, 1),
	}
	l.nalCache.SetCheck(l.checkNAL)
	if cfg.OnEvent != nil {
		l.eventCb.Store(cfg.OnEvent)
	}
	l.chatSessions = newChatSessionRegistry(cfg.Keystore.NodeID())
	l.chatSessions.blocked = chatBlockSet(cfg.ChatBlocked)
	l.central.activity = newActivityStates()
	l.loadRoster()
	return l
}

// HubURL returns the hub URL this leaf connects to.
func (l *Leaf) HubURL() string {
	return l.cfg.HubURL
}

// Network returns the V3Net network name this leaf is subscribed to.
func (l *Leaf) Network() string { return l.cfg.Network }

// Poll runs a single poll cycle. Exported for integration testing.
func (l *Leaf) Poll(ctx context.Context) (int, error) {
	return l.poll(ctx)
}

// RunSSE runs the SSE connection loop. Exported for integration testing.
func (l *Leaf) RunSSE(ctx context.Context) {
	l.runSSE(ctx)
}

// SetOnEvent sets the event callback. Safe for concurrent use.
func (l *Leaf) SetOnEvent(fn func(protocol.Event)) {
	l.eventCb.Store(fn)
}

// onEvent loads and invokes the event callback if set.
func (l *Leaf) onEvent(ev protocol.Event) {
	if v := l.eventCb.Load(); v != nil {
		v.(func(protocol.Event))(ev)
	}
}

// Close releases resources held by the leaf (idle HTTP connections, etc.).
func (l *Leaf) Close() {
	l.client.CloseIdleConnections()
	l.sseClient.CloseIdleConnections()
}

// NewChatSession creates a ChatSession for handle and registers it on this leaf.
func (l *Leaf) NewChatSession(handle string) *ChatSession {
	s := &ChatSession{
		leaf:   l,
		handle: handle,
		origin: rand.Text(),
		events: make(chan chat.ChatEvent, 64),
	}
	l.chatSessions.register(s)
	return s
}

// Start begins the polling and SSE goroutines. Blocks until ctx is cancelled.
func (l *Leaf) Start(ctx context.Context) {
	slog.Info("leaf: starting", "network", l.cfg.Network, "hub", l.cfg.HubURL)

	// Pin the hub's key from /info, then subscribe (bootstrap, signed with
	// the existing node key): every reply after that is checked against the
	// pin. Retry with exponential backoff if the hub is temporarily
	// unreachable.
	subscribeBackoff := 5 * time.Second
	for {
		err := l.checkHubKey(ctx)
		if err == nil {
			err = l.subscribe(ctx)
		}
		if err != nil {
			// A changed key, or a bad signature on the reply, pauses
			// syncing; wait for the sysop rather than backing off.
			if l.HubPaused() {
				if l.waitHubTrusted(ctx) != nil {
					return
				}
				continue
			}
			slog.Warn("leaf: subscribe failed, retrying", "network", l.cfg.Network, "error", err, "retry_in", subscribeBackoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(subscribeBackoff):
			}
			subscribeBackoff *= 2
			if subscribeBackoff > 5*time.Minute {
				subscribeBackoff = 5 * time.Minute
			}
			continue
		}
		break
	}
	slog.Info("leaf: subscribed to hub", "network", l.cfg.Network, "hub", l.cfg.HubURL)

	// Fetch the NAL once up front, so areas the hub added while this node
	// was offline reach OnNAL without waiting for the next change event.
	if err := l.refreshNAL(ctx); err != nil {
		slog.Warn("leaf: initial NAL fetch failed", "network", l.cfg.Network, "error", err)
	}
	// The roster needs the coordinator pin the NAL fetch settles, and the
	// hub's feature list; startRoster retries until the hub has answered.
	wgRoster := make(chan struct{})
	go func() {
		defer close(wgRoster)
		l.startRoster(ctx)
	}()
	defer func() { <-wgRoster }()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		l.runPoller(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		l.runSSE(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		l.runCentral(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		l.runCentralActivity(ctx)
	}()

	wg.Wait()
	slog.Info("leaf: stopped", "network", l.cfg.Network)
}
