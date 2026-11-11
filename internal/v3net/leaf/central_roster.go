package leaf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// The Central board roster on a leaf (see protocol.CentralRoster).
//
// The leaf takes a roster only when it is for this network, valid, and
// signed by the coordinator key this subscription pins, which is the key
// the network's NAL is verified against, not the hub's. It keeps the newest
// it has taken, in memory and in Config.RosterPath, so a restart, a hub
// that stops serving it, or a hub that serves an older one changes nothing.
// A roster from a new coordinator, after a handover moved the pin, replaces
// the old coordinator's whatever their seqs.
//
// With a roster, the board's own Central screens and alerts show only
// boards on it: a directory listing, an activity snapshot or an alert from
// any other board is dropped, matched on node ID and full public key. This
// board itself always passes. Without one, nothing is dropped, as before.

// rosterState is the leaf's roster: the newest it has taken, and what it
// knows of the last fetch.
type rosterState struct {
	mu        sync.Mutex
	roster    *protocol.CentralRoster
	lastFetch time.Time
	lastErr   string
	// fetching is set while a background fetch runs, so a burst of
	// events makes one fetch; again is set when another request came
	// meanwhile, so the fetch runs once more when it ends and an update
	// published during it is not missed.
	fetching, again bool
	// raw is the held roster as served, and unsaved says it has not yet
	// reached RosterPath, so the next sight of it writes it again.
	raw     []byte
	unsaved bool
	// pinRefused says the last roster was refused for its coordinator
	// key, so a NAL refresh that moves the pin fetches again.
	pinRefused bool
}

// RosterStatus is what a leaf knows about its network's roster.
type RosterStatus struct {
	// Supported is whether the hub serves rosters; Held whether the leaf
	// has one (and so filters by it), with its Seq and member count.
	Supported bool
	Held      bool
	Seq       int64
	Members   int
	// LastFetch is when a fetch last reached the hub, LastError the
	// latest failure or refusal.
	LastFetch time.Time
	LastError string
}

// RosterStatus returns the leaf's roster state.
func (l *Leaf) RosterStatus() RosterStatus {
	st := RosterStatus{}
	st.Supported, _ = l.HubSupports(protocol.FeatureCentralRoster)
	l.rosterState.mu.Lock()
	defer l.rosterState.mu.Unlock()
	if r := l.rosterState.roster; r != nil {
		st.Held, st.Seq, st.Members = true, r.Seq, len(r.Members)
	}
	st.LastFetch, st.LastError = l.rosterState.lastFetch, l.rosterState.lastErr
	return st
}

// Roster returns a copy of the roster the leaf holds, or nil.
func (l *Leaf) Roster() *protocol.CentralRoster {
	l.rosterState.mu.Lock()
	defer l.rosterState.mu.Unlock()
	if l.rosterState.roster == nil {
		return nil
	}
	r := *l.rosterState.roster
	r.Members = slices.Clone(r.Members)
	return &r
}

// rosterAdmits reports whether a Central object from nodeID, signed with
// pubKeyB64, may be shown: there is no roster, it is this board's, or the
// roster names that board with that key.
func (l *Leaf) rosterAdmits(nodeID, pubKeyB64 string) bool {
	if nodeID == l.cfg.Keystore.NodeID() {
		return true
	}
	l.rosterState.mu.Lock()
	defer l.rosterState.mu.Unlock()
	return l.rosterState.roster == nil || l.rosterState.roster.Admits(nodeID, pubKeyB64)
}

// loadRoster reads the roster kept at Config.RosterPath, if there is one
// for this network. One that does not verify is ignored and logged.
func (l *Leaf) loadRoster() {
	if l.cfg.RosterPath == "" {
		return
	}
	data, err := os.ReadFile(l.cfg.RosterPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("leaf: read kept central roster", "network", l.cfg.Network, "path", l.cfg.RosterPath, "error", err)
		}
		return
	}
	var r protocol.CentralRoster
	if err := json.Unmarshal(data, &r); err != nil || r.Validate(l.cfg.Network) != nil || r.Verify() != nil {
		slog.Warn("leaf: kept central roster does not verify; ignoring it", "network", l.cfg.Network, "path", l.cfg.RosterPath)
		return
	}
	l.rosterState.roster = &r
	l.rosterState.raw = data
}

// pinnedCoordKey returns the coordinator key this subscription pins, or ""
// before one is pinned.
func (l *Leaf) pinnedCoordKey() string {
	l.pinMu.Lock()
	defer l.pinMu.Unlock()
	return l.pins.coord
}

// errNoRoster is returned by fetchRoster when the hub has none.
var errNoRoster = errors.New("leaf: the network has no roster")

// fetchRoster asks the hub for the network's roster and takes it if it
// passes (see the notes above). It reports why one was not taken.
func (l *Leaf) fetchRoster(ctx context.Context) error {
	if ok, _ := l.HubSupports(protocol.FeatureCentralRoster); !ok {
		return nil
	}
	path := fmt.Sprintf("/v3net/v1/%s/central/roster", l.cfg.Network)
	resp, err := l.signedDo(ctx, http.MethodGet, path, nil)
	if err != nil {
		return l.noteRosterErr(err)
	}
	body, err := readBody(resp.Body, maxRespBytes)
	_ = resp.Body.Close() // read side; already read in full
	if err != nil {
		return l.noteRosterErr(fmt.Errorf("leaf: read roster: %w", err))
	}
	l.rosterState.mu.Lock()
	l.rosterState.lastFetch = time.Now()
	l.rosterState.mu.Unlock()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		// Kept rosters stay in force: a hub that stops serving one does
		// not turn the filter off, and the sysop should hear of it.
		l.rosterState.mu.Lock()
		held := l.rosterState.roster != nil
		l.rosterState.mu.Unlock()
		if held {
			l.noteRosterErr(errors.New("the hub serves no roster; keeping the one this board holds"))
		} else {
			l.noteRosterErr(nil)
		}
		return errNoRoster
	default:
		return l.noteRosterErr(fmt.Errorf("leaf: GET %s returned %d: %s", path, resp.StatusCode, hubErrorText(body)))
	}
	var r protocol.CentralRoster
	if err := json.Unmarshal(body, &r); err != nil {
		return l.noteRosterErr(fmt.Errorf("leaf: decode roster: %w", err))
	}
	return l.noteRosterErr(l.takeRoster(&r, body))
}

// takeRoster takes r, raw as served, when it is valid, signed by the pinned
// coordinator key, and newer than the one held (or from a new coordinator).
func (l *Leaf) takeRoster(r *protocol.CentralRoster, raw []byte) error {
	if err := r.Validate(l.cfg.Network); err != nil {
		return fmt.Errorf("leaf: roster refused: %w", err)
	}
	if err := r.Verify(); err != nil {
		return fmt.Errorf("leaf: roster refused: %w", err)
	}
	coord := l.pinnedCoordKey()
	if coord == "" || r.CoordPubKeyB64 != coord {
		// Perhaps the pin has yet to follow a handover: the next NAL
		// refresh fetches again.
		l.rosterState.mu.Lock()
		l.rosterState.pinRefused = true
		l.rosterState.mu.Unlock()
		if coord == "" {
			return errors.New("leaf: roster refused: no coordinator key pinned yet")
		}
		return errors.New("leaf: roster refused: not signed by the pinned coordinator key")
	}
	l.rosterState.mu.Lock()
	l.rosterState.pinRefused = false
	held := l.rosterState.roster
	if held != nil && held.CoordPubKeyB64 == r.CoordPubKeyB64 && r.Seq <= held.Seq {
		// The one held, or an older one: nothing to take, but the one
		// held may still be waiting to be kept.
		retry := l.rosterState.unsaved
		l.rosterState.mu.Unlock()
		if retry {
			return l.keepRoster()
		}
		return nil
	}
	l.rosterState.roster = r
	l.rosterState.raw = slices.Clone(raw)
	l.rosterState.unsaved = l.cfg.RosterPath != ""
	l.rosterState.mu.Unlock()

	slog.Info("leaf: central roster taken", "network", l.cfg.Network, "seq", r.Seq, "members", len(r.Members))
	return l.keepRoster()
}

// keepRoster writes the held roster to RosterPath if it has not reached it
// yet. A failure leaves it marked unsaved, so the next fetch tries again.
func (l *Leaf) keepRoster() error {
	l.rosterState.mu.Lock()
	raw, unsaved := l.rosterState.raw, l.rosterState.unsaved
	l.rosterState.mu.Unlock()
	if !unsaved || l.cfg.RosterPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(l.cfg.RosterPath), 0o755); err != nil {
		return fmt.Errorf("leaf: keep roster: %w", err)
	}
	if err := atomicfile.WriteFile(l.cfg.RosterPath, raw, 0o644); err != nil {
		return fmt.Errorf("leaf: keep roster: %w", err)
	}
	l.rosterState.mu.Lock()
	if string(l.rosterState.raw) == string(raw) {
		l.rosterState.unsaved = false
	}
	l.rosterState.mu.Unlock()
	return nil
}

// rosterAfterNAL fetches the roster again once a NAL refresh may have
// moved the coordinator pin: when the last roster was refused for its key,
// or the one held is not the pinned coordinator's.
func (l *Leaf) rosterAfterNAL(ctx context.Context) {
	coord := l.pinnedCoordKey()
	l.rosterState.mu.Lock()
	need := l.rosterState.pinRefused ||
		(l.rosterState.roster != nil && coord != "" && l.rosterState.roster.CoordPubKeyB64 != coord)
	l.rosterState.mu.Unlock()
	if need {
		l.refreshRosterSoon(ctx)
	}
}

// noteRosterErr records err as the last roster failure (nil clears it)
// and returns it.
func (l *Leaf) noteRosterErr(err error) error {
	l.rosterState.mu.Lock()
	if err == nil {
		l.rosterState.lastErr = ""
	} else {
		l.rosterState.lastErr = err.Error()
	}
	l.rosterState.mu.Unlock()
	if err != nil {
		slog.Warn("leaf: central roster", "network", l.cfg.Network, "error", err)
	}
	return err
}

// refreshRosterSoon fetches the roster in the background, once for a
// burst of calls, for an event or a reconnect.
func (l *Leaf) refreshRosterSoon(ctx context.Context) {
	l.rosterState.mu.Lock()
	if l.rosterState.fetching {
		// The fetch in flight may have read the hub before this update:
		// run once more after it.
		l.rosterState.again = true
		l.rosterState.mu.Unlock()
		return
	}
	l.rosterState.fetching = true
	l.rosterState.mu.Unlock()
	go func() {
		for {
			fctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			_ = l.fetchRoster(fctx) // logged
			cancel()
			l.rosterState.mu.Lock()
			if !l.rosterState.again || ctx.Err() != nil {
				l.rosterState.fetching, l.rosterState.again = false, false
				l.rosterState.mu.Unlock()
				return
			}
			l.rosterState.again = false
			l.rosterState.mu.Unlock()
		}
	}()
}

// dispatchRosterEvent refetches the roster when the hub says the
// coordinator published a new one.
func (l *Leaf) dispatchRosterEvent(ctx context.Context, ev protocol.Event) {
	if ev.Type != protocol.EventCentralRosterUpdated {
		return
	}
	var p protocol.CentralRosterEventPayload
	if err := json.Unmarshal(ev.Data, &p); err != nil || (p.Network != "" && p.Network != l.cfg.Network) || p.Expired(time.Now()) {
		return
	}
	l.refreshRosterSoon(ctx)
}

// rosterFilterDirectory drops directory entries the roster does not admit.
func (l *Leaf) rosterFilterDirectory(entries []protocol.CentralDirectoryEntry) []protocol.CentralDirectoryEntry {
	return slices.DeleteFunc(entries, func(e protocol.CentralDirectoryEntry) bool {
		return !l.rosterAdmits(e.Listing.NodeID, e.Listing.OriginPubKeyB64)
	})
}

// rosterFilterActivity drops activity snapshots the roster does not admit.
func (l *Leaf) rosterFilterActivity(boards []protocol.CentralActivityBoard) []protocol.CentralActivityBoard {
	return slices.DeleteFunc(boards, func(b protocol.CentralActivityBoard) bool {
		return !l.rosterAdmits(b.Activity.NodeID, b.Activity.OriginPubKeyB64)
	})
}

// PublishRoster signs members as the network's roster, with this board as
// its coordinator, and posts it to the hub, which takes it only from the
// coordinator in force. Its seq is above any roster the leaf holds. On
// success the leaf holds it too.
func (l *Leaf) PublishRoster(ctx context.Context, members []protocol.CentralRosterMember) (*protocol.CentralRoster, error) {
	seq := time.Now().UnixNano()
	l.rosterState.mu.Lock()
	if held := l.rosterState.roster; held != nil && held.Seq >= seq {
		seq = held.Seq + 1
	}
	l.rosterState.mu.Unlock()
	r := &protocol.CentralRoster{
		V3NetCentral: protocol.CentralRosterVersion,
		Network:      l.cfg.Network,
		Seq:          seq,
		Members:      slices.Clone(members),
		PublishedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	if err := r.Sign(l.cfg.Keystore); err != nil {
		return nil, err
	}
	if err := r.Validate(l.cfg.Network); err != nil {
		return nil, fmt.Errorf("leaf: roster: %w", err)
	}
	data, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("leaf: marshal roster: %w", err)
	}
	path := fmt.Sprintf("/v3net/v1/%s/central/roster", l.cfg.Network)
	resp, err := l.signedDo(ctx, http.MethodPost, path, data)
	if err != nil {
		return nil, err
	}
	body, _ := readBody(resp.Body, maxRespBytes)
	_ = resp.Body.Close() // read side; already read in full
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("leaf: POST %s returned %d: %s", path, resp.StatusCode, hubErrorText(body))
	}
	if err := l.takeRoster(r, data); err != nil {
		slog.Warn("leaf: published roster not held here", "network", l.cfg.Network, "error", err)
	}
	return r, nil
}
