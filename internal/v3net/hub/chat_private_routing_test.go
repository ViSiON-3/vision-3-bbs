package hub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// Read raw streams, before any leaf-side recipient filtering. A public marker
// queued after the PM bounds the check without a sleep or a negative timeout.
func TestChatPrivate_RawStreamIsolation(t *testing.T) {
	h, hubKS := setupTestHub(t)
	freezeUserLimit(t, h)
	ts := httptest.NewServer(h.newMux())
	t.Cleanup(ts.Close)
	sender := loadTestKeystore(t, "sender.key")
	recipient := loadTestKeystore(t, "recipient.key")
	observer := loadTestKeystore(t, "observer.key")
	for _, ks := range []*keystore.Keystore{sender, recipient, observer} {
		registerLeaf(t, ts, ks)
	}
	// The same node also has an authenticated stream on another network.
	if _, err := h.subscribers.Add(Subscriber{NodeID: recipient.NodeID(), Network: "othernet", PubKeyB64: recipient.PubKeyBase64(), Status: "active"}); err != nil {
		t.Fatal(err)
	}
	base := ts.URL + "/v3net/v1/testnet/chat/rooms/"
	// A node header alone must not let the observer attach as the recipient.
	spoof := signedRequest(t, observer, "GET", ts.URL+"/v3net/v1/testnet/events", "")
	spoof.Header.Set(headerNodeID, recipient.NodeID())
	spoofResp, err := http.DefaultClient.Do(spoof)
	if err != nil {
		t.Fatal(err)
	}
	spoofResp.Body.Close()
	if spoofResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("forged recipient stream: %d", spoofResp.StatusCode)
	}
	for _, ks := range []*keystore.Keystore{recipient, observer} {
		if code := sendSigned(t, ks, "POST", base+"join", `{"room":"lobby","handle":"bob"}`, nil); code != http.StatusOK {
			t.Fatalf("join duplicate handle: %d", code)
		}
	}
	type stream struct {
		name, network string
		req           *http.Request
		resp          *http.Response
		receives      bool
	}
	var streams []stream
	for _, tc := range []struct {
		name, network string
		ks            *keystore.Keystore
		receives      bool
	}{
		{"sender", "testnet", sender, false},
		{"recipient", "testnet", recipient, true},
		{"recipient second stream", "testnet", recipient, true},
		{"observer with duplicate handle", "testnet", observer, false},
		{"recipient on other network", "othernet", recipient, false},
	} {
		req := signedRequest(t, tc.ks, "GET", ts.URL+"/v3net/v1/"+tc.network+"/events", "")
		req.Header.Set(protocol.HeaderNonce, tc.name)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s stream: %d", tc.name, resp.StatusCode)
		}
		if err := protocol.VerifyResponse(hubKS.PubKey(), req.Header.Get(headerSignature), tc.name, resp.StatusCode, nil, resp.Header.Get(protocol.HeaderHubSignature)); err != nil {
			t.Fatalf("%s response signature: %v", tc.name, err)
		}
		streams = append(streams, stream{tc.name, tc.network, req, resp, tc.receives})
	}
	body := fmt.Sprintf(`{"handle":"alice","to_handle":"bob","to_node":%q,"text":"private payload"}`, recipient.NodeID())
	req := signedRequest(t, sender, "POST", base+"private", body)
	req.Header.Set(protocol.HeaderNonce, "private-post")
	resp, reply := doRequest(t, req)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("private status: %d", resp.StatusCode)
	}
	if err := protocol.VerifyResponse(hubKS.PubKey(), req.Header.Get(headerSignature), "private-post", resp.StatusCode, reply, resp.Header.Get(protocol.HeaderHubSignature)); err != nil {
		t.Fatalf("private response signature: %v", err)
	}
	marker, err := protocol.NewEvent(protocol.EventPing, protocol.PingPayload{})
	if err != nil {
		t.Fatal(err)
	}
	for _, network := range []string{"testnet", "othernet"} {
		h.broadcaster.Publish(network, marker)
	}
	for _, s := range streams {
		t.Run(s.name, func(t *testing.T) {
			count := 1
			if s.receives {
				count++
			}
			evs := readSSEEvents(t, s.resp.Body, count)
			for i, ev := range evs {
				seq, err := strconv.ParseUint(ev.id, 10, 64)
				if err != nil || seq != uint64(i+1) {
					t.Fatalf("sequence %q, want %d", ev.id, i+1)
				}
				if err := protocol.VerifyEvent(hubKS.PubKey(), s.network, s.req.Header.Get(headerSignature), s.req.Header.Get(protocol.HeaderNonce), seq, ev.typ, []byte(ev.data), ev.sig); err != nil {
					t.Fatalf("event signature: %v", err)
				}
			}
			if s.receives {
				if evs[0].typ != protocol.EventChatPrivate {
					t.Fatalf("first event: %s", evs[0].typ)
				}
				var msg protocol.ChatMsgPayload
				if err := json.Unmarshal([]byte(evs[0].data), &msg); err != nil {
					t.Fatal(err)
				}
				if msg.ToNode != recipient.NodeID() || msg.ToHandle != "bob" || msg.FromNode != sender.NodeID() || msg.FromHandle != "alice" || msg.Text != "private payload" {
					t.Fatalf("private payload: %+v", msg)
				}
			}
			if last := evs[len(evs)-1]; last.typ != marker.Type || last.data != string(marker.Data) {
				t.Fatalf("expected public marker after exactly %d private events; got %s: %s", count-1, last.typ, last.data)
			}
		})
	}
}

func TestBroadcaster_TargetedDeliveryLifecycle(t *testing.T) {
	b := NewBroadcaster()
	public, cancelPublic := b.Subscribe("net")
	defer cancelPublic()
	target, cancelTarget := b.subscribe("net", "recipient")
	defer cancelTarget()
	other, cancelOther := b.subscribe("net", "observer")
	defer cancelOther()
	foreign, cancelForeign := b.subscribe("othernet", "recipient")
	defer cancelForeign()
	ev := protocol.Event{Type: protocol.EventChatPrivate, Data: json.RawMessage(`{"text":"secret"}`)}

	// An empty destination or an offline node cannot become a broadcast.
	b.publishToNode("net", "", ev)
	b.publishToNode("net", "offline", ev)
	if len(target) != 0 {
		t.Fatal("target received a message for an empty or offline destination")
	}
	// Keep the existing bounded, nonblocking behavior for targeted streams.
	for i := 0; i < eventBufSize+1; i++ {
		b.publishToNode("net", "recipient", ev)
	}
	if len(target) != eventBufSize {
		t.Fatalf("target buffered %d events, want %d", len(target), eventBufSize)
	}
	for name, ch := range map[string]<-chan protocol.Event{"public": public, "observer": other, "other network": foreign} {
		if len(ch) != 0 {
			t.Errorf("private event leaked to %s", name)
		}
	}
	cancelTarget()
	cancelTarget()
	b.publishToNode("net", "recipient", ev)
	for range target {
	}
}
