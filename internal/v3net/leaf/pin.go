package leaf

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// A leaf pins two keys per network: the coordinator key that signs the NAL,
// and the hub key /info serves. A NAL carries its signer's key, so verifying
// it against that key alone proves nothing about who signed it; comparing
// the key with a pinned one does. Keys come from the sysop's config (pasted
// in, or copied from the registry when the network was joined) or, failing
// that, are pinned on first use. A key that later differs from the pin is
// rejected and reported, never adopted: the sysop accepts it by replacing or
// blanking the pin in v3net.json. A changed hub key also pauses syncing with
// the hub until then (see hubsig.go).
//
// The one exception is a signed coordinator handover (see
// protocol.CoordHandover). The hub serves the handovers it holds with the
// NAL; when they lead, each signed by the key before, from the pinned key to
// the key now offered, the leaf moves the pin itself, logs the move and
// hands the new pin to OnKeyPinned with the key it replaced. The hub key
// follows the same chain when the hub signs the NAL with its own key, which
// is how every hub runs today. A stolen old key can hand the network to
// someone else this way, but it could already sign NALs.

// KeyKind names which of a network's keys a KeyEvent is about.
type KeyKind string

// The keys a leaf pins.
const (
	// KeyCoordinator is the network coordinator's key, which signs the NAL.
	KeyCoordinator KeyKind = "coordinator"
	// KeyHub is the hub's own node key, served by /info.
	KeyHub KeyKind = "hub"
)

// ErrHubKeyMismatch means the hub's /info served a key other than the one
// pinned for it. Check for it with errors.Is.
var ErrHubKeyMismatch = errors.New("leaf: hub key does not match the pinned key")

// ErrHubNodeIDMismatch means /info's hub_node_id is not the node ID derived
// from its hub_pubkey_b64.
var ErrHubNodeIDMismatch = errors.New("leaf: hub node ID does not match the hub public key")

// ErrNoStoredEntry is what Config.PinStored and Config.HubSignedStored return
// when the store has no entry for the subscription. A reload's copy held one, so
// the entry was removed since the copy was read, and the reload that follows
// from that removal retires the leaf; until then the leaf stays as it is.
var ErrNoStoredEntry = errors.New("leaf: store has no entry for this subscription")

// KeyEvent describes a key the leaf pinned on first use, or a key the hub
// offered that differs from the pin. It is passed to Config.OnKeyPinned and
// Config.OnKeyChanged.
type KeyEvent struct {
	Network string
	HubURL  string
	Kind    KeyKind
	// Pinned is the key in force: for OnKeyPinned the key just pinned, for
	// OnKeyChanged the key the leaf keeps trusting.
	Pinned string
	// Offered is the key the hub served. For OnKeyPinned it equals Pinned.
	Offered string
	// Previous is set for OnKeyPinned when a signed coordinator handover
	// moved the pin: it is the pin the move replaced, which the caller may
	// overwrite with Pinned. Empty otherwise.
	Previous string
}

// StoredPin is what the caller's store holds for one kind of key. Empty strings
// mean "none".
type StoredPin struct {
	Pin, Pending string
}

// StoredHubSigned is what the caller's store holds about whether the hub signs.
type StoredHubSigned struct {
	// Signed is the record itself (see Config.HubSigned).
	Signed bool
	// Clears is the entry's count of the sysop's clears of it
	// (config.V3NetLeafConfig.HubSignedClears).
	Clears int
}

// Pins is the set of keys a leaf holds for its network, as kept in its
// v3net.json entry. Empty strings mean "none".
type Pins struct {
	Coord, Hub               string
	PendingCoord, PendingHub string
	// HubSigned is the entry's record that the hub signs (see
	// Config.HubSigned). HubSignedClears is the highest count of clears the
	// caller has seen, in this copy or an earlier one; a store holding more
	// holds a clear the caller has yet to see. UpdatePins and Retire return the
	// store's count, once read, which is what the next reload is told against.
	HubSigned       bool
	HubSignedClears int
	// CoordEdited and HubEdited say the file's pin changed since the copy
	// the caller last handed over. A pin not yet stored is replaced only
	// then, and not when the change is the leaf's own earlier store (see
	// unsavedFollowsStore): otherwise the file simply does not hold it yet.
	CoordEdited, HubEdited bool
	// HubSignedEdited says the sysop wrote hubSigned: its value changed, or
	// the entry's count of clears rose (config.V3NetLeafConfig.HubSignedClears),
	// which is how a sysop clears a record the file never got. A record not
	// yet stored is dropped only then. A lower count is an older copy of the
	// file, or the sysop lowering it by hand, and neither is a clear.
	HubSignedEdited bool
	// CoordUnread, HubUnread and HubSignedUnread are set on what UpdatePins and
	// Retire return when the store could not be read, or held no entry, for
	// the check that field needed. Such a field stays as the leaf has it, so
	// the copy's was not acted on, and the next reload's edits to it are told
	// against whatever the caller told this one's against. A field that was
	// checked has been acted on, and is told against what is returned.
	CoordUnread, HubUnread, HubSignedUnread bool
}

// UnsavedState is what a leaf holds that its caller's store does not yet:
// pins OnKeyPinned has not stored, with the pin a handover moved each from,
// and a hubSigned record OnHubSigned has not stored. Empty strings and false
// mean none.
type UnsavedState struct {
	Coord, CoordMovedFrom string
	Hub, HubMovedFrom     string
	HubSigned             bool
}

// keyPins is the leaf's pinned and pending keys, guarded by Leaf.pinMu.
type keyPins struct {
	coord, hub               string
	pendingCoord, pendingHub string
	// coordUnsaved and hubUnsaved mark an in-memory pin, or the clearing of
	// an accepted pending key, that OnKeyPinned has not yet stored. It is
	// offered to OnKeyPinned again on every matching fetch until it is.
	coordUnsaved, hubUnsaved bool
	// coordMovedFrom and hubMovedFrom are the pin a handover replaced,
	// until OnKeyPinned stores the move. Several moves before a store keep
	// the first pin, the one the caller still holds.
	coordMovedFrom, hubMovedFrom string

	// hubSigned is whether the hub has been seen signing, so unsigned
	// replies are refused (see hubsig.go). hubSignedUnsaved marks it set
	// but not yet stored by OnHubSigned. hubSignedCfg is the value the
	// caller's config holds, as far as the leaf knows: UpdatePins acts on
	// a change from it, which is the sysop clearing or setting it.
	hubSigned, hubSignedUnsaved, hubSignedCfg bool
	// hubSignedCleared is set when the sysop's clear reached this leaf. A
	// reply a predecessor verified is older than that, so it is not recorded
	// here or handed on (see HandLateRepliesTo).
	hubSignedCleared bool
	// hubSignedLate marks a record first set after the leaf retired, and
	// hubSignedNext is the replacement to hand one to.
	hubSignedLate bool
	hubSignedNext *Leaf

	// retired is set by Retire and EndStores: a replacement leaf has taken
	// over this subscription's stores, or the subscription is gone, so the
	// leaf stores nothing more.
	retired bool
}

// slots returns pointers to the pin, the pending key and the unsaved flag
// for kind.
func (p *keyPins) slots(kind KeyKind) (pin, pending *string, unsaved *bool) {
	if kind == KeyHub {
		return &p.hub, &p.pendingHub, &p.hubUnsaved
	}
	return &p.coord, &p.pendingCoord, &p.coordUnsaved
}

// movedFrom returns a pointer to the pin a handover replaced for kind.
func (p *keyPins) movedFrom(kind KeyKind) *string {
	if kind == KeyHub {
		return &p.hubMovedFrom
	}
	return &p.coordMovedFrom
}

// pinState is what a reload's verdict on a pin depends on.
type pinState struct {
	pin, movedFrom string
	unsaved        bool
}

// state returns the pinState for kind.
func (p *keyPins) state(kind KeyKind) pinState {
	pin, _, unsaved := p.slots(kind)
	return pinState{pin: *pin, movedFrom: *p.movedFrom(kind), unsaved: *unsaved}
}

// UpdatePins installs pins edited in v3net.json on a running leaf, without
// rebuilding it, so its last accepted NAL stays in effect. A pin whose value
// changed replaces the leaf's pin and its pending key (the sysop acted on
// it); an unchanged pin keeps the leaf's own pending key, which may be newer
// than the file. A pin not yet stored is kept unless the file's pin was
// edited (Pins.CoordEdited, Pins.HubEdited), since a file that lacks it says
// nothing about it, so a reload of an unrelated change leaves what the leaf
// trusts alone; so is one that follows, by a handover, from the pin the
// leaf's own store left in the file (see unsavedFollowsStore).
//
// p is a copy of the file read earlier, so a pin the leaf has stored since
// would move back if it were believed. Each pin is therefore checked against
// the store as it is now, and the store wins (see currentFilePin). A sysop's
// edit is in the store too, even one back to an earlier key. The pins returned
// are the ones checked, which is what the next reload's edits are told against.
// A store that cannot be read, or holds no entry, shows the copy may be stale
// and has nothing better, so the leaf stays as it is (see Pins.CoordUnread).
//
// When the coordinator pin changed and ctx is non-nil, the NAL is re-fetched in the background so an accepted key takes effect at
// once; likewise /info is re-checked when the hub pin changed. Pass a nil
// ctx for a leaf that is not running. Safe for concurrent use.
//
// HubSigned is applied by applyHubSigned.
func (l *Leaf) UpdatePins(ctx context.Context, p Pins) Pins {
	l.applyHubSigned(&p)

	var coordChanged, hubChanged, coordRead, hubRead bool
	p.Coord, p.PendingCoord, coordChanged, coordRead = l.applyFilePin(KeyCoordinator, p.Coord, p.PendingCoord, p.CoordEdited)
	p.Hub, p.PendingHub, hubChanged, hubRead = l.applyFilePin(KeyHub, p.Hub, p.PendingHub, p.HubEdited)
	p.CoordUnread, p.HubUnread = !coordRead, !hubRead

	if ctx == nil || (!coordChanged && !hubChanged) {
		return p
	}
	slog.Info("leaf: pinned keys changed in config",
		"network", l.cfg.Network, "hub", l.cfg.HubURL, "coordinator", coordChanged, "hub_key", hubChanged)
	go func() {
		if hubChanged {
			if err := l.checkHubKey(ctx); err != nil {
				slog.Warn("leaf: hub key check failed", "network", l.cfg.Network, "hub", l.cfg.HubURL, "error", err)
			}
		}
		if coordChanged {
			if err := l.refreshNAL(ctx); err != nil {
				slog.Warn("leaf: NAL re-fetch after pin change failed", "network", l.cfg.Network, "error", err)
			}
		}
	}()
	return p
}

// applyFilePin installs the file's pin for kind and its pending key, as
// UpdatePins describes, and returns the file's pin and pending key as checked,
// whether the leaf's pin changed, and whether the store could be read; if it
// could not, the pin and pending key returned are the leaf's. It holds the save
// lock for kind, so no store lands between the read of the store and acting on
// it. A handover or first-use pin takes no save lock until it is stored, so if
// the leaf's pin moved meanwhile the verdict is reached again.
func (l *Leaf) applyFilePin(kind KeyKind, filePin, filePending string, edited bool) (pinOut, pendingOut string, changed, read bool) {
	mu := l.saveMu(kind)
	mu.Lock()
	defer mu.Unlock()

	for {
		l.pinMu.Lock()
		before := l.pins.state(kind)
		l.pinMu.Unlock()
		pinNow, pendingNow, editedNow, read := l.currentFilePin(kind, before, filePin, filePending, edited)
		if l.pinReadHook != nil {
			l.pinReadHook(kind)
		}
		if !read {
			return l.PinnedKey(kind), l.PendingKey(kind), false, false
		}
		if changed, settled := l.installFilePin(kind, before, pinNow, pendingNow, editedNow); settled {
			return pinNow, pendingNow, changed, true
		}
	}
}

// installFilePin acts on the verdict applyFilePin reached for the leaf's pin in
// state before. settled is false, and nothing is done, if that is no longer the
// leaf's state.
func (l *Leaf) installFilePin(kind KeyKind, before pinState, filePin, filePending string, edited bool) (changed, settled bool) {
	l.pinMu.Lock()
	defer l.pinMu.Unlock()
	if l.pins.state(kind) != before {
		return false, false
	}
	pin, pending, unsaved := l.pins.slots(kind)
	if normalizeKey(filePin) == *pin {
		// The file holds this pin, so it no longer moves from the one before,
		// and a sysop's edit back to that one is an edit. The pin stays
		// unsaved if it was: a pending key may still need clearing.
		*l.pins.movedFrom(kind) = ""
		return false, true
	}
	if !edited && *unsaved {
		return false, true
	}
	*pin = normalizeKey(filePin)
	*pending = normalizeKey(filePending)
	*unsaved = false
	*l.pins.movedFrom(kind) = ""
	return true, true
}

// currentFilePin brings a reload's copy of the file's pin and pending key for
// kind up to date with the store (Config.PinStored), and says whether the pin
// counts as edited for a leaf whose pin is in state st. The caller holds the
// save lock for kind, so the read sees every store the leaf has finished and
// none starts until the caller is done.
//
// A store that differs from the copy was written after the copy was read. If
// the leaf wrote it the pin already matches, or waits to be stored over it; if
// anyone else did it is an edit. read is false if the store cannot be read or
// holds no entry for this subscription, and the caller keeps what the leaf has:
// the copy may be the stale one, and the file will reload once it is fixed. With
// no store at all (Config.PinStored nil) the copy is all there is, and stands.
func (l *Leaf) currentFilePin(kind KeyKind, st pinState, filePin, filePending string, edited bool) (pin, pending string, editedOut, read bool) {
	if l.cfg.PinStored != nil {
		stored, err := l.cfg.PinStored(kind)
		if err != nil {
			l.noteStoreUnread(string(kind)+" key", err)
			return "", "", false, false
		}
		edited = edited || normalizeKey(stored.Pin) != normalizeKey(filePin)
		filePin, filePending = stored.Pin, stored.Pending
	}
	return filePin, filePending, edited && !unsavedFollowsStore(st, filePin), true
}

// noteStoreUnread logs that a reload could not check what, so the leaf keeps
// what it has. An entry removed since the reload's read is no fault: the reload
// that follows retires the leaf.
func (l *Leaf) noteStoreUnread(what string, err error) {
	if errors.Is(err, ErrNoStoredEntry) {
		slog.Info("leaf: subscription no longer in the config; keeping what the leaf holds",
			"network", l.cfg.Network, "hub", l.cfg.HubURL, "checking", what)
		return
	}
	slog.Warn("leaf: could not read the config; keeping what the leaf holds",
		"network", l.cfg.Network, "hub", l.cfg.HubURL, "checking", what, "error", err)
}

// unsavedFollowsStore reports whether a reload whose store holds storedPin
// leaves a pin in state st standing: the pin is waiting to be stored over the
// pin a handover moved it from, and the store holds that pin now.
//
// The leaf stores each pin over the one before, so while no one else writes
// the entry, the store holds exactly the pin the unsaved one moved from.
// Applying it would move the pin back along the chain. A sysop's edit leaves
// something else in the store, including a key the leaf passed through
// earlier, so it still wins. Retyping the pin the store already holds changes
// nothing in the file and is no edit.
func unsavedFollowsStore(st pinState, storedPin string) bool {
	return st.unsaved && st.movedFrom != "" && normalizeKey(storedPin) == st.movedFrom
}

// lockStores takes every save lock, so it returns once work the leaf started
// has finished storing, and no store starts until the returned func is called.
func (l *Leaf) lockStores() (unlock func()) {
	l.coordSaveMu.Lock()
	l.hubSaveMu.Lock()
	l.hubSignedSaveMu.Lock()
	return func() {
		l.hubSignedSaveMu.Unlock()
		l.hubSaveMu.Unlock()
		l.coordSaveMu.Unlock()
	}
}

// EndStores ends this leaf's stores, for a leaf whose subscription was removed.
// Work it started can still be finishing, and were the subscription re-added
// that work would store into the new entry, so it takes the save locks and no
// callback that writes to the store runs after it returns. What the leaf held
// goes with it: a re-added subscription starts from its entry. Call it once the
// leaf has stopped.
func (l *Leaf) EndStores() {
	defer l.lockStores()()
	l.pinMu.Lock()
	defer l.pinMu.Unlock()
	l.pins.retired = true
}

// Retire ends this leaf's stores and returns what a replacement for the same
// subscription should start from: p, a reload's copy of the file, brought up to
// date with the store, and what this leaf holds that the store does not, less
// anything the reload replaces. The pins are checked as UpdatePins checks them.
//
// So is the hubSigned record, except that here the store's true needs no clear
// to be checked against: a record stored after the copy was read reaches the
// replacement, which would otherwise take unsigned replies until the next
// reload. A record not yet stored goes with it unless the sysop wrote the field,
// in the copy or in the store since (see Pins.HubSignedEdited), so a raised count
// of clears wins whichever of the two holds it. The store is what a clear is
// checked against, so a store holding the record beats a copy's clear: a verified
// reply came after that clear, or the sysop has set it again.
//
// A store that cannot be read, or holds no entry, leaves each of these as the
// leaf has it, and the record in force (see Pins.CoordUnread).
//
// Call it once the leaf has stopped. Work it started can still be finishing, so
// it takes the save locks; a store after that would land unseen by the
// replacement, so the leaf stores nothing more. A reply verified after this
// reaches the replacement through HandLateRepliesTo.
func (l *Leaf) Retire(p Pins) (Pins, UnsavedState) {
	defer l.lockStores()()

	l.pinMu.Lock()
	l.pins.retired = true
	coord, hub := l.pins.state(KeyCoordinator), l.pins.state(KeyHub)
	signedUnsaved := l.pins.hubSigned && l.pins.hubSignedUnsaved
	signedCfg := l.pins.hubSignedCfg
	l.pinMu.Unlock()

	var u UnsavedState
	settle := func(kind KeyKind, st pinState, pin, pending *string, edited bool, unsaved, movedFrom *string) (read bool) {
		var replaced bool
		*pin, *pending, replaced, read = l.currentFilePin(kind, st, *pin, *pending, edited)
		if !read {
			*pin, *pending = st.pin, l.PendingKey(kind)
			if st.unsaved {
				*unsaved, *movedFrom = st.pin, st.movedFrom
			}
			return false
		}
		if !st.unsaved || replaced {
			return true
		}
		*unsaved, *movedFrom = st.pin, st.movedFrom
		if normalizeKey(*pin) == st.pin {
			// The file holds it, as in installFilePin.
			*movedFrom = ""
		}
		return true
	}
	p.CoordUnread = !settle(KeyCoordinator, coord, &p.Coord, &p.PendingCoord, p.CoordEdited, &u.Coord, &u.CoordMovedFrom)
	p.HubUnread = !settle(KeyHub, hub, &p.Hub, &p.PendingHub, p.HubEdited, &u.Hub, &u.HubMovedFrom)
	u.HubSigned = l.settleHubSigned(&p, signedUnsaved, signedCfg)
	return p, u
}

// settleHubSigned brings p's hubSigned record up to date for Retire, for a leaf
// with signedUnsaved a record not yet stored and signedCfg the value it knows
// the config to hold, and reports whether the record not yet stored is carried.
func (l *Leaf) settleHubSigned(p *Pins, signedUnsaved, signedCfg bool) bool {
	edited := p.HubSignedEdited
	if l.cfg.HubSignedStored != nil {
		stored, err := l.cfg.HubSignedStored()
		if err != nil {
			l.noteStoreUnread("hubSigned", err)
			p.HubSignedUnread = true
			p.HubSigned = p.HubSigned || signedCfg
			return signedUnsaved
		}
		edited = edited || stored.Signed != p.HubSigned || stored.Clears > p.HubSignedClears
		p.HubSigned, p.HubSignedClears = stored.Signed, stored.Clears
	}
	if !p.HubSigned && (edited || signedCfg) {
		l.pinMu.Lock()
		l.pins.hubSignedCleared = true
		l.pinMu.Unlock()
	}
	return signedUnsaved && !p.HubSigned && !edited
}

// HandLateRepliesTo makes next, this retired leaf's replacement for the same
// network and hub, the one told if a hub signature first verifies here from now
// on, or did since Retire. A reply under way when the leaf stopped would
// otherwise be forgotten, and next would take unsigned replies until its own
// first verified reply. It only ever sets the record, and not if the sysop's
// clear reached this leaf or next (see keyPins.hubSignedCleared). Next stores
// what it is told. A leaf with no replacement on the same hub, EndStores' case,
// is never given one: what it learned says nothing about another hub.
func (l *Leaf) HandLateRepliesTo(next *Leaf) {
	l.pinMu.Lock()
	if l.pins.hubSignedCleared {
		l.pinMu.Unlock()
		return
	}
	l.pins.hubSignedNext = next
	late := l.pins.hubSignedLate
	l.pinMu.Unlock()
	if late {
		next.lateHubSigned()
	}
}

// lateHubSigned records a reply a predecessor verified, as noteHubSigned does a
// reply of this leaf's own, unless the sysop's clear has reached this leaf.
func (l *Leaf) lateHubSigned() {
	l.pinMu.Lock()
	cleared := l.pins.hubSignedCleared
	l.pinMu.Unlock()
	if !cleared {
		l.noteHubSigned()
	}
}

// applyHubSigned installs the config's hubSigned, v, when it differs from
// the value the leaf last knew the config to hold, so a copy of the file
// that predates a record the leaf has not stored yet does not undo it,
// while the sysop setting or clearing it takes effect at once.
//
// A clear needs more care. A reload can read the file before
// OnHubSigned stores the record and be applied after, by when the leaf
// takes the file to hold it; that stale copy looks just like the sysop
// clearing it. So a clear is checked against the store as it is now
// (Config.HubSignedStored), read under the same lock noteHubSigned holds
// while it stores, so the read sees every store that has finished. A
// store that cannot be read, or holds no entry, leaves the record in force,
// and sets p.HubSignedUnread.
//
// A record OnHubSigned failed to store is in force although the config
// lacks it. A reload that says nothing about it (edited false) keeps it and
// the retry, so saving something else does not change what the leaf
// enforces. One where the sysop wrote the field (edited, see
// Pins.HubSignedEdited) and whose store confirms the clear drops it, as an
// edited pin replaces an unsaved one: otherwise the sysop clearing hubSigned
// after a hub rollback would be undone by the retried store. A later
// verified reply records it again, since the hub then signs. Only a record
// the first read saw is dropped: noteHubSigned can set one between the two
// reads, before it waits for the lock to store it, and that one was not
// checked against the store, so it is left for noteHubSigned.
//
// A store's count of clears above p.HubSignedClears is an edit too: the copy
// predates that clear, and p returns the store's count for the next reload to
// be told against, so that reload would not see it. So a stale copy's lower
// count leaves the next reload's baseline alone, while a count really lowered
// in the file lowers it, and the next rise counts.
//
// Once the record is cleared, with requireSignedHub off, a pause caused
// only by unsigned replies lifts at once, even for a hub whose /info
// serves no key to resume on.
func (l *Leaf) applyHubSigned(p *Pins) {
	l.hubSignedSaveMu.Lock()
	defer l.hubSignedSaveMu.Unlock()

	v, edited := p.HubSigned, p.HubSignedEdited
	l.pinMu.Lock()
	cfg, unsaved := l.pins.hubSignedCfg, l.pins.hubSignedUnsaved
	l.pinMu.Unlock()
	if l.hubSignedReadHook != nil {
		l.hubSignedReadHook()
	}
	var stored StoredHubSigned
	var storeErr error
	if l.cfg.HubSignedStored != nil {
		if stored, storeErr = l.cfg.HubSignedStored(); storeErr == nil {
			edited = edited || stored.Clears > p.HubSignedClears
			p.HubSignedClears = stored.Clears
		}
	}
	clearing := !v && (cfg || (edited && unsaved))
	if clearing && storeErr != nil {
		l.noteStoreUnread("hubSigned", storeErr)
		p.HubSignedUnread = true
		return
	}
	if clearing && l.cfg.HubSignedStored != nil {
		v = stored.Signed
	}

	l.pinMu.Lock()
	if !v && (clearing || edited) {
		l.pins.hubSignedCleared = true
	}
	dropped := clearing && !v && l.pins.hubSignedUnsaved
	changed := v != l.pins.hubSignedCfg || dropped
	if changed {
		l.pins.hubSignedCfg = v
		l.pins.hubSigned = v
		l.pins.hubSignedUnsaved = false
	}
	l.pinMu.Unlock()
	switch {
	case dropped:
		slog.Info("leaf: config does not record that the hub signs; dropping the record not yet stored",
			"network", l.cfg.Network, "hub", l.cfg.HubURL)
	case changed:
		slog.Info("leaf: hub signing record changed in config",
			"network", l.cfg.Network, "hub", l.cfg.HubURL, "hub_signed", v)
	default:
		return
	}
	if !v {
		l.liftUnsignedPause()
	}
}

// UnsavedState returns what the leaf holds that its caller's store does not
// yet. The service uses it to carry that state across a leaf rebuild. Safe
// for concurrent use.
func (l *Leaf) UnsavedState() UnsavedState {
	l.pinMu.Lock()
	defer l.pinMu.Unlock()
	var u UnsavedState
	if l.pins.coordUnsaved {
		u.Coord, u.CoordMovedFrom = l.pins.coord, l.pins.coordMovedFrom
	}
	if l.pins.hubUnsaved {
		u.Hub, u.HubMovedFrom = l.pins.hub, l.pins.hubMovedFrom
	}
	u.HubSigned = l.pins.hubSigned && l.pins.hubSignedUnsaved
	return u
}

// RestoreUnsaved installs u, taken from an earlier leaf for the same
// subscription, so the stores it was waiting on are retried as they would
// have been. Call it before Start.
func (l *Leaf) RestoreUnsaved(u UnsavedState) {
	l.pinMu.Lock()
	defer l.pinMu.Unlock()
	restore := func(kind KeyKind, key, movedFrom string) {
		if key == "" {
			return
		}
		pin, pending, unsaved := l.pins.slots(kind)
		*pin, *unsaved = key, true
		*l.pins.movedFrom(kind) = movedFrom
		if *pending == key {
			*pending = ""
		}
	}
	restore(KeyCoordinator, u.Coord, u.CoordMovedFrom)
	restore(KeyHub, u.Hub, u.HubMovedFrom)
	if u.HubSigned {
		l.pins.hubSigned, l.pins.hubSignedUnsaved = true, true
	}
}

// LastNAL returns the last NAL this leaf accepted, however old, or nil.
// The service uses it to carry a trusted NAL across a leaf rebuild.
func (l *Leaf) LastNAL() *protocol.NAL {
	if l.nalCache == nil {
		return nil
	}
	return l.nalCache.Last(l.cfg.Network)
}

// SeedNAL installs n as this leaf's last accepted NAL, provided it is for
// this network and signed by the pinned coordinator key; it reports whether
// it did. Call it before Start, with a NAL an earlier leaf for the same
// subscription accepted, so a rebuild does not lose the trusted area list
// while the hub serves a key the sysop has not accepted.
func (l *Leaf) SeedNAL(n *protocol.NAL) bool {
	if n == nil || l.nalCache == nil || n.Network != l.cfg.Network {
		return false
	}
	if pin := l.PinnedKey(KeyCoordinator); pin == "" || pin != n.CoordPubKeyB64 {
		return false
	}
	l.nalCache.Put(l.cfg.Network, n)
	return true
}

// PinnedKey returns the key currently pinned for kind, or "" when none is
// pinned yet. Safe for concurrent use.
func (l *Leaf) PinnedKey(kind KeyKind) string {
	l.pinMu.Lock()
	defer l.pinMu.Unlock()
	pin, _, _ := l.pins.slots(kind)
	return *pin
}

// PendingKey returns the most recent key offered for kind that differs from
// the pin, or "" when there is none. Safe for concurrent use.
func (l *Leaf) PendingKey(kind KeyKind) string {
	l.pinMu.Lock()
	defer l.pinMu.Unlock()
	_, pending, _ := l.pins.slots(kind)
	return *pending
}

// checkNAL accepts a signature-verified NAL only if its coordinator node ID
// matches its key and that key is the pinned one, pinning it first if no key
// is pinned yet, or moving the pin to it along the NAL's handovers. A
// mismatch returns an error wrapping nal.ErrCoordKeyMismatch or
// nal.ErrCoordNodeIDMismatch.
func (l *Leaf) checkNAL(n *protocol.NAL) error {
	if err := nal.VerifyCoordNodeID(n); err != nil {
		return err
	}
	l.followHandovers(KeyCoordinator, n.CoordPubKeyB64, n.Handovers)
	pinned, ok := l.checkPin(KeyCoordinator, n.CoordPubKeyB64)
	if !ok {
		return fmt.Errorf("%w: pinned %s, NAL signed by %s", nal.ErrCoordKeyMismatch, pinned, n.CoordPubKeyB64)
	}
	return nil
}

// checkPin compares offered with the pinned key for kind. With no pin, it
// pins offered (trust on first use), tells OnKeyPinned and returns true; it
// also tells OnKeyPinned when offered matches the pin and was the pending
// key, which means the sysop accepted it and the pending record can go.
// If OnKeyPinned fails, the pin still holds in memory and the store is
// retried on each later matching fetch until it succeeds.
// With a different pin, it records offered as pending, reports it through
// OnKeyChanged the first time it is offered, and returns false with the
// pinned key. The callbacks run without pinMu held.
func (l *Leaf) checkPin(kind KeyKind, offered string) (pinned string, ok bool) {
	l.pinMu.Lock()
	pin, pending, unsaved := l.pins.slots(kind)
	if *pin == "" || *pin == offered {
		first := *pin == ""
		// A pending key that is now the pin was accepted by the sysop.
		accepted := *pending != "" && *pending == offered
		*pin = offered
		if accepted {
			*pending = ""
		}
		if first || accepted {
			*unsaved = true
		}
		retry := *unsaved
		l.pinMu.Unlock()
		switch {
		case first:
			slog.Info("leaf: pinned key on first use",
				"network", l.cfg.Network, "hub", l.cfg.HubURL, "key", string(kind), "pubkey", offered)
		case accepted:
			slog.Info("leaf: sysop-accepted key now in use",
				"network", l.cfg.Network, "hub", l.cfg.HubURL, "key", string(kind), "pubkey", offered)
		}
		if retry {
			l.savePin(kind)
		}
		return offered, true
	}
	pinned = *pin
	reported := *pending == offered
	*pending = offered
	l.pinMu.Unlock()

	if reported {
		slog.Warn("leaf: hub still offers a changed key; rejected",
			"network", l.cfg.Network, "hub", l.cfg.HubURL, "key", string(kind),
			"pinned", pinned, "offered", offered)
		return pinned, false
	}
	slog.Error("leaf: key changed; rejected until the sysop accepts it",
		"network", l.cfg.Network, "hub", l.cfg.HubURL, "key", string(kind),
		"pinned", pinned, "offered", offered,
		"accept", "verify the new key with the network operator, then replace or blank the pin in v3net.json")
	l.reportKeyChanged(kind, pinned, offered)
	return pinned, false
}

// reportKeyChanged hands a changed key to OnKeyChanged, which records it in the
// caller's store, unless the leaf was retired. It holds the save lock for kind,
// so Retire and EndStores wait for it, and a reload's read of the store sees it.
func (l *Leaf) reportKeyChanged(kind KeyKind, pinned, offered string) {
	if l.cfg.OnKeyChanged == nil {
		return
	}
	mu := l.saveMu(kind)
	mu.Lock()
	defer mu.Unlock()
	l.pinMu.Lock()
	retired := l.pins.retired
	l.pinMu.Unlock()
	if retired {
		return
	}
	l.cfg.OnKeyChanged(KeyEvent{Network: l.cfg.Network, HubURL: l.cfg.HubURL, Kind: kind, Pinned: pinned, Offered: offered})
}

// savePin hands the unsaved pin for kind to OnKeyPinned and, if it was
// stored (or there is nowhere to store it), clears the unsaved flag,
// provided the pin is still the same key. A failure leaves the flag set for
// the next fetch.
//
// Saves of one kind run one at a time, each reading the pin and the key a
// handover moved it from when its turn comes. Two handovers in quick
// succession (A to B, then B to C) therefore store B, then C in place of B;
// were they to overlap, C's save would name A as the key it replaces, find B
// in the file and store nothing, while still clearing the unsaved flag.
func (l *Leaf) savePin(kind KeyKind) {
	mu := l.saveMu(kind)
	mu.Lock()
	defer mu.Unlock()

	l.pinMu.Lock()
	pin, _, unsaved := l.pins.slots(kind)
	key, previous, need := *pin, *l.pins.movedFrom(kind), *unsaved
	retired := l.pins.retired
	l.pinMu.Unlock()
	if !need || key == "" || retired {
		// A save that ran ahead of this one stored it already, or the leaf
		// that replaced this one now owns the store, or the subscription is
		// gone (see Retire, EndStores).
		return
	}
	if l.cfg.OnKeyPinned != nil {
		ev := KeyEvent{Network: l.cfg.Network, HubURL: l.cfg.HubURL, Kind: kind, Pinned: key, Offered: key, Previous: previous}
		if err := l.cfg.OnKeyPinned(ev); err != nil {
			slog.Warn("leaf: could not store pinned key; will retry at the next fetch",
				"network", l.cfg.Network, "hub", l.cfg.HubURL, "key", string(kind), "error", err)
			return
		}
	}
	l.pinMu.Lock()
	defer l.pinMu.Unlock()
	switch moved := l.pins.movedFrom(kind); {
	case *pin == key:
		*unsaved = false
		*moved = ""
	case *moved != "":
		// A later handover moved the pin on meanwhile; the caller now
		// holds key, so that is what the next store replaces.
		*moved = key
	}
}

// saveMu returns the lock that orders stores of the pin for kind.
func (l *Leaf) saveMu(kind KeyKind) *sync.Mutex {
	if kind == KeyHub {
		return &l.hubSaveMu
	}
	return &l.coordSaveMu
}

// followHandovers moves the pin for kind to offered when the handovers lead
// from the pin to it (see nal.FollowHandovers), and reports whether it did.
// With no pin, or offered already pinned, there is nothing to follow; with
// no chain the pin stays and checkPin rejects offered as before. The moved
// pin is marked unsaved, so the next checkPin hands it to OnKeyPinned.
func (l *Leaf) followHandovers(kind KeyKind, offered string, hs []protocol.CoordHandover) bool {
	from := l.PinnedKey(kind)
	if from == "" || from == offered || len(hs) == 0 {
		return false
	}
	chain, err := nal.FollowHandovers(l.cfg.Network, from, offered, hs)
	if err != nil {
		slog.Debug("leaf: handovers do not lead to the offered key",
			"network", l.cfg.Network, "hub", l.cfg.HubURL, "key", string(kind), "error", err)
		return false
	}

	l.pinMu.Lock()
	pin, pending, unsaved := l.pins.slots(kind)
	if *pin != from {
		// The sysop changed the pin while the chain was checked.
		l.pinMu.Unlock()
		return false
	}
	*pin = offered
	if *pending == offered {
		*pending = ""
	}
	if moved := l.pins.movedFrom(kind); *moved == "" {
		*moved = from
	}
	*unsaved = true
	l.pinMu.Unlock()

	slog.Warn("leaf: key moved by a signed coordinator handover",
		"network", l.cfg.Network, "hub", l.cfg.HubURL, "key", string(kind),
		"from", from, "to", offered, "steps", len(chain))
	return true
}

// followHubHandover moves the hub pin to offered, the key /info now serves,
// when the hub signs its NAL with that key and the NAL's handovers lead to
// it from the pinned hub key. That is a hub acting as its own coordinator
// after a handover: one key change, which the coordinator pin follows when
// the NAL is next checked. A NAL that cannot be fetched or verified leaves
// the pin as it is.
func (l *Leaf) followHubHandover(ctx context.Context, offered string) {
	if pin := l.PinnedKey(KeyHub); pin == "" || pin == offered {
		return
	}
	n, err := nal.Fetch(ctx, l.cfg.HubURL+fmt.Sprintf("/v3net/v1/%s/nal", l.cfg.Network), l.client)
	if err != nil {
		slog.Debug("leaf: no NAL to check the hub key's handovers against", "network", l.cfg.Network, "error", err)
		return
	}
	if n.Network != l.cfg.Network || n.CoordPubKeyB64 != offered || nal.Verify(n) != nil {
		return
	}
	l.followHandovers(KeyHub, offered, n.Handovers)
}

// checkHubKey fetches the network's /info and checks its hub key against the
// pin, pinning it on first use or moving it along a signed handover. A hub that does not publish its key (older
// hubs) is skipped. A mismatch pauses syncing with the hub and returns an
// error wrapping ErrHubKeyMismatch; a key that matches lifts any pause.
func (l *Leaf) checkHubKey(ctx context.Context) error {
	path := fmt.Sprintf("/v3net/v1/%s/info", l.cfg.Network)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.cfg.HubURL+path, nil)
	if err != nil {
		return fmt.Errorf("leaf: create GET request: %w", err)
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return fmt.Errorf("leaf: GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }() // read side
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("leaf: GET %s: status %d", path, resp.StatusCode)
	}
	body, err := readBody(resp.Body, maxRespBytes)
	if err != nil {
		return fmt.Errorf("leaf: GET %s: %w", path, err)
	}
	var info protocol.NetworkInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return fmt.Errorf("leaf: decode network info: %w", err)
	}
	if info.HubPubKeyB64 == "" {
		slog.Debug("leaf: hub does not publish its key; not pinned", "network", l.cfg.Network, "hub", l.cfg.HubURL)
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(info.HubPubKeyB64)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return fmt.Errorf("leaf: network info has an invalid hub key")
	}
	if want := keystore.NodeIDFromPubKey(raw); info.HubNodeID != want {
		return fmt.Errorf("%w: info names %q, key derives %q", ErrHubNodeIDMismatch, info.HubNodeID, want)
	}
	l.followHubHandover(ctx, info.HubPubKeyB64)
	if pinned, ok := l.checkPin(KeyHub, info.HubPubKeyB64); !ok {
		l.pauseHub("hub key does not match the pinned key")
		return fmt.Errorf("%w: pinned %s, hub offered %s", ErrHubKeyMismatch, pinned, info.HubPubKeyB64)
	}
	l.resumeHub()
	return nil
}

// normalizeKey trims the whitespace a sysop may paste around a key.
func normalizeKey(k string) string {
	return strings.TrimSpace(k)
}
