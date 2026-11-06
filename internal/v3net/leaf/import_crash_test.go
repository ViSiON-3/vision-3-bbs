package leaf

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// errStopped stands in for the BBS stopping in the middle of an import.
var errStopped = errors.New("BBS stopped")

// storeWriter is a JAM base that can be told to stop the BBS just before or
// just after its next write, by panicking with errStopped. It is a JAMFinder
// unless noFind is set.
type storeWriter struct {
	mu         sync.Mutex
	messages   []protocol.Message
	stopBefore string // UUID whose write stops the BBS before storing it
	stopAfter  string // UUID whose write stops the BBS after storing it
	failOnce   string // UUID whose next write returns an error
	failStored string // UUID whose next write stores it, then returns an error
	findErr    error  // returned by FindMessage while set
	findCalls  int
}

func (w *storeWriter) WriteMessage(msg protocol.Message) (int64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch msg.MsgUUID {
	case w.stopBefore:
		w.stopBefore = ""
		panic(errStopped)
	case w.failOnce:
		w.failOnce = ""
		return 0, errors.New("disk full")
	}
	w.messages = append(w.messages, msg)
	if msg.MsgUUID == w.failStored {
		w.failStored = ""
		return 0, errors.New("sync failed")
	}
	if msg.MsgUUID == w.stopAfter {
		w.stopAfter = ""
		panic(errStopped)
	}
	return int64(len(w.messages)), nil
}

func (w *storeWriter) FindMessage(msg protocol.Message) (int64, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.findCalls++
	if w.findErr != nil {
		return 0, false, w.findErr
	}
	for i, m := range w.messages {
		if m.MsgUUID == msg.MsgUUID && m.OriginNode == msg.OriginNode {
			return int64(i + 1), true, nil
		}
	}
	return 0, false, nil
}

// uuids lists the stored messages' UUIDs in order.
func (w *storeWriter) uuids() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []string
	for _, m := range w.messages {
		out = append(out, m.MsgUUID)
	}
	return out
}

// plainWriter is a storeWriter that cannot look for a message.
type plainWriter struct{ w *storeWriter }

func (p plainWriter) WriteMessage(msg protocol.Message) (int64, error) { return p.w.WriteMessage(msg) }

// newSinceHub serves msgs from a sinceHub (see message_sig_test.go), which
// honours the poll cursor, so a test can tell whether it moved past a message.
func newSinceHub(t *testing.T, msgs ...protocol.Message) (*sinceHub, *httptest.Server) {
	t.Helper()
	h := &sinceHub{messages: msgs}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return h, ts
}

func (h *sinceHub) add(msgs ...protocol.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = append(h.messages, msgs...)
}

// pollStopped polls and expects the BBS to stop (errStopped) mid-import.
func pollStopped(t *testing.T, l *Leaf) {
	t.Helper()
	defer func() {
		if r := recover(); r != errStopped {
			t.Fatalf("poll recovered %v, want the BBS to stop mid-import", r)
		}
	}()
	_, _ = l.Poll(context.Background())
	t.Fatal("poll finished; the BBS was meant to stop mid-import")
}

func pollOK(t *testing.T, l *Leaf) {
	t.Helper()
	if _, err := l.Poll(context.Background()); err != nil {
		t.Fatalf("poll: %v", err)
	}
}

func wantStored(t *testing.T, w *storeWriter, want ...string) {
	t.Helper()
	got := w.uuids()
	if len(got) != len(want) {
		t.Fatalf("stored %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stored %v, want %v", got, want)
		}
	}
}

// Message UUIDs for the tests below: p is polled before the others.
var (
	uP = sigUUID(10)
	uA = sigUUID(11)
	uB = sigUUID(12)
)

// The BBS stops after a message is written to JAM but before the dedup index
// records it. The next poll fetches it again and must not write a second
// copy (#920).
func TestImportStoppedAfterWriteIsNotDuplicated(t *testing.T) {
	_, ts := newSinceHub(t, testMessage(uA), testMessage(uB))
	w := &storeWriter{stopAfter: uA}
	l, ix := setupLeaf(t, ts.URL, w)

	pollStopped(t, l)
	pollOK(t, l)
	wantStored(t, w, uA, uB)
	if seen, pending, err := ix.Lookup("testnet", "test.example.net", uA); err != nil || !seen || pending {
		t.Errorf("a: seen=%v pending=%v err=%v; want recorded and confirmed", seen, pending, err)
	}
	pollOK(t, l)
	wantStored(t, w, uA, uB)
}

// The BBS stops after the import claims a message but before it is written.
// The poll cursor must not move past it, so the next poll fetches and writes
// it, once.
func TestImportStoppedBeforeWriteIsNotLost(t *testing.T) {
	hub, ts := newSinceHub(t, testMessage(uP))
	w := &storeWriter{stopBefore: uA}
	l, _ := setupLeaf(t, ts.URL, w)

	pollOK(t, l)
	hub.add(testMessage(uA), testMessage(uB))
	pollStopped(t, l)
	pollOK(t, l)
	wantStored(t, w, uP, uA, uB)
	if w.findCalls != 1 {
		t.Errorf("JAM was asked %d times for an unfinished import, want once (for a)", w.findCalls)
	}
}

// A write that fails releases the claim, so the message is not recorded and
// a later copy is imported as new, as before claims.
func TestImportWriteFailureReleasesClaim(t *testing.T) {
	_, ts := newSinceHub(t, testMessage(uA))
	w := &storeWriter{failOnce: uA}
	l, ix := setupLeaf(t, ts.URL, w)

	pollOK(t, l)
	if seen, _, err := ix.Lookup("testnet", "test.example.net", uA); err != nil || seen {
		t.Errorf("a after a failed write: seen=%v err=%v; want not recorded", seen, err)
	}
	pollOK(t, l)
	wantStored(t, w, uA)
}

// A JAM writer that cannot look for a message gets an unfinished import
// written again: the most the leaf can do without one.
func TestImportUnfinishedWithoutFinderIsWrittenAgain(t *testing.T) {
	_, ts := newSinceHub(t, testMessage(uA))
	w := &storeWriter{stopAfter: uA}
	l, ix := setupLeaf(t, ts.URL, plainWriter{w})

	pollStopped(t, l)
	pollOK(t, l)
	wantStored(t, w, uA, uA)
	if seen, pending, _ := ix.Lookup("testnet", "test.example.net", uA); !seen || pending {
		t.Errorf("a: seen=%v pending=%v; want confirmed", seen, pending)
	}
}

// When JAM cannot be checked for an unfinished import, the poll stops before
// a later message can become the cursor; once it can, the message is found,
// not written again, and the later one follows.
func TestImportUncheckableClaimStopsPoll(t *testing.T) {
	hub, ts := newSinceHub(t, testMessage(uA))
	w := &storeWriter{stopAfter: uA}
	l, ix := setupLeaf(t, ts.URL, w)

	pollStopped(t, l)
	hub.add(testMessage(uB))
	w.mu.Lock()
	w.findErr = errors.New("disk read error")
	w.mu.Unlock()
	if _, err := l.Poll(context.Background()); err == nil {
		t.Fatal("poll with an unfinished import JAM cannot check returned no error")
	}
	wantStored(t, w, uA)
	if last, _, _ := ix.LastSeen("testnet"); last != "" {
		t.Errorf("cursor moved to %q past the unsettled import", last)
	}

	w.mu.Lock()
	w.findErr = nil
	w.mu.Unlock()
	pollOK(t, l)
	wantStored(t, w, uA, uB)
}

// A write that fails after storing the message (a failed sync, say) is found
// in JAM and confirmed, not released and written again.
func TestImportWriteErrorAfterStoreIsConfirmed(t *testing.T) {
	_, ts := newSinceHub(t, testMessage(uA))
	w := &storeWriter{failStored: uA}
	l, ix := setupLeaf(t, ts.URL, w)

	pollOK(t, l)
	if seen, pending, _ := ix.Lookup("testnet", "test.example.net", uA); !seen || pending {
		t.Errorf("a: seen=%v pending=%v; want confirmed", seen, pending)
	}
	pollOK(t, l)
	wantStored(t, w, uA)
}
