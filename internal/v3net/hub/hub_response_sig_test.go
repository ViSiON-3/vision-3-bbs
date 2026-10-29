package hub

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// doRequest sends a request and returns the response with its body.
func doRequest(t *testing.T, req *http.Request) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, body
}

// Every response to a signed request carries the hub's signature over that
// request's signature, the status and the body: successes, refusals and the
// subscribe bootstrap alike. Public endpoints answering unsigned requests
// are left alone.
func TestHubSignsResponses(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	strangerKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "stranger.key"))
	if err != nil {
		t.Fatalf("load stranger keystore: %v", err)
	}
	registerLeaf(t, ts, leafKS)

	reqs := map[string]*http.Request{
		"messages": signedRequest(t, leafKS, "GET", ts.URL+"/v3net/v1/testnet/messages?since=&limit=10", ""),
		"refused":  signedRequest(t, strangerKS, "GET", ts.URL+"/v3net/v1/testnet/messages", ""),
		"subscribe": signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/subscribe",
			`{"network":"testnet","node_id":"`+leafKS.NodeID()+`","pubkey_b64":"`+leafKS.PubKeyBase64()+`","bbs_name":"Test BBS"}`),
	}
	for name, req := range reqs {
		reqSig := req.Header.Get(headerSignature)
		resp, body := doRequest(t, req)
		sig := resp.Header.Get(protocol.HeaderHubSignature)
		if sig == "" {
			t.Errorf("%s: response (%d) is unsigned", name, resp.StatusCode)
			continue
		}
		if err := protocol.VerifyResponse(hubKS.PubKey(), reqSig, resp.StatusCode, body, sig); err != nil {
			t.Errorf("%s: hub signature does not verify: %v", name, err)
		}
		if name == "refused" && resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("refused: status %d, want 401", resp.StatusCode)
		}
	}

	resp, _ := doRequest(t, httptestGet(t, ts.URL+"/v3net/v1/testnet/info"))
	if sig := resp.Header.Get(protocol.HeaderHubSignature); sig != "" {
		t.Errorf("unsigned /info request got a hub signature %q", sig)
	}
}

func httptestGet(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return req
}

// sseEvent is one event as read off the wire.
type sseEvent struct {
	typ, id, sig, data string
}

// readSSEEvents reads n events from an SSE body, field by field.
func readSSEEvents(t *testing.T, body io.Reader, n int) []sseEvent {
	t.Helper()
	out := make(chan []sseEvent, 1)
	go func() {
		var evs []sseEvent
		var cur sseEvent
		sc := bufio.NewScanner(body)
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				evs = append(evs, cur)
				cur = sseEvent{}
				if len(evs) == n {
					out <- evs
					return
				}
			case strings.HasPrefix(line, "event: "):
				cur.typ = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "id: "):
				cur.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "sig: "):
				cur.sig = strings.TrimPrefix(line, "sig: ")
			case strings.HasPrefix(line, "data: "):
				cur.data = strings.TrimPrefix(line, "data: ")
			}
		}
		out <- evs
	}()
	select {
	case evs := <-out:
		if len(evs) != n {
			t.Fatalf("read %d events, want %d", len(evs), n)
		}
		return evs
	case <-time.After(5 * time.Second):
		t.Fatal("timed out reading SSE events")
		return nil
	}
}

// The stream's headers are signed as a response with an empty body, and
// each event carries the next sequence number and a signature bound to the
// network, the request that opened the stream, that number, its type and
// its data.
func TestHubSignsSSEEvents(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	registerLeaf(t, ts, leafKS)

	req := signedRequest(t, leafKS, "GET", ts.URL+"/v3net/v1/testnet/events", "")
	reqSig := req.Header.Get(headerSignature)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("SSE connect: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SSE status %d", resp.StatusCode)
	}
	if err := protocol.VerifyResponse(hubKS.PubKey(), reqSig, http.StatusOK, nil, resp.Header.Get(protocol.HeaderHubSignature)); err != nil {
		t.Fatalf("SSE response signature: %v", err)
	}

	for _, text := range []string{"one", "two"} {
		ev, _ := protocol.NewEvent(protocol.EventChat, protocol.ChatPayload{From: "Test", Node: "test", Text: text})
		h.broadcaster.Publish("testnet", ev)
	}
	evs := readSSEEvents(t, resp.Body, 2)
	for i, ev := range evs {
		seq, err := strconv.ParseUint(ev.id, 10, 64)
		if err != nil || seq != uint64(i+1) {
			t.Errorf("event %d: id %q, want %d", i, ev.id, i+1)
			continue
		}
		if err := protocol.VerifyEvent(hubKS.PubKey(), "testnet", reqSig, seq, ev.typ, []byte(ev.data), ev.sig); err != nil {
			t.Errorf("event %d: signature: %v", i, err)
		}
	}
	// The first event replayed in the second's place does not verify.
	if err := protocol.VerifyEvent(hubKS.PubKey(), "testnet", reqSig, 2, evs[0].typ, []byte(evs[0].data), evs[0].sig); err == nil {
		t.Error("event 1 verified as event 2")
	}
}
