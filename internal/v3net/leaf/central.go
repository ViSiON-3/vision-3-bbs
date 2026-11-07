package leaf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// V3Net Central on a leaf. The leaf learns from the hub's /info which
// Central features the hub serves; a hub from before Central serves none,
// and the leaf then neither publishes nor offers the directory, while
// message networking carries on. With the directory supported, the leaf
// publishes this board's listing if the sysop turned publication on, and
// otherwise withdraws any listing it published before, so turning
// publication off is honoured at the next start. Fetching the directory
// is separate: it needs the sysop's participation switch and nothing about
// this board or its callers goes to the hub for it.

// Central timing.
var (
	// centralRepublishInterval is how often a publishing leaf refreshes its
	// listing, moving its expiry on. It is well inside
	// protocol.MaxCentralListingTTL, so a board that is up never expires.
	centralRepublishInterval = 6 * time.Hour
	// centralListingTTL is how long each published listing declares
	// itself valid for.
	centralListingTTL = protocol.MaxCentralListingTTL
	// centralDirectoryCacheTTL is how long a fetched directory is served
	// from memory before a caller's next look fetches it again. A listing
	// event empties the cache sooner.
	centralDirectoryCacheTTL = time.Minute
	// centralRetryMin and centralRetryMax bound the backoff of a failed
	// publish, withdrawal or feature check.
	centralRetryMin = time.Minute
	centralRetryMax = 30 * time.Minute
)

// centralSeenEvents is how many Central event IDs the leaf remembers to
// drop a replayed event.
const centralSeenEvents = 256

// maxDirectoryRespBytes bounds a directory reply. The largest listing
// within protocol's limits is under 3KB, so a full directory of
// protocol.MaxCentralListings is under 2MB; the cap leaves room for the
// hub's own fields around each one.
const maxDirectoryRespBytes = 4 << 20

// ErrCentralUnsupported is returned by FetchCentralDirectory when the hub
// does not serve the directory, or has not said yet whether it does.
var ErrCentralUnsupported = errors.New("leaf: hub does not serve the Central directory")

// ErrCentralDisabled is returned by FetchCentralDirectory when Central is
// not enabled for this subscription.
var ErrCentralDisabled = errors.New("leaf: Central is not enabled for this network")

// CentralListingSpec is what a leaf publishes about its board. Name is
// required; the rest may be empty. Capabilities is filled in by the leaf
// from what it supports, plus any given here.
type CentralListingSpec struct {
	Name         string
	Description  string
	Addresses    []string
	Capabilities []string
	Software     string
}

// CentralStatus is what a leaf knows about Central on its network.
type CentralStatus struct {
	// Enabled is the sysop's participation switch for this network.
	Enabled bool
	// Publishing is whether the sysop turned listing publication on.
	Publishing bool
	// SupportKnown is whether the hub's feature list has been read, and
	// Supported whether it names the directory.
	SupportKnown bool
	Supported    bool
	// Connected is whether the event stream is open now.
	Connected bool
	// LastPublished is when the listing last reached the hub (zero if
	// never this run), and LastError the most recent publish or withdrawal
	// failure, cleared by a success.
	LastPublished time.Time
	LastError     string
	// Withdrawn is whether this run withdrew the listing (publication off).
	Withdrawn bool
	// DirectoryFetched is when the directory was last fetched and verified
	// (zero if never), and DirectoryCount how many listings it held.
	DirectoryFetched time.Time
	DirectoryCount   int
}

// centralState is the leaf's Central bookkeeping. mu guards every field
// but listingWire and listingHalted.
type centralState struct {
	mu sync.Mutex

	// listingWire serializes the listing's PUTs and DELETEs, and
	// listingHalted, set by WithdrawCentralListing, refuses every PUT after
	// it, so a final withdrawal is never followed by a publish.
	listingWire   sync.Mutex
	listingHalted bool // guarded by listingWire

	featuresKnown bool
	features      []string

	lastPublished time.Time
	lastError     string
	withdrawn     bool

	dirFetched time.Time
	dirStale   bool
	dir        []protocol.CentralDirectoryEntry

	seen     []string // ring of event IDs acted on
	seenNext int

	// activity is the per-kind state of Who's Online and Last Callers
	// (central_activity.go). The map is fixed at New; mu guards the
	// states' fields.
	activity map[string]*activityState
}

// centralEnabled reports whether Central participation is on for this
// subscription.
func (l *Leaf) centralEnabled() bool { return l.cfg.CentralEnabled }

// centralPublishing reports whether the sysop turned publication on.
func (l *Leaf) centralPublishing() bool {
	return l.cfg.CentralListing != nil && l.cfg.CentralListing.Name != ""
}

// noteHubFeatures records the feature list the hub's /info served.
func (l *Leaf) noteHubFeatures(features []string) {
	l.central.mu.Lock()
	l.central.featuresKnown = true
	l.central.features = slices.Clone(features)
	l.central.mu.Unlock()
}

// HubSupports reports whether the hub serves feature, and whether the hub's
// feature list has been read at all. Before it has, supported is false.
func (l *Leaf) HubSupports(feature string) (supported, known bool) {
	l.central.mu.Lock()
	defer l.central.mu.Unlock()
	return slices.Contains(l.central.features, feature), l.central.featuresKnown
}

// SSEConnected reports whether the event stream is open now.
func (l *Leaf) SSEConnected() bool { return l.sseConnected.Load() }

// CentralStatus returns what the leaf knows about Central on its network.
func (l *Leaf) CentralStatus() CentralStatus {
	l.central.mu.Lock()
	defer l.central.mu.Unlock()
	return CentralStatus{
		Enabled:          l.centralEnabled(),
		Publishing:       l.centralPublishing(),
		SupportKnown:     l.central.featuresKnown,
		Supported:        slices.Contains(l.central.features, protocol.FeatureCentralDirectory),
		Connected:        l.sseConnected.Load(),
		LastPublished:    l.central.lastPublished,
		LastError:        l.central.lastError,
		Withdrawn:        l.central.withdrawn,
		DirectoryFetched: l.central.dirFetched,
		DirectoryCount:   len(l.central.dir),
	}
}

// fetchHubFeatures reads /info for the hub's feature list and records it.
func (l *Leaf) fetchHubFeatures(ctx context.Context) error {
	info, err := l.fetchHubInfo(ctx)
	if err != nil {
		return err
	}
	l.noteHubFeatures(info.Features)
	return nil
}

// runCentral keeps the board's listing in step with the sysop's choice
// until ctx ends: published and refreshed while publication is on,
// withdrawn once at start while it is off. It first makes sure the hub's
// feature list is known, and does nothing on a hub that does not serve the
// directory. Failures are retried with backoff and shown in CentralStatus.
func (l *Leaf) runCentral(ctx context.Context) {
	delay := centralRetryMin
	for {
		if _, known := l.HubSupports(protocol.FeatureCentralDirectory); known {
			break
		}
		if err := l.fetchHubFeatures(ctx); err == nil {
			break
		} else {
			l.setCentralError("feature check: " + err.Error())
			slog.Debug("leaf: central feature check failed; retrying", "network", l.cfg.Network, "error", err, "delay", delay)
		}
		if !sleepCtx(ctx, delay) {
			return
		}
		delay = min(delay*2, centralRetryMax)
	}
	if supported, _ := l.HubSupports(protocol.FeatureCentralDirectory); !supported {
		slog.Info("leaf: hub does not serve the Central directory; Central is off for this network", "network", l.cfg.Network, "hub", l.cfg.HubURL)
		return
	}

	if !l.centralPublishing() {
		// Publication is off: make sure nothing of ours is listed. One
		// idempotent request, retried until the hub answers.
		delay = centralRetryMin
		for {
			err := l.withdrawCentralListing(ctx)
			if err == nil {
				return
			}
			l.setCentralError("withdraw: " + err.Error())
			slog.Warn("leaf: central listing withdrawal failed; retrying", "network", l.cfg.Network, "error", err, "delay", delay)
			if !sleepCtx(ctx, delay) {
				return
			}
			delay = min(delay*2, centralRetryMax)
		}
	}

	delay = centralRetryMin
	for {
		wait := centralRepublishInterval
		err := l.publishCentralListing(ctx)
		if errors.Is(err, errListingHalted) {
			return
		}
		if err != nil {
			l.setCentralError("publish: " + err.Error())
			slog.Warn("leaf: central listing publish failed; retrying", "network", l.cfg.Network, "error", err, "delay", delay)
			wait = delay
			delay = min(delay*2, centralRetryMax)
		} else {
			delay = centralRetryMin
		}
		if !sleepCtx(ctx, wait) {
			return
		}
	}
}

// sleepCtx waits for d or until ctx ends, reporting whether the wait ran
// its course.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func (l *Leaf) setCentralError(msg string) {
	l.central.mu.Lock()
	l.central.lastError = msg
	l.central.mu.Unlock()
}

// buildCentralListing returns this board's listing, signed, for now.
func (l *Leaf) buildCentralListing(now time.Time) (*protocol.CentralListing, error) {
	spec := l.cfg.CentralListing
	if spec == nil || spec.Name == "" {
		return nil, errors.New("leaf: no listing configured")
	}
	caps := []string{protocol.FeatureCentralDirectory}
	for _, c := range spec.Capabilities {
		if !slices.Contains(caps, c) {
			caps = append(caps, c)
		}
	}
	now = now.UTC()
	listing := &protocol.CentralListing{
		V3NetCentral: protocol.CentralListingVersion,
		Network:      l.cfg.Network,
		Seq:          now.UnixNano(),
		Name:         protocol.CleanBBSName(spec.Name),
		Description:  protocol.TrimField(spec.Description, protocol.MaxCentralDescriptionLen),
		Addresses:    slices.Clone(spec.Addresses),
		Capabilities: caps,
		Software:     protocol.TrimField(spec.Software, protocol.MaxCentralSoftwareLen),
		PublishedAt:  now.Format(time.RFC3339),
		ExpiresAt:    now.Add(centralListingTTL).Format(time.RFC3339),
	}
	if err := listing.Sign(l.cfg.Keystore); err != nil {
		return nil, err
	}
	// Refused here, with the sysop's own text in the error, rather than
	// by the hub with a bare status.
	if err := listing.Validate(l.cfg.Network); err != nil {
		return nil, fmt.Errorf("leaf: listing is not valid: %w", err)
	}
	return listing, nil
}

// publishCentralListing signs and PUTs this board's listing.
func (l *Leaf) publishCentralListing(ctx context.Context) error {
	l.central.listingWire.Lock()
	defer l.central.listingWire.Unlock()
	if l.central.listingHalted {
		return errListingHalted
	}
	listing, err := l.buildCentralListing(time.Now())
	if err != nil {
		return err
	}
	data, err := json.Marshal(listing)
	if err != nil {
		return fmt.Errorf("leaf: marshal listing: %w", err)
	}
	path := fmt.Sprintf("/v3net/v1/%s/central/listing", l.cfg.Network)
	resp, err := l.signedDo(ctx, http.MethodPut, path, data)
	if err != nil {
		return err
	}
	body, _ := readBody(resp.Body, maxRespBytes)
	_ = resp.Body.Close() // read side; already read in full
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("leaf: PUT %s returned %d: %s", path, resp.StatusCode, hubErrorText(body))
	}
	l.central.mu.Lock()
	l.central.lastPublished = time.Now()
	l.central.lastError = ""
	l.central.withdrawn = false
	l.central.mu.Unlock()
	slog.Info("leaf: central listing published", "network", l.cfg.Network, "name", listing.Name)
	return nil
}

// withdrawCentralListing DELETEs this board's listing. The hub answers the
// same whether or not it held one.
func (l *Leaf) withdrawCentralListing(ctx context.Context) error {
	l.central.listingWire.Lock()
	defer l.central.listingWire.Unlock()
	return l.withdrawCentralListingLocked(ctx)
}

// withdrawCentralListingLocked is withdrawCentralListing with listingWire
// held.
func (l *Leaf) withdrawCentralListingLocked(ctx context.Context) error {
	path := fmt.Sprintf("/v3net/v1/%s/central/listing", l.cfg.Network)
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
	l.central.withdrawn = true
	l.central.lastError = ""
	l.central.mu.Unlock()
	slog.Info("leaf: central listing withdrawn", "network", l.cfg.Network)
	return nil
}

// WithdrawCentralListing withdraws this board's listing from the hub now,
// whatever the configuration says. The service calls it before stopping a
// leaf whose subscription is being removed, moved to another hub or having
// publication turned off, so the listing goes at once and from the hub
// that holds it, rather than waiting for a replacement leaf that may never
// contact that hub.
func (l *Leaf) WithdrawCentralListing(ctx context.Context) error {
	// For good: wait for a publish in flight, and publish no more, so a
	// refresh or retry racing the stop cannot list the board again on a
	// hub no replacement leaf will withdraw from.
	// The DELETE's time starts once the wait ends, as for WithdrawActivity.
	l.central.listingWire.Lock()
	defer l.central.listingWire.Unlock()
	l.central.listingHalted = true
	ctx, cancel := context.WithTimeout(ctx, centralWithdrawBudget)
	defer cancel()
	return l.withdrawCentralListingLocked(ctx)
}

// errListingHalted is returned by publishCentralListing once
// WithdrawCentralListing has run.
var errListingHalted = errors.New("leaf: listing withdrawn; not publishing")

// hubErrorText pulls the error field out of a hub's JSON error body, or
// takes the body as text, and returns it fit for a caller's screen: pipes
// and control characters become spaces, since the text ends up in status
// lines every caller can see and a hub is not trusted with their
// terminals, and it is cut to 200 characters on a rune boundary.
func hubErrorText(body []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	s := string(body)
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		s = e.Error
	}
	s = strings.Map(func(r rune) rune {
		if r == '|' || r < 0x20 || (r >= 0x7F && r <= 0x9F) || r == utf8.RuneError {
			return ' '
		}
		return r
	}, s)
	return protocol.TrimField(strings.TrimSpace(s), 200)
}

// FetchCentralDirectory returns the network's board directory: every
// listing the hub serves that verifies as signed by the node it is listed
// under and has not expired. A listing that fails any check is dropped and
// logged, never shown. The result is served from memory for
// centralDirectoryCacheTTL after a fetch, or until a listing event; pass
// force to fetch anyway. It returns ErrCentralDisabled when participation
// is off for this subscription and ErrCentralUnsupported when the hub does
// not serve the directory. The slice is the caller's to keep.
func (l *Leaf) FetchCentralDirectory(ctx context.Context, force bool) ([]protocol.CentralDirectoryEntry, error) {
	if !l.centralEnabled() {
		return nil, ErrCentralDisabled
	}
	if supported, _ := l.HubSupports(protocol.FeatureCentralDirectory); !supported {
		return nil, ErrCentralUnsupported
	}

	l.central.mu.Lock()
	if !force && !l.central.dirStale && !l.central.dirFetched.IsZero() && time.Since(l.central.dirFetched) < centralDirectoryCacheTTL {
		out := slices.Clone(l.central.dir)
		l.central.mu.Unlock()
		return out, nil
	}
	l.central.mu.Unlock()

	path := fmt.Sprintf("/v3net/v1/%s/central/listings", l.cfg.Network)
	resp, err := l.signedDo(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	body, err := readBody(resp.Body, maxDirectoryRespBytes)
	_ = resp.Body.Close() // read side; already read in full
	if err != nil {
		return nil, fmt.Errorf("leaf: read directory: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		// A hub whose /info named the feature but that has since been
		// rolled back. Stop offering it.
		l.noteHubFeatures(nil)
		return nil, ErrCentralUnsupported
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("leaf: GET %s returned %d: %s", path, resp.StatusCode, hubErrorText(body))
	}
	var dir protocol.CentralDirectory
	if err := json.Unmarshal(body, &dir); err != nil {
		return nil, fmt.Errorf("leaf: decode directory: %w", err)
	}
	entries := verifyCentralDirectory(&dir, l.cfg.Network, time.Now())

	l.central.mu.Lock()
	l.central.dir = entries
	l.central.dirFetched = time.Now()
	l.central.dirStale = false
	l.central.mu.Unlock()
	return slices.Clone(entries), nil
}

// verifyCentralDirectory keeps the directory's entries whose listing is for
// network, validates, has not expired at now, and verifies as signed by the
// key its node ID derives. Entries are capped at protocol.MaxCentralListings
// and one per node, in the order the hub served them.
func verifyCentralDirectory(dir *protocol.CentralDirectory, network string, now time.Time) []protocol.CentralDirectoryEntry {
	var out []protocol.CentralDirectoryEntry
	seen := make(map[string]bool)
	for i := range dir.Listings {
		if len(out) >= protocol.MaxCentralListings {
			break
		}
		e := dir.Listings[i]
		lst := &e.Listing
		var why string
		switch {
		case lst.Validate(network) != nil:
			why = "invalid"
		case lst.Expired(now):
			why = "expired"
		case lst.Verify() != nil:
			why = "signature failed"
		case seen[lst.NodeID]:
			why = "duplicate node"
		}
		if why != "" {
			slog.Warn("leaf: dropped central listing", "network", network, "node", lst.NodeID, "reason", why)
			continue
		}
		seen[lst.NodeID] = true
		out = append(out, e)
	}
	return out
}

// dispatchCentralEvent acts on a Central event: a fresh listing event
// marks the cached directory stale, and a fresh activity event the cached
// feed of its kind, so the next look fetches again. One past its expiry,
// or whose ID was acted on before, is dropped: a replay would only cost a
// fetch, but the rule is the same for every Central event.
func (l *Leaf) dispatchCentralEvent(ev protocol.Event) {
	var meta protocol.EventMeta
	var network, kind string
	switch ev.Type {
	case protocol.EventCentralListingUpdated, protocol.EventCentralListingWithdrawn:
		var p protocol.CentralListingEventPayload
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			return
		}
		meta, network = p.EventMeta, p.Network
	case protocol.EventCentralActivityUpdated:
		var p protocol.CentralActivityEventPayload
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			return
		}
		meta, network, kind = p.EventMeta, p.Network, p.Kind
		if l.central.activity[kind] == nil {
			return
		}
	default:
		return
	}
	if network != "" && network != l.cfg.Network {
		return
	}
	if meta.Expired(time.Now()) {
		slog.Debug("leaf: dropped expired central event", "network", l.cfg.Network, "type", ev.Type, "id", meta.EventID)
		return
	}
	l.central.mu.Lock()
	defer l.central.mu.Unlock()
	if meta.EventID != "" {
		if slices.Contains(l.central.seen, meta.EventID) {
			slog.Debug("leaf: dropped replayed central event", "network", l.cfg.Network, "type", ev.Type, "id", meta.EventID)
			return
		}
		if len(l.central.seen) < centralSeenEvents {
			l.central.seen = append(l.central.seen, meta.EventID)
		} else {
			l.central.seen[l.central.seenNext] = meta.EventID
			l.central.seenNext = (l.central.seenNext + 1) % centralSeenEvents
		}
	}
	if kind != "" {
		l.central.activity[kind].stale = true
		return
	}
	l.central.dirStale = true
}
