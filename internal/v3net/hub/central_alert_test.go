package hub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// alertFor builds and signs ks's alert of kind on testnet, published at
// now and valid for two minutes.
func alertFor(t *testing.T, ks *keystore.Keystore, kind, handle string, now time.Time) *protocol.CentralAlert {
	t.Helper()
	id, err := protocol.NewCentralAlertID()
	if err != nil {
		t.Fatal(err)
	}
	now = now.UTC().Truncate(time.Second)
	a := &protocol.CentralAlert{
		V3NetCentral: protocol.CentralAlertVersion, Kind: kind, ID: id, Network: "testnet",
		BBS: "Board " + ks.NodeID()[:4], Handle: handle,
		PublishedAt: now.Format(time.RFC3339), ExpiresAt: now.Add(2 * time.Minute).Format(time.RFC3339),
	}
	switch kind {
	case protocol.CentralAlertChatJoin:
		a.Room = "lobby"
	case protocol.CentralAlertPost:
		a.Area = "fel.general"
	}
	if err := a.Sign(ks); err != nil {
		t.Fatal(err)
	}
	return a
}

// postAlert sends a as ks and returns the status and decoded reply.
func postAlert(t *testing.T, ts *httptest.Server, ks *keystore.Keystore, a *protocol.CentralAlert) (int, protocol.CentralAlertResponse, string) {
	t.Helper()
	b, _ := json.Marshal(a)
	resp, err := http.DefaultClient.Do(signedRequest(t, ks, http.MethodPost, ts.URL+"/v3net/v1/testnet/central/alerts", string(b)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var r protocol.CentralAlertResponse
	_ = json.Unmarshal(body, &r)
	return resp.StatusCode, r, string(body)
}

// nextAlertEvent waits for the next central_alert event, skipping others.
func nextAlertEvent(t *testing.T, events <-chan protocol.Event, wait time.Duration) (protocol.Event, bool) {
	t.Helper()
	deadline := time.After(wait)
	for {
		select {
		case ev := <-events:
			if ev.Type == protocol.EventCentralAlert {
				return ev, true
			}
		case <-deadline:
			return protocol.Event{}, false
		}
	}
}

// The hub names activity alerts in /info.
func TestNetworkInfoAdvertisesAlerts(t *testing.T) {
	if !slices.Contains(hubFeatures, protocol.FeatureCentralAlerts) {
		t.Error("hub does not advertise central.alerts")
	}
}

// An alert is relayed to the network byte for byte, so its board's
// signature still verifies; a retry of it is acknowledged as a duplicate
// and not relayed again.
func TestCentralAlertRelayedOnceAsSigned(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	a := newLeafKS(t)
	registerLeaf(t, ts, a)
	events, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()

	for _, kind := range protocol.CentralAlertKinds {
		alert := alertFor(t, a, kind, "Alice", time.Now())
		code, r, body := postAlert(t, ts, a, alert)
		if code != http.StatusOK || !r.OK || r.Duplicate {
			t.Fatalf("%s: %d %s", kind, code, body)
		}
		ev, ok := nextAlertEvent(t, events, time.Second)
		if !ok {
			t.Fatalf("%s: not relayed", kind)
		}
		var got protocol.CentralAlert
		if err := json.Unmarshal(ev.Data, &got); err != nil {
			t.Fatal(err)
		}
		if err := got.Verify(); err != nil {
			t.Errorf("%s: relayed alert does not verify: %v", kind, err)
		}
		if got != *alert {
			t.Errorf("%s: relayed %+v, sent %+v", kind, got, *alert)
		}

		// A retry (lost reply) is acknowledged and not sent on again.
		code, r, _ = postAlert(t, ts, a, alert)
		if code != http.StatusOK || !r.Duplicate {
			t.Errorf("%s: retry: %d %+v", kind, code, r)
		}
		if _, ok := nextAlertEvent(t, events, 100*time.Millisecond); ok {
			t.Errorf("%s: retry relayed again", kind)
		}
	}
}

// The hub refuses an alert that is not the sending board's, is signed by
// another key, is forged after signing, has expired, or is dated ahead.
// None of them is relayed.
func TestCentralAlertRefusals(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	a, b := newLeafKS(t), newLeafKS(t)
	registerLeaf(t, ts, a)
	registerLeaf(t, ts, b)
	events, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()

	// b sends a's alert: not b's.
	if code, _, _ := postAlert(t, ts, b, alertFor(t, a, protocol.CentralAlertLogon, "Alice", time.Now())); code != http.StatusForbidden {
		t.Errorf("another board's alert: %d, want 403", code)
	}
	// A relay changed the handle after signing.
	forged := alertFor(t, a, protocol.CentralAlertLogon, "Alice", time.Now())
	forged.Handle = "Mallory"
	if code, _, _ := postAlert(t, ts, a, forged); code != http.StatusForbidden {
		t.Errorf("forged alert: %d, want 403", code)
	}
	// Signed by a key the hub does not have for a: unregistered node.
	stranger := newLeafKS(t)
	if code, _, _ := postAlert(t, ts, stranger, alertFor(t, stranger, protocol.CentralAlertLogon, "Eve", time.Now())); code/100 == 2 {
		t.Errorf("unregistered node's alert accepted: %d", code)
	}
	// Old news, replayed after it expired.
	if code, _, _ := postAlert(t, ts, a, alertFor(t, a, protocol.CentralAlertLogon, "Alice", time.Now().Add(-3*time.Minute))); code != http.StatusUnprocessableEntity {
		t.Errorf("expired alert: %d, want 422", code)
	}
	// Dated past the allowed clock skew.
	if code, _, _ := postAlert(t, ts, a, alertFor(t, a, protocol.CentralAlertLogon, "Alice", time.Now().Add(protocol.MaxCentralClockSkew+time.Minute))); code != http.StatusUnprocessableEntity {
		t.Errorf("future alert: %d, want 422", code)
	}
	if ev, ok := nextAlertEvent(t, events, 100*time.Millisecond); ok {
		t.Errorf("a refused alert was relayed: %s", ev.Data)
	}
}

// IDs are forgotten once their alerts expire, and not before.
func TestAlertSeenPrune(t *testing.T) {
	s := newAlertSeen()
	now := time.Now().UTC().Truncate(time.Second)
	a := &protocol.CentralAlert{NodeID: "n", Network: "testnet", ID: "abcdefghijklmnopqrstuv", ExpiresAt: now.Add(time.Minute).Format(time.RFC3339)}
	if !s.claim(a) {
		t.Fatal("first claim refused")
	}
	s.prune(now)
	if s.claim(a) {
		t.Error("claimed again before expiry")
	}
	// The same ID from another board, or on another network, is its own.
	other := *a
	other.NodeID = "m"
	if !s.claim(&other) {
		t.Error("another board's alert with the same ID refused")
	}
	s.prune(now.Add(time.Minute))
	if len(s.ids) != 0 {
		t.Errorf("%d IDs kept past expiry", len(s.ids))
	}
}
