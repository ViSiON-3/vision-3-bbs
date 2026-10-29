package leaf

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// signingPagedHub is a hub that signs its replies and serves pages[n] to
// the nth poll, as a MessagePage when the leaf asks for format=page and the
// hub knows it (pageForm), else as a bare array with X-V3Net-Has-More, as a
// signing hub from before format=page does. forgeHeader sets
// X-V3Net-Has-More on every page, as a proxy could.
type signingPagedHub struct {
	ks          *keystore.Keystore
	pages       [][]protocol.Message
	pageForm    bool
	forgeHeader bool

	mu      sync.Mutex
	formats []string
}

func (h *signingPagedHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/info") {
		_ = json.NewEncoder(w).Encode(protocol.NetworkInfo{
			Name: "testnet", HubNodeID: h.ks.NodeID(), HubPubKeyB64: h.ks.PubKeyBase64(),
		})
		return
	}
	h.mu.Lock()
	n := len(h.formats)
	format := r.URL.Query().Get("format")
	h.formats = append(h.formats, format)
	h.mu.Unlock()

	var page []protocol.Message
	if n < len(h.pages) {
		page = h.pages[n]
	}
	more := n+1 < len(h.pages)
	var body []byte
	if h.pageForm && format == protocol.PollFormatPage {
		body, _ = json.Marshal(protocol.MessagePage{Messages: page, HasMore: more})
	} else {
		body, _ = json.Marshal(page)
		if more {
			w.Header().Set("X-V3Net-Has-More", "true")
		}
	}
	if h.forgeHeader {
		w.Header().Set("X-V3Net-Has-More", "true")
	}
	sig, _ := protocol.SignResponse(h.ks, r.Header.Get("X-V3Net-Signature"), r.Header.Get(protocol.HeaderNonce), http.StatusOK, body)
	w.Header().Set(protocol.HeaderHubSignature, sig)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

func (h *signingPagedHub) requests() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.formats...)
}

// pagedLeaf returns a leaf polling hub, with the hub's key pinned.
func pagedLeaf(t *testing.T, hub *signingPagedHub) (*Leaf, *mockJAMWriter) {
	t.Helper()
	ts := httptest.NewServer(hub)
	t.Cleanup(ts.Close)
	writer := &mockJAMWriter{}
	l, _ := setupLeaf(t, ts.URL, writer)
	l.pins.hub = hub.ks.PubKeyBase64()
	return l, writer
}

// A leaf asks for format=page and follows has_more in the signed body.
func TestPollFollowsSignedHasMore(t *testing.T) {
	hub := &signingPagedHub{ks: newCoordKey(t), pageForm: true, pages: [][]protocol.Message{
		{testMessage(testUUID1)},
		{testMessage(testUUID2), testMessage(testUUID3)},
	}}
	l, writer := pagedLeaf(t, hub)
	if n, err := l.Poll(context.Background()); err != nil || n != 3 || writer.count() != 3 {
		t.Fatalf("Poll = %d, %v (wrote %d); want all 3 messages", n, err, writer.count())
	}
	if got := hub.requests(); len(got) != 2 || got[0] != protocol.PollFormatPage || got[1] != protocol.PollFormatPage {
		t.Errorf("request formats = %q, want format=page on both pages", got)
	}
}

// With has_more in the body, an X-V3Net-Has-More header added on the way is
// ignored: it cannot make the leaf poll for pages the hub did not offer.
func TestPollIgnoresHasMoreHeaderWithPage(t *testing.T) {
	hub := &signingPagedHub{ks: newCoordKey(t), pageForm: true, forgeHeader: true, pages: [][]protocol.Message{
		{testMessage(testUUID1)},
	}}
	l, _ := pagedLeaf(t, hub)
	if _, err := l.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if got := hub.requests(); len(got) != 1 {
		t.Errorf("%d requests; the forged header was followed", len(got))
	}
}

// A signing hub from before format=page (V3NET-RESP-SIG-v1 over a bare
// array) is still accepted, its paging read from the header as before.
func TestPollOlderSigningHubUsesHeader(t *testing.T) {
	hub := &signingPagedHub{ks: newCoordKey(t), pages: [][]protocol.Message{
		{testMessage(testUUID1)},
		{testMessage(testUUID2)},
	}}
	l, writer := pagedLeaf(t, hub)
	if n, err := l.Poll(context.Background()); err != nil || n != 2 || writer.count() != 2 {
		t.Fatalf("Poll = %d, %v (wrote %d); want both pages", n, err, writer.count())
	}
	if l.HubPaused() {
		t.Error("paused by an older signing hub")
	}
}

// has_more changed on the way to the leaf fails the hub's signature, in
// either direction, against a real hub.
func TestPollHasMoreTamperingDetected(t *testing.T) {
	for _, flip := range [][2]string{
		{`"has_more":false`, `"has_more":true`}, // poll for pages the hub did not offer
		{`"has_more":true`, `"has_more":false`}, // stop paging early
	} {
		ts, h, hubKS := newTestHub(t, true)
		seedHubNAL(t, h, hubKS, "gen.general")
		p := newHubProxy(t, ts.URL)
		l := subscribedLeaf(t, p.ts, "Leaf BBS", "gen.general")
		l.pins.hub = hubKS.PubKeyBase64()
		if flip[0] == `"has_more":true` {
			// One more message than a page holds, so the hub says there is more.
			for i := range 101 {
				if err := l.SendMessage(ownMessage(l, sigUUID(i))); err != nil {
					t.Fatalf("SendMessage %d: %v", i, err)
				}
			}
		}
		var flipped bool
		p.setRewrite(func(path string, body []byte) []byte {
			if !strings.HasSuffix(path, "/messages") || !strings.Contains(string(body), flip[0]) {
				return body
			}
			flipped = true
			return []byte(strings.Replace(string(body), flip[0], flip[1], 1))
		})
		if _, err := l.Poll(context.Background()); !errors.Is(err, ErrHubSignature) {
			t.Errorf("%s changed to %s: Poll = %v, want ErrHubSignature", flip[0], flip[1], err)
		}
		if !flipped {
			t.Errorf("the hub's page never carried %s", flip[0])
		}
	}
}
