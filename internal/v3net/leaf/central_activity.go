package leaf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// V3Net Central phase 2 on a leaf: network Who's Online and Last Callers.
//
// For each kind the sysop turned on (and the hub serves), the leaf keeps
// the board's snapshot on the hub in step with the BBS: it asks its
// CentralActivitySource what may be published, signs it and PUTs it, on
// every change the BBS signals (NotifyCentralActivity) and on a heartbeat.
// The source applies the callers' consent, so a snapshot only ever holds
// callers who opted in, as they stand at the moment it is built: a caller
// who withdraws is gone from the next one, and with them every call of
// theirs the previous one carried. For a kind the sysop turned off, the
// leaf withdraws whatever it published before, once, at start.
//
// Presence is also republished whenever the event stream reconnects, since
// the hub drops a board's presence when its stream closes.

// CentralActivitySource is what the BBS tells a leaf it may publish. Both
// methods return only callers who consented and are not hidden; the leaf
// cleans, caps and signs what they return. The source is shared by every
// leaf and must be safe for concurrent use. callersDefault is the
// subscription's Callers Default switch: with it set, a caller who has not
// chosen counts as consenting.
type CentralActivitySource interface {
	// CentralPresence returns one entry per visible, consenting session on
	// the board now: Handle, a Session ID unique to the call, and the
	// caller's chosen Availability, if any.
	CentralPresence(callersDefault bool) []protocol.CentralActivityEntry
	// CentralLastCalls returns the consenting callers' calls that began
	// after since, each with Handle, At and (once it ended) Until.
	CentralLastCalls(since time.Time, callersDefault bool) []protocol.CentralActivityEntry
}

// Activity timing.
var (
	// centralPresenceHeartbeat is how often a board refreshes its presence
	// with nothing changed, and centralPresenceTTL how long each snapshot
	// is valid for: three heartbeats, inside protocol.MaxCentralPresenceTTL.
	centralPresenceHeartbeat = time.Minute
	centralPresenceTTL       = 3 * time.Minute
	// centralLastCallsHeartbeat is the longest a board goes between
	// last-callers snapshots; it publishes sooner when its oldest call is
	// about to pass the retention limit.
	centralLastCallsHeartbeat = 30 * time.Minute
	// centralLastCallsRetention is how long a call stays published: the
	// protocol's limit, less a margin so a snapshot built now is accepted.
	centralLastCallsRetention = protocol.MaxCentralLastCallRetention - time.Minute
	// centralActivityDebounce gathers the changes of a burst of logons and
	// logoffs into one publish.
	centralActivityDebounce = 2 * time.Second
	// centralActivityRetryMin and Max bound the backoff of a failed
	// activity publish. Presence goes stale in minutes, so these are
	// shorter than the listing's.
	centralActivityRetryMin = 10 * time.Second
	centralActivityRetryMax = 5 * time.Minute
	// centralActivityCacheTTL is how long a fetched feed is served from
	// memory; an activity event empties it sooner.
	centralActivityCacheTTL = 30 * time.Second
)

// maxActivityRespBytes bounds a feed reply: MaxCentralListings snapshots
// of at most about 12KB each, with room for the hub's fields.
const maxActivityRespBytes = 8 << 20

// activityKinds are the activity kinds, in the order the leaf runs them.
var activityKinds = []string{protocol.CentralKindPresence, protocol.CentralKindLastCallers}

// activityState is the leaf's bookkeeping for one kind. kick is buffered
// so a notification never blocks; centralState.mu guards the rest.
type activityState struct {
	kick chan struct{}

	// wire serializes this kind's PUTs and DELETEs, and halted, set by
	// WithdrawActivity, refuses every PUT after it: a final withdrawal
	// waits for a publish in flight and is never followed by one, so the
	// hub cannot be left holding a snapshot the board withdrew.
	wire   sync.Mutex
	halted bool // guarded by wire

	lastSeq       int64
	lastPublished time.Time
	lastCount     int
	lastError     string
	withdrawn     bool

	fetched time.Time
	stale   bool
	feed    []protocol.CentralActivityBoard
}

// ActivityStatus is what a leaf knows about one activity kind.
type ActivityStatus struct {
	// Enabled is the sysop's switch for the kind on this network, which
	// needs Central itself on; Supported whether the hub serves it.
	Enabled   bool
	Supported bool
	// CallersDefault is whether callers who have not chosen count as
	// opted in.
	CallersDefault bool
	// LastPublished is when a snapshot last reached the hub (zero if not
	// this run) and LastCount how many entries it held; Withdrawn whether
	// this run withdrew the kind; LastError the latest failure.
	LastPublished time.Time
	LastCount     int
	Withdrawn     bool
	LastError     string
}

// newActivityStates returns the per-kind state for a new leaf.
func newActivityStates() map[string]*activityState {
	m := make(map[string]*activityState, len(activityKinds))
	for _, k := range activityKinds {
		m[k] = &activityState{kick: make(chan struct{}, 1)}
	}
	return m
}

// activityEnabled reports whether the sysop turned kind on for this
// subscription. A kind needs Central participation on as well, and a
// source to say what to publish.
func (l *Leaf) activityEnabled(kind string) bool {
	if !l.cfg.CentralEnabled {
		return false
	}
	switch kind {
	case protocol.CentralKindPresence:
		return l.cfg.CentralPresence
	case protocol.CentralKindLastCallers:
		return l.cfg.CentralLastCallers
	}
	return false
}

// ActivityStatus returns what the leaf knows about kind.
func (l *Leaf) ActivityStatus(kind string) ActivityStatus {
	st := l.central.activity[kind]
	if st == nil {
		return ActivityStatus{}
	}
	supported, _ := l.HubSupports(protocol.CentralActivityFeature(kind))
	l.central.mu.Lock()
	defer l.central.mu.Unlock()
	return ActivityStatus{
		Enabled:        l.activityEnabled(kind),
		Supported:      supported,
		CallersDefault: l.cfg.CentralCallersDefault,
		LastPublished:  st.lastPublished,
		LastCount:      st.lastCount,
		Withdrawn:      st.withdrawn,
		LastError:      st.lastError,
	}
}

// NotifyCentralActivity tells the leaf the board's activity may have
// changed (a logon, a logoff, a caller's consent), so it republishes each
// kind it publishes shortly. It never blocks.
func (l *Leaf) NotifyCentralActivity() {
	for _, st := range l.central.activity {
		select {
		case st.kick <- struct{}{}:
		default:
		}
	}
}

// kickActivity schedules a republish of one kind.
func (l *Leaf) kickActivity(kind string) {
	if st := l.central.activity[kind]; st != nil {
		select {
		case st.kick <- struct{}{}:
		default:
		}
	}
}

// runCentralActivity runs every activity kind until ctx ends.
func (l *Leaf) runCentralActivity(ctx context.Context) {
	var wg sync.WaitGroup
	for _, kind := range activityKinds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.runActivityKind(ctx, kind)
		}()
	}
	wg.Wait()
}

// runActivityKind keeps one kind's snapshot in step with the BBS: once the
// hub's features are known (runCentral reads them), it does nothing on a
// hub without the feature, withdraws once if the sysop turned the kind
// off, and otherwise publishes on each notification and heartbeat.
func (l *Leaf) runActivityKind(ctx context.Context, kind string) {
	feature := protocol.CentralActivityFeature(kind)
	for {
		if _, known := l.HubSupports(feature); known {
			break
		}
		if !sleepCtx(ctx, 2*time.Second) {
			return
		}
	}
	if supported, _ := l.HubSupports(feature); !supported {
		slog.Info("leaf: hub does not serve a Central feature; it is off for this network", "network", l.cfg.Network, "feature", feature)
		return
	}
	st := l.central.activity[kind]

	if !l.activityEnabled(kind) || l.cfg.Activity == nil {
		delay := centralActivityRetryMin
		for {
			err := l.withdrawActivity(ctx, kind)
			if err == nil {
				return
			}
			l.setActivityError(kind, "withdraw: "+err.Error())
			slog.Warn("leaf: central activity withdrawal failed; retrying", "network", l.cfg.Network, "kind", kind, "error", err, "delay", delay)
			if !sleepCtx(ctx, delay) {
				return
			}
			delay = min(delay*2, centralActivityRetryMax)
		}
	}

	delay := centralActivityRetryMin
	for {
		wait, err := l.publishActivity(ctx, kind)
		if errors.Is(err, errActivityHalted) {
			return
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			l.setActivityError(kind, "publish: "+err.Error())
			slog.Warn("leaf: central activity publish failed; retrying", "network", l.cfg.Network, "kind", kind, "error", err, "delay", delay)
			wait = delay
			delay = min(delay*2, centralActivityRetryMax)
		} else {
			delay = centralActivityRetryMin
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-st.kick:
			timer.Stop()
			// Let a burst of changes land, then publish them together.
			if !sleepCtx(ctx, centralActivityDebounce) {
				return
			}
			select {
			case <-st.kick:
			default:
			}
		}
	}
}

// setActivityError records the latest failure for kind, for Status.
func (l *Leaf) setActivityError(kind, msg string) {
	l.central.mu.Lock()
	if st := l.central.activity[kind]; st != nil {
		st.lastError = msg
	}
	l.central.mu.Unlock()
}

// buildActivity returns the board's signed snapshot of kind at now, and
// how long until it should be refreshed with nothing changed.
func (l *Leaf) buildActivity(kind string, now time.Time) (*protocol.CentralActivity, time.Duration, error) {
	src := l.cfg.Activity
	if src == nil {
		return nil, 0, errors.New("leaf: no activity source")
	}
	now = now.UTC().Truncate(time.Second)
	bbs := protocol.CleanBBSName(l.cfg.BBSName)
	if bbs == "" || protocol.CleanCentralText(bbs, protocol.MaxBBSNameLen) == "" {
		bbs = l.cfg.Keystore.NodeID()
	}

	var entries []protocol.CentralActivityEntry
	expires := now.Add(centralPresenceTTL)
	refresh := centralPresenceHeartbeat
	switch kind {
	case protocol.CentralKindPresence:
		seen := make(map[string]bool)
		for _, e := range src.CentralPresence(l.cfg.CentralCallersDefault) {
			h := protocol.CleanCentralText(e.Handle, protocol.MaxCentralHandleLen)
			if h == "" || seen[e.Session] {
				continue
			}
			avail := e.Availability
			if avail != protocol.CentralAvailable && avail != protocol.CentralBusy {
				avail = ""
			}
			seen[e.Session] = true
			entries = append(entries, protocol.CentralActivityEntry{Handle: h, Session: e.Session, Availability: avail})
			if len(entries) == protocol.MaxCentralPresenceEntries {
				break
			}
		}
	case protocol.CentralKindLastCallers:
		cutoff := now.Add(-centralLastCallsRetention)
		calls := src.CentralLastCalls(cutoff, l.cfg.CentralCallersDefault)
		type call struct {
			e     protocol.CentralActivityEntry
			began time.Time
		}
		var kept []call
		for _, e := range calls {
			h := protocol.CleanCentralText(e.Handle, protocol.MaxCentralHandleLen)
			began, err := time.Parse(time.RFC3339, e.At)
			if h == "" || err != nil {
				continue
			}
			began = began.UTC().Truncate(time.Second)
			if !began.After(cutoff) || began.After(now) {
				continue
			}
			out := protocol.CentralActivityEntry{Handle: h, At: began.Format(time.RFC3339)}
			if ended, err := time.Parse(time.RFC3339, e.Until); err == nil && !ended.Before(began) {
				out.Until = ended.UTC().Truncate(time.Second).Format(time.RFC3339)
			}
			kept = append(kept, call{out, began})
		}
		// Newest first, at most the protocol's limit.
		sort.SliceStable(kept, func(i, j int) bool { return kept[i].began.After(kept[j].began) })
		if len(kept) > protocol.MaxCentralLastCallEntries {
			kept = kept[:protocol.MaxCentralLastCallEntries]
		}
		expires = now.Add(protocol.MaxCentralLastCallRetention)
		refresh = centralLastCallsHeartbeat
		for _, c := range kept {
			entries = append(entries, c.e)
		}
		if n := len(kept); n > 0 {
			// The snapshot ends when its oldest call passes the limit;
			// publish again just before, without that call.
			oldest := kept[n-1].began
			expires = oldest.Add(protocol.MaxCentralLastCallRetention)
			refresh = min(refresh, max(oldest.Add(centralLastCallsRetention).Sub(now), time.Minute))
		}
	default:
		return nil, 0, fmt.Errorf("leaf: unknown activity kind %q", kind)
	}

	l.central.mu.Lock()
	st := l.central.activity[kind]
	seq := max(time.Now().UnixNano(), st.lastSeq+1)
	st.lastSeq = seq
	l.central.mu.Unlock()

	a := &protocol.CentralActivity{
		V3NetCentral: protocol.CentralActivityVersion,
		Kind:         kind,
		Network:      l.cfg.Network,
		Seq:          seq,
		BBS:          bbs,
		Entries:      entries,
		PublishedAt:  now.Format(time.RFC3339),
		ExpiresAt:    expires.Format(time.RFC3339),
	}
	if a.Entries == nil {
		a.Entries = []protocol.CentralActivityEntry{}
	}
	if err := a.Sign(l.cfg.Keystore); err != nil {
		return nil, 0, err
	}
	if err := a.Validate(l.cfg.Network, kind); err != nil {
		return nil, 0, fmt.Errorf("leaf: activity snapshot is not valid: %w", err)
	}
	return a, refresh, nil
}

// publishActivity builds and PUTs the board's snapshot of kind, returning
// how long until the next heartbeat publish.
func (l *Leaf) publishActivity(ctx context.Context, kind string) (time.Duration, error) {
	st := l.central.activity[kind]
	st.wire.Lock()
	defer st.wire.Unlock()
	if st.halted {
		return 0, errActivityHalted
	}
	a, refresh, err := l.buildActivity(kind, time.Now())
	if err != nil {
		return 0, err
	}
	data, err := json.Marshal(a)
	if err != nil {
		return 0, fmt.Errorf("leaf: marshal activity: %w", err)
	}
	path := fmt.Sprintf("/v3net/v1/%s/central/%s", l.cfg.Network, kind)
	resp, err := l.signedDo(ctx, http.MethodPut, path, data)
	if err != nil {
		return 0, err
	}
	body, _ := readBody(resp.Body, maxRespBytes)
	_ = resp.Body.Close() // read side; already read in full
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("leaf: PUT %s returned %d: %s", path, resp.StatusCode, hubErrorText(body))
	}
	l.central.mu.Lock()
	st.lastPublished = time.Now()
	st.lastCount = len(a.Entries)
	st.lastError = ""
	st.withdrawn = false
	l.central.mu.Unlock()
	slog.Debug("leaf: central activity published", "network", l.cfg.Network, "kind", kind, "entries", len(a.Entries))
	return refresh, nil
}

// withdrawActivity DELETEs the board's snapshot of kind.
func (l *Leaf) withdrawActivity(ctx context.Context, kind string) error {
	st := l.central.activity[kind]
	st.wire.Lock()
	defer st.wire.Unlock()
	return l.withdrawActivityLocked(ctx, kind)
}

// withdrawActivityLocked is withdrawActivity with the kind's wire lock held.
func (l *Leaf) withdrawActivityLocked(ctx context.Context, kind string) error {
	path := fmt.Sprintf("/v3net/v1/%s/central/%s", l.cfg.Network, kind)
	resp, err := l.signedDo(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	body, _ := readBody(resp.Body, maxRespBytes)
	_ = resp.Body.Close() // read side; already read in full
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("leaf: DELETE %s returned %d: %s", path, resp.StatusCode, hubErrorText(body))
	}
	l.central.mu.Lock()
	st := l.central.activity[kind]
	st.withdrawn = true
	st.lastError = ""
	l.central.mu.Unlock()
	slog.Info("leaf: central activity withdrawn", "network", l.cfg.Network, "kind", kind)
	return nil
}

// WithdrawActivity withdraws the board's snapshot of kind now, for good:
// it waits for a publish in flight to finish, and this leaf publishes the
// kind no more. The service calls it before stopping a leaf that will not
// publish the kind again.
//
// The DELETE gets centralWithdrawBudget from when the wait ends, so a slow
// publish ahead of it cannot use up its time; the wait itself is bounded
// by that publish's own request timeout. ctx still cancels both.
func (l *Leaf) WithdrawActivity(ctx context.Context, kind string) error {
	st := l.central.activity[kind]
	if st == nil {
		return fmt.Errorf("leaf: unknown activity kind %q", kind)
	}
	st.wire.Lock()
	defer st.wire.Unlock()
	st.halted = true
	ctx, cancel := context.WithTimeout(ctx, centralWithdrawBudget)
	defer cancel()
	return l.withdrawActivityLocked(ctx, kind)
}

// centralWithdrawBudget is how long a final withdrawal's DELETE may take,
// counted from when it can be sent.
var centralWithdrawBudget = 10 * time.Second

// errActivityHalted is returned by publishActivity once WithdrawActivity
// has run.
var errActivityHalted = errors.New("leaf: activity withdrawn; not publishing")

// FetchCentralActivity returns the network's feed of kind: every board's
// snapshot the hub serves that validates, has not expired, and verifies
// as signed by the node it names, one per node. Presence is kept only for
// boards the hub says are connected, and calls older than the retention
// limit are dropped from a last-callers snapshot whatever the hub sent.
// It returns ErrCentralDisabled unless the sysop turned the kind on, and
// ErrCentralUnsupported when the hub does not serve it. The result is
// cached for centralActivityCacheTTL or until an activity event; force
// fetches anyway. The slice is the caller's.
func (l *Leaf) FetchCentralActivity(ctx context.Context, kind string, force bool) ([]protocol.CentralActivityBoard, error) {
	st := l.central.activity[kind]
	if st == nil || !l.activityEnabled(kind) {
		return nil, ErrCentralDisabled
	}
	if supported, _ := l.HubSupports(protocol.CentralActivityFeature(kind)); !supported {
		return nil, ErrCentralUnsupported
	}
	l.central.mu.Lock()
	if !force && !st.stale && !st.fetched.IsZero() && time.Since(st.fetched) < centralActivityCacheTTL {
		// Expiry is checked again on the way out of the cache: a snapshot
		// or call can pass its limit while cached, with no event to say so.
		out := liveActivityFeed(st.feed, kind, time.Now())
		l.central.mu.Unlock()
		return out, nil
	}
	l.central.mu.Unlock()

	path := fmt.Sprintf("/v3net/v1/%s/central/%s", l.cfg.Network, kind)
	resp, err := l.signedDo(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	body, err := readBody(resp.Body, maxActivityRespBytes)
	_ = resp.Body.Close() // read side; already read in full
	if err != nil {
		return nil, fmt.Errorf("leaf: read activity: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrCentralUnsupported
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("leaf: GET %s returned %d: %s", path, resp.StatusCode, hubErrorText(body))
	}
	var feed protocol.CentralActivityFeed
	if err := json.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("leaf: decode activity: %w", err)
	}
	boards := verifyActivityFeed(&feed, l.cfg.Network, kind, time.Now())

	l.central.mu.Lock()
	st.feed = boards
	st.fetched = time.Now()
	st.stale = false
	l.central.mu.Unlock()
	return cloneActivityFeed(boards), nil
}

// verifyActivityFeed keeps the feed's boards whose snapshot is for network
// and kind, validates, has not expired at now and verifies as signed by
// the key its node ID derives, at most protocol.MaxCentralListings and one
// per node. Presence from a board the hub does not report connected is
// dropped, and so is a call older than the retention limit.
func verifyActivityFeed(feed *protocol.CentralActivityFeed, network, kind string, now time.Time) []protocol.CentralActivityBoard {
	var out []protocol.CentralActivityBoard
	seen := make(map[string]bool)
	for i := range feed.Boards {
		if len(out) >= protocol.MaxCentralListings {
			break
		}
		b := feed.Boards[i]
		a := &b.Activity
		var why string
		switch {
		case a.Validate(network, kind) != nil:
			why = "invalid"
		case a.Expired(now):
			why = "expired"
		case a.Verify() != nil:
			why = "signature failed"
		case seen[a.NodeID]:
			why = "duplicate node"
		case kind == protocol.CentralKindPresence && !b.Online:
			why = "board offline"
		}
		if why != "" {
			if why != "board offline" {
				slog.Warn("leaf: dropped central activity", "network", network, "kind", kind, "node", a.NodeID, "reason", why)
			}
			continue
		}
		seen[a.NodeID] = true
		if kind == protocol.CentralKindLastCallers {
			a.Entries = slices.DeleteFunc(a.Entries, func(e protocol.CentralActivityEntry) bool {
				return now.Sub(e.Began()) > protocol.MaxCentralLastCallRetention
			})
		}
		out = append(out, b)
	}
	return out
}

// liveActivityFeed returns a copy of feed without the snapshots that have
// expired at now and, for last callers, the calls past the retention limit.
func liveActivityFeed(feed []protocol.CentralActivityBoard, kind string, now time.Time) []protocol.CentralActivityBoard {
	out := cloneActivityFeed(feed)
	out = slices.DeleteFunc(out, func(b protocol.CentralActivityBoard) bool { return b.Activity.Expired(now) })
	if kind == protocol.CentralKindLastCallers {
		for i := range out {
			out[i].Activity.Entries = slices.DeleteFunc(out[i].Activity.Entries, func(e protocol.CentralActivityEntry) bool {
				return now.Sub(e.Began()) > protocol.MaxCentralLastCallRetention
			})
		}
	}
	return out
}

// cloneActivityFeed copies a feed deeply enough for the caller to change
// its entries without touching the cache.
func cloneActivityFeed(in []protocol.CentralActivityBoard) []protocol.CentralActivityBoard {
	out := make([]protocol.CentralActivityBoard, len(in))
	for i, b := range in {
		out[i] = b
		out[i].Activity.Entries = slices.Clone(b.Activity.Entries)
	}
	return out
}
