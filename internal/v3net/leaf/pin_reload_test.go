package leaf

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
)

// pinsFor is a reload's copy of the file holding key as kind's pin.
func pinsFor(kind KeyKind, key, pending string, edited bool) Pins {
	if kind == KeyHub {
		return Pins{Hub: key, PendingHub: pending, HubEdited: edited}
	}
	return Pins{Coord: key, PendingCoord: pending, CoordEdited: edited}
}

// A reload that read the file before the leaf stored its pin, and applies
// after, leaves the stored pin in force, whether the file's pin looks edited
// or not (#817).
func TestUpdatePinsCopyReadBeforeStore(t *testing.T) {
	for _, kind := range []KeyKind{KeyCoordinator, KeyHub} {
		for _, edited := range []bool{false, true} {
			a, b := newCoordKey(t), newCoordKey(t)
			l, stored := handoverLeaf(t, kind, a, b)
			copyOfFile := *stored
			l.savePin(kind) // the store lands after the copy was read
			if *stored != b.PubKeyBase64() || copyOfFile != a.PubKeyBase64() {
				t.Fatalf("store holds %s, copy %s; want B, A", *stored, copyOfFile)
			}

			l.UpdatePins(nil, pinsFor(kind, copyOfFile, "stale-pending", edited)) //nolint:staticcheck // nil ctx: the leaf is not running
			if got := l.PinnedKey(kind); got != b.PubKeyBase64() {
				t.Errorf("%s edited=%v: pin = %s, want the stored B", kind, edited, got)
			}
			if got := l.PendingKey(kind); got != "" {
				t.Errorf("%s edited=%v: pending = %q, want the stale copy's ignored", kind, edited, got)
			}
		}
	}
}

// A first-use pin the leaf stored after the copy was read is not blanked (#817).
func TestUpdatePinsCopyReadBeforeFirstUseStore(t *testing.T) {
	l, _ := setupLeaf(t, "http://unused.invalid", &mockJAMWriter{})
	key := newCoordKey(t).PubKeyBase64()
	stored := ""
	l.cfg.OnKeyPinned = func(ev KeyEvent) error { stored = ev.Pinned; return nil }
	l.cfg.PinStored = func(k KeyKind) (StoredPin, error) {
		if k != KeyCoordinator {
			return StoredPin{}, ErrNoStoredEntry
		}
		return StoredPin{Pin: stored}, nil
	}
	if _, ok := l.checkPin(KeyCoordinator, key); !ok || stored != key {
		t.Fatalf("first use not stored: ok=%v, store %q", ok, stored)
	}

	l.UpdatePins(nil, Pins{}) //nolint:staticcheck // nil ctx: the leaf is not running
	if got := l.PinnedKey(KeyCoordinator); got != key {
		t.Errorf("pin = %q, want the stored first-use pin", got)
	}
}

// A sysop's edit in the store wins, whether the copy holds it or it was written
// after the copy was read, and including one back to a key the leaf held
// earlier. Its pending key comes with it (#817).
func TestUpdatePinsEditInStoreWins(t *testing.T) {
	for name, unsaved := range map[string]bool{"stored pin": false, "unsaved pin": true} {
		t.Run(name, func(t *testing.T) {
			a, b, c, sysop := newCoordKey(t), newCoordKey(t), newCoordKey(t), newCoordKey(t)
			for _, edit := range []struct {
				name, file, want string
				edited           bool
			}{
				{"edit in the copy", sysop.PubKeyBase64(), sysop.PubKeyBase64(), true},
				{"edit back in the copy", a.PubKeyBase64(), a.PubKeyBase64(), true},
				{"edit after the copy", b.PubKeyBase64(), sysop.PubKeyBase64(), false},
				{"edit back after the copy", b.PubKeyBase64(), a.PubKeyBase64(), false},
			} {
				l, stored := handoverLeaf(t, KeyCoordinator, a, b, c)
				if !unsaved {
					l.savePin(KeyCoordinator)
				}
				*stored = edit.want
				pending := "pending-" + edit.name
				l.cfg.PinStored = func(k KeyKind) (StoredPin, error) {
					if k != KeyCoordinator {
						return StoredPin{}, ErrNoStoredEntry
					}
					return StoredPin{Pin: *stored, Pending: pending}, nil
				}

				l.UpdatePins(nil, Pins{Coord: edit.file, CoordEdited: edit.edited}) //nolint:staticcheck // nil ctx: the leaf is not running
				if got := l.PinnedKey(KeyCoordinator); got != edit.want {
					t.Errorf("%s: pin = %s, want %s", edit.name, got, edit.want)
				}
				if got := l.PendingKey(KeyCoordinator); got != pending {
					t.Errorf("%s: pending = %q, want the store's", edit.name, got)
				}
				if u := l.UnsavedState(); u.Coord != "" {
					t.Errorf("%s: unsaved %+v, want none", edit.name, u)
				}
			}
		})
	}
}

// A handover that lands between the reload's read of the store and its update
// of the pin is not undone, even by a reload that looks like an edit: the
// verdict is reached again with the pin as it now is. The store that follows,
// which waits on the save lock, then lands over the right key (#817).
func TestUpdatePinsHandoverBetweenReadAndApply(t *testing.T) {
	start, a, b := newCoordKey(t), newCoordKey(t), newCoordKey(t)
	l, stored := handoverLeaf(t, KeyCoordinator, start, a)
	l.savePin(KeyCoordinator)
	hs := handoverChain(t, a, b)

	var saves sync.WaitGroup
	fired := false
	l.pinReadHook = func(KeyKind) {
		if fired {
			return
		}
		fired = true
		if !l.followHandovers(KeyCoordinator, b.PubKeyBase64(), hs) {
			t.Error("handover not followed")
		}
		saves.Add(1)
		go func() {
			defer saves.Done()
			l.savePin(KeyCoordinator)
		}()
	}
	l.UpdatePins(nil, Pins{Coord: a.PubKeyBase64(), CoordEdited: true}) //nolint:staticcheck // nil ctx: the leaf is not running
	saves.Wait()

	if got := l.PinnedKey(KeyCoordinator); got != b.PubKeyBase64() {
		t.Errorf("pin = %s, want the handover's B", got)
	}
	if *stored != b.PubKeyBase64() {
		t.Errorf("store holds %s, want B", *stored)
	}
}

// A store and reloads running together never move the pin back along the
// chain, whatever copy of the file each reload holds. Run with -race (#817).
func TestUpdatePinsConcurrentWithStores(t *testing.T) {
	keys := make([]*keystore.Keystore, 40)
	index := make(map[string]int, len(keys))
	for i := range keys {
		keys[i] = newCoordKey(t)
		index[keys[i].PubKeyBase64()] = i
	}
	hs := handoverChain(t, keys...)

	l, _ := setupLeaf(t, "http://unused.invalid", &mockJAMWriter{})
	l.pins.coord = keys[0].PubKeyBase64()
	var mu sync.Mutex
	stored := keys[0].PubKeyBase64()
	read := func() string {
		mu.Lock()
		defer mu.Unlock()
		return stored
	}
	l.cfg.OnKeyPinned = func(ev KeyEvent) error {
		mu.Lock()
		defer mu.Unlock()
		stored = ev.Pinned
		return nil
	}
	l.cfg.PinStored = func(k KeyKind) (StoredPin, error) {
		if k != KeyCoordinator {
			return StoredPin{}, ErrNoStoredEntry
		}
		return StoredPin{Pin: read()}, nil
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i, k := range keys[1:] {
			if !l.followHandovers(KeyCoordinator, k.PubKeyBase64(), hs[i:i+1]) {
				t.Errorf("handover to key %d not followed: pin is key %d", i+1, index[l.PinnedKey(KeyCoordinator)])
				return
			}
			l.savePin(KeyCoordinator)
		}
	}()

	prev, high := read(), 0
	for running := true; running; {
		select {
		case <-done:
			running = false
		default:
		}
		// The reload reads the file, then the pin moves on, then it applies.
		file := read()
		l.UpdatePins(nil, Pins{Coord: file, CoordEdited: file != prev}) //nolint:staticcheck // nil ctx: the leaf is not running
		prev = file
		got := index[l.PinnedKey(KeyCoordinator)]
		if got < high {
			t.Errorf("pin moved back from key %d to key %d", high, got)
		}
		high = got
	}
	<-done
	if got := l.PinnedKey(KeyCoordinator); got != keys[len(keys)-1].PubKeyBase64() {
		t.Errorf("pin = %s, want the last key", got)
	}
	if got := read(); got != keys[len(keys)-1].PubKeyBase64() {
		t.Errorf("store holds %s, want the last key", got)
	}
}

// Retire brings the copy up to date with the store, keeps what the store does
// not hold unless the copy or the store shows an edit, and stops further
// stores (#817).
func TestRetire(t *testing.T) {
	a, b, c, sysop := newCoordKey(t), newCoordKey(t), newCoordKey(t), newCoordKey(t)

	t.Run("copy read before a store", func(t *testing.T) {
		l, stored := handoverLeaf(t, KeyHub, a, b)
		l.savePin(KeyHub)
		p, u := l.Retire(Pins{Hub: a.PubKeyBase64(), PendingHub: "stale-pending"})
		if p.Hub != *stored || p.PendingHub != "" {
			t.Errorf("pins %+v, want the store's %s", p, *stored)
		}
		if u != (UnsavedState{}) {
			t.Errorf("unsaved %+v, want none", u)
		}
	})

	t.Run("unsaved pin following the store", func(t *testing.T) {
		l, stored := handoverLeaf(t, KeyCoordinator, a, b, c)
		p, u := l.Retire(Pins{Coord: a.PubKeyBase64(), CoordEdited: true})
		if p.Coord != *stored {
			t.Errorf("pins %+v, want the store's %s", p, *stored)
		}
		if u.Coord != c.PubKeyBase64() || u.CoordMovedFrom != *stored {
			t.Errorf("unsaved %+v, want C moving from the store's", u)
		}
	})

	t.Run("edit back to an earlier key", func(t *testing.T) {
		l, stored := handoverLeaf(t, KeyCoordinator, a, b, c)
		*stored = a.PubKeyBase64()
		p, u := l.Retire(Pins{Coord: b.PubKeyBase64()})
		if p.Coord != a.PubKeyBase64() || u.Coord != "" {
			t.Errorf("pins %+v, unsaved %+v; want the sysop's A and the pin dropped", p, u)
		}
	})

	t.Run("edit in the copy with no store to read", func(t *testing.T) {
		l, _ := handoverLeaf(t, KeyCoordinator, a, b, c)
		l.cfg.PinStored = nil
		p, u := l.Retire(Pins{Coord: sysop.PubKeyBase64(), CoordEdited: true})
		if p.Coord != sysop.PubKeyBase64() || u.Coord != "" {
			t.Errorf("pins %+v, unsaved %+v; want the copy's edit", p, u)
		}
	})

	t.Run("no store after", func(t *testing.T) {
		l, stored := handoverLeaf(t, KeyCoordinator, a, b)
		before := *stored
		_, u := l.Retire(Pins{Coord: before})
		if u.Coord != b.PubKeyBase64() {
			t.Fatalf("unsaved %+v, want B carried", u)
		}
		l.savePin(KeyCoordinator)
		if *stored != before {
			t.Errorf("a retired leaf stored %s", *stored)
		}
	})
}

// With a store that cannot be read or holds no entry, the copy may be the stale
// one, edit or not, so the leaf's pin, pending key and hubSigned record stand,
// and the caller is told (#821).
func TestUpdatePinsKeepsLeafWhenStoreUnread(t *testing.T) {
	for name, err := range map[string]error{"unreadable": errors.New("gone"), "no entry": ErrNoStoredEntry} {
		t.Run(name, func(t *testing.T) {
			a, b := newCoordKey(t), newCoordKey(t)
			l, _ := handoverLeaf(t, KeyCoordinator, a, b)
			l.pins.pendingCoord = "pending-of-the-leaf"
			l.pins.hubSigned, l.pins.hubSignedCfg = true, true
			l.cfg.PinStored = func(KeyKind) (StoredPin, error) { return StoredPin{}, err }
			l.cfg.HubSignedStored = func() (StoredHubSigned, error) { return StoredHubSigned{}, err }

			p := l.UpdatePins(nil, Pins{Coord: newCoordKey(t).PubKeyBase64(), PendingCoord: "copy", CoordEdited: true, HubSignedEdited: true}) //nolint:staticcheck // nil ctx: the leaf is not running
			if !p.CoordUnread || !p.HubUnread || !p.HubSignedUnread {
				t.Errorf("unread flags %+v, want all set", p)
			}
			if got := l.PinnedKey(KeyCoordinator); got != b.PubKeyBase64() {
				t.Errorf("pin = %s, want the leaf's B", got)
			}
			if got := l.PendingKey(KeyCoordinator); got != "pending-of-the-leaf" {
				t.Errorf("pending = %q, want the leaf's", got)
			}
			if u := l.UnsavedState(); u.Coord != b.PubKeyBase64() || u.CoordMovedFrom != a.PubKeyBase64() {
				t.Errorf("unsaved %+v, want B still moving from A", u)
			}
			if !l.HubSigned() {
				t.Error("the record was dropped, relaxing what the leaf enforces")
			}
		})
	}
}

// Each field reports its own read, so a clear that was checked and applied is not
// reported as unread because a pin's read failed after it (#821).
func TestUnreadFlagsPerField(t *testing.T) {
	newLeaf := func(pinErr, signedErr error) *Leaf {
		l, _ := setupLeaf(t, "http://unused.invalid", &mockJAMWriter{})
		l.pins.hubSigned, l.pins.hubSignedCfg = true, true
		l.cfg.PinStored = func(KeyKind) (StoredPin, error) { return StoredPin{}, pinErr }
		l.cfg.HubSignedStored = func() (StoredHubSigned, error) { return StoredHubSigned{}, signedErr }
		return l
	}
	gone := errors.New("gone")
	copyOf := Pins{HubSignedClears: 1, HubSignedEdited: true}

	l := newLeaf(gone, nil)
	p := l.UpdatePins(nil, copyOf) //nolint:staticcheck // nil ctx: the leaf is not running
	if !p.CoordUnread || !p.HubUnread || p.HubSignedUnread || l.HubSigned() {
		t.Errorf("pins unread: %+v, hubSigned=%v; want the clear applied and only the pins unread", p, l.HubSigned())
	}
	p, _ = newLeaf(gone, nil).Retire(copyOf)
	if !p.CoordUnread || !p.HubUnread || p.HubSignedUnread || p.HubSigned {
		t.Errorf("Retire, pins unread: %+v; want the clear applied and only the pins unread", p)
	}

	l = newLeaf(nil, gone)
	p = l.UpdatePins(nil, copyOf) //nolint:staticcheck // nil ctx: the leaf is not running
	if p.CoordUnread || p.HubUnread || !p.HubSignedUnread || !l.HubSigned() {
		t.Errorf("hubSigned unread: %+v, hubSigned=%v; want the record kept and only it unread", p, l.HubSigned())
	}
	p, _ = newLeaf(nil, gone).Retire(copyOf)
	if p.CoordUnread || p.HubUnread || !p.HubSignedUnread || !p.HubSigned {
		t.Errorf("Retire, hubSigned unread: %+v; want the record kept and only it unread", p)
	}
}

// Retire treats a store it cannot read the same way (#821).
func TestRetireKeepsLeafWhenStoreUnread(t *testing.T) {
	for name, err := range map[string]error{"unreadable": errors.New("gone"), "no entry": ErrNoStoredEntry} {
		t.Run(name, func(t *testing.T) {
			a, b := newCoordKey(t), newCoordKey(t)
			l, _ := handoverLeaf(t, KeyCoordinator, a, b)
			l.pins.pendingCoord = "pending-of-the-leaf"
			l.cfg.PinStored = func(KeyKind) (StoredPin, error) { return StoredPin{}, err }

			p, u := l.Retire(Pins{Coord: newCoordKey(t).PubKeyBase64(), PendingCoord: "copy", CoordEdited: true})
			if !p.CoordUnread || !p.HubUnread {
				t.Errorf("unread flags %+v, want the pins' set", p)
			}
			if p.Coord != b.PubKeyBase64() || p.PendingCoord != "pending-of-the-leaf" {
				t.Errorf("pins %+v, want the leaf's", p)
			}
			if u.Coord != b.PubKeyBase64() || u.CoordMovedFrom != a.PubKeyBase64() {
				t.Errorf("unsaved %+v, want B still moving from A", u)
			}
		})
	}
}

// Retire brings the hubSigned record up to date with the store, so a replacement
// never starts without a record in force, unless the sysop cleared it (#821).
func TestRetireHubSigned(t *testing.T) {
	type leafState struct{ signed, unsaved, cfg bool }
	var (
		noRecord = leafState{}
		unsaved  = leafState{signed: true, unsaved: true}
		saved    = leafState{signed: true, cfg: true}
	)
	gone := errors.New("gone")
	cases := []struct {
		name   string
		leaf   leafState
		copyOf Pins
		noRead bool // no store to read
		stored StoredHubSigned
		err    error

		wantSigned, wantCarried, wantUnread bool
		wantClears                          int
	}{
		{name: "stored after the copy was read", leaf: saved, stored: StoredHubSigned{Signed: true}, wantSigned: true},
		{name: "stored by another writer", leaf: noRecord, stored: StoredHubSigned{Signed: true}, wantSigned: true},
		{name: "not yet stored", leaf: unsaved, wantCarried: true},
		{name: "not yet stored, copy holds it", leaf: unsaved, copyOf: Pins{HubSigned: true}, stored: StoredHubSigned{Signed: true}, wantSigned: true},
		{
			name: "unsaved, cleared in the copy", leaf: unsaved,
			copyOf: Pins{HubSignedClears: 1, HubSignedEdited: true}, stored: StoredHubSigned{Clears: 1},
			wantClears: 1,
		},
		{name: "unsaved, cleared since the copy", leaf: unsaved, stored: StoredHubSigned{Clears: 1}, wantClears: 1},
		{
			name: "stored, cleared in the copy", leaf: saved,
			copyOf: Pins{HubSignedClears: 1, HubSignedEdited: true}, stored: StoredHubSigned{Clears: 1},
			wantClears: 1,
		},
		{name: "stored, cleared since the copy", leaf: saved, copyOf: Pins{HubSigned: true}, stored: StoredHubSigned{Clears: 1}, wantClears: 1},
		{
			name: "recorded again since the clear in the copy", leaf: saved,
			copyOf: Pins{HubSignedClears: 1, HubSignedEdited: true}, stored: StoredHubSigned{Signed: true, Clears: 1},
			wantSigned: true, wantClears: 1,
		},
		{
			name: "unreadable, stored, clear in the copy", leaf: saved,
			copyOf: Pins{HubSignedClears: 1, HubSignedEdited: true}, err: gone,
			wantSigned: true, wantUnread: true, wantClears: 1,
		},
		{
			name: "unreadable, unsaved, clear in the copy", leaf: unsaved,
			copyOf: Pins{HubSignedClears: 1, HubSignedEdited: true}, err: gone,
			wantCarried: true, wantUnread: true, wantClears: 1,
		},
		{name: "no entry, stored", leaf: saved, err: ErrNoStoredEntry, wantSigned: true, wantUnread: true},
		{name: "no entry, unsaved", leaf: unsaved, err: ErrNoStoredEntry, wantCarried: true, wantUnread: true},
		{name: "no store, unsaved", leaf: unsaved, noRead: true, wantCarried: true},
		{name: "no store, unsaved, edited", leaf: unsaved, noRead: true, copyOf: Pins{HubSignedEdited: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l, _ := setupLeaf(t, "http://unused.invalid", &mockJAMWriter{})
			l.pins.hubSigned, l.pins.hubSignedUnsaved, l.pins.hubSignedCfg = c.leaf.signed, c.leaf.unsaved, c.leaf.cfg
			if !c.noRead {
				l.cfg.HubSignedStored = func() (StoredHubSigned, error) { return c.stored, c.err }
			}

			p, u := l.Retire(c.copyOf)
			if p.HubSigned != c.wantSigned || p.HubSignedClears != c.wantClears {
				t.Errorf("record %v with %d clears, want %v with %d", p.HubSigned, p.HubSignedClears, c.wantSigned, c.wantClears)
			}
			if u.HubSigned != c.wantCarried {
				t.Errorf("carried unsaved = %v, want %v", u.HubSigned, c.wantCarried)
			}
			if p.HubSignedUnread != c.wantUnread {
				t.Errorf("HubSignedUnread = %v, want %v", p.HubSignedUnread, c.wantUnread)
			}
		})
	}
}

// Retire waits for a store of the record that is under way, and sees it (#821).
func TestRetireWaitsForHubSignedStore(t *testing.T) {
	l, _ := setupLeaf(t, "http://unused.invalid", &mockJAMWriter{})
	var mu sync.Mutex
	file := false
	entered, release := make(chan struct{}), make(chan struct{})
	l.cfg.OnHubSigned = func() error {
		close(entered)
		<-release
		mu.Lock()
		defer mu.Unlock()
		file = true
		return nil
	}
	l.cfg.HubSignedStored = func() (StoredHubSigned, error) {
		mu.Lock()
		defer mu.Unlock()
		return StoredHubSigned{Signed: file}, nil
	}
	go l.noteHubSigned()
	<-entered

	retired := make(chan Pins)
	go func() {
		p, _ := l.Retire(Pins{})
		retired <- p
	}()
	select {
	case <-retired:
		t.Fatal("Retire did not wait for the store under way")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if p := <-retired; !p.HubSigned {
		t.Error("the replacement would start without the record just stored")
	}
}

// A retired leaf, replaced or with its subscription removed, stores nothing
// more: not a pin, not a pending key, not hubSigned (#817, #821).
func TestRetiredLeafStoresNothing(t *testing.T) {
	for name, retire := range map[string]func(*Leaf){
		"Retire":    func(l *Leaf) { l.Retire(Pins{}) },
		"EndStores": (*Leaf).EndStores,
	} {
		t.Run(name, func(t *testing.T) {
			l, _ := setupLeaf(t, "http://unused.invalid", &mockJAMWriter{})
			var stores []string
			l.cfg.OnKeyPinned = func(KeyEvent) error { stores = append(stores, "pin"); return nil }
			l.cfg.OnKeyChanged = func(KeyEvent) { stores = append(stores, "pending") }
			l.cfg.OnHubSigned = func() error { stores = append(stores, "hubSigned"); return nil }
			retire(l)

			first, other := newCoordKey(t).PubKeyBase64(), newCoordKey(t).PubKeyBase64()
			if _, ok := l.checkPin(KeyCoordinator, first); !ok {
				t.Fatal("first use refused")
			}
			if _, ok := l.checkPin(KeyCoordinator, other); ok {
				t.Fatal("a changed key was accepted")
			}
			l.noteHubSigned()
			if len(stores) != 0 {
				t.Errorf("a retired leaf attempted stores: %v", stores)
			}
		})
	}
}

// EndStores waits for a record of a changed key that is under way, so none lands
// after it (#821).
func TestEndStoresWaitsForKeyChanged(t *testing.T) {
	l, _ := setupLeaf(t, "http://unused.invalid", &mockJAMWriter{})
	l.pins.coord = newCoordKey(t).PubKeyBase64()
	entered, release := make(chan struct{}), make(chan struct{})
	l.cfg.OnKeyChanged = func(KeyEvent) {
		close(entered)
		<-release
	}
	go l.checkPin(KeyCoordinator, newCoordKey(t).PubKeyBase64())
	<-entered

	ended := make(chan struct{})
	go func() {
		l.EndStores()
		close(ended)
	}()
	select {
	case <-ended:
		t.Fatal("EndStores did not wait for the record under way")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-ended
}
