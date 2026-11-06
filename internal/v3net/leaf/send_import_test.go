package leaf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// postingHub is a sinceHub that also takes posts. A posted message is
// stored, then onPost runs before the hub replies: the hub serving the post
// to a poll while its sender still waits for the reply.
type postingHub struct {
	*sinceHub
	onPost func()
}

func (h *postingHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sinceHub.ServeHTTP(w, r)
		return
	}
	var m protocol.Message
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.add(m)
	if h.onPost != nil {
		h.onPost()
	}
	w.WriteHeader(http.StatusCreated)
}

// A poll that fetches this node's own post while the post is still being
// sent must not import it as a second copy: the send records the post
// before the import can look it up (#938).
func TestPollDuringSendDoesNotImportOwnPost(t *testing.T) {
	hub := &postingHub{sinceHub: &sinceHub{}}
	ts := httptest.NewServer(hub)
	t.Cleanup(ts.Close)
	w := &storeWriter{}
	l, ix := setupLeaf(t, ts.URL, w)

	done := make(chan struct{})
	var pollErr error
	hub.onPost = func() {
		go func() {
			defer close(done)
			_, pollErr = l.Poll(context.Background())
		}()
		// Let the poll import the post, if nothing holds it back, before
		// the hub replies to the send.
		select {
		case <-done:
		case <-time.After(300 * time.Millisecond):
		}
	}

	msg := ownMessage(l, uA)
	if err := l.SendPost(context.Background(), msg); err != nil {
		t.Fatalf("SendPost: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the poll did not finish after the send")
	}
	if pollErr != nil {
		t.Fatalf("poll: %v", pollErr)
	}
	wantStored(t, w) // nothing: the post is this node's own
	if seen, pending, err := ix.Lookup("testnet", msg.OriginNode, uA); err != nil || !seen || pending {
		t.Errorf("own post: seen=%v pending=%v err=%v; want recorded and confirmed", seen, pending, err)
	}
}
