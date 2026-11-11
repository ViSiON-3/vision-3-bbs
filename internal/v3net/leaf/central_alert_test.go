package leaf

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// allAlerts turns every alert kind on.
func allAlerts() map[string]bool {
	m := map[string]bool{}
	for _, k := range protocol.CentralAlertKinds {
		m[k] = true
	}
	return m
}

// receiveAlerts gives l every alert kind and returns a channel of what
// OnAlert is handed.
func receiveAlerts(t *testing.T, l *Leaf) <-chan protocol.CentralAlert {
	t.Helper()
	got := make(chan protocol.CentralAlert, 16)
	l.cfg.CentralAlerts = allAlerts()
	l.cfg.OnAlert = func(a protocol.CentralAlert) { got <- a }
	if err := l.fetchHubFeatures(context.Background()); err != nil {
		t.Fatal(err)
	}
	return got
}

// noAlert fails if an alert arrives within a short wait.
func noAlert(t *testing.T, got <-chan protocol.CentralAlert, what string) {
	t.Helper()
	select {
	case a := <-got:
		t.Errorf("%s: got %+v", what, a)
	case <-time.After(200 * time.Millisecond):
	}
}

// One board's alert reaches another board through the hub, verified and
// naming the right board, caller, room and area; the sending board does
// not alert itself, and a retry of the same alert is not handed on twice.
func TestCentralAlertBetweenBoards(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	alpha := subscribedLeaf(t, ts, "Alpha")
	beta := subscribedLeaf(t, ts, "Beta")
	alphaGot := receiveAlerts(t, alpha)
	betaGot := receiveAlerts(t, beta)
	watchEvents(t, alpha)
	watchEvents(t, beta)

	for _, spec := range []AlertSpec{
		{Kind: protocol.CentralAlertLogon, Handle: "Alice"},
		{Kind: protocol.CentralAlertLogoff, Handle: "Alice"},
		{Kind: protocol.CentralAlertChatJoin, Handle: "Alice", Room: "lobby"},
		{Kind: protocol.CentralAlertPost, Handle: "Alice", Area: "fel.general"},
	} {
		id, _ := protocol.NewCentralAlertID()
		spec.ID = id
		if err := alpha.SendAlert(context.Background(), spec); err != nil {
			t.Fatalf("%s: send: %v", spec.Kind, err)
		}
		select {
		case a := <-betaGot:
			if a.Kind != spec.Kind || a.ID != id || a.Handle != "Alice" || a.BBS != "Alpha" ||
				a.NodeID != alpha.cfg.Keystore.NodeID() || a.Room != spec.Room || a.Area != spec.Area {
				t.Errorf("%s: got %+v", spec.Kind, a)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: never reached Beta", spec.Kind)
		}
		// A retry, as after a lost reply: the hub acknowledges it and
		// relays nothing.
		if err := alpha.SendAlert(context.Background(), spec); err != nil {
			t.Errorf("%s: retry: %v", spec.Kind, err)
		}
	}
	noAlert(t, betaGot, "retries")
	noAlert(t, alphaGot, "the sending board")
	if st := alpha.AlertStatus(); !st.Supported || st.LastSent.IsZero() || st.LastError != "" {
		t.Errorf("status: %+v", st)
	}
}

// SendAlert sends nothing for a kind the sysop has off, with Central off,
// or for a chat join with network chat off, and the receiving board drops
// alerts of a kind it has off.
func TestCentralAlertSysopSwitches(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	alpha := subscribedLeaf(t, ts, "Alpha")
	beta := subscribedLeaf(t, ts, "Beta")
	receiveAlerts(t, alpha)
	betaGot := receiveAlerts(t, beta)
	watchEvents(t, beta)

	id, _ := protocol.NewCentralAlertID()
	alpha.cfg.CentralAlerts[protocol.CentralAlertLogon] = false
	if err := alpha.SendAlert(context.Background(), AlertSpec{Kind: protocol.CentralAlertLogon, ID: id, Handle: "Alice"}); !errors.Is(err, ErrAlertsOff) {
		t.Errorf("kind off: %v", err)
	}
	alpha.cfg.CentralChat = false
	if err := alpha.SendAlert(context.Background(), AlertSpec{Kind: protocol.CentralAlertChatJoin, ID: id, Handle: "Alice", Room: "lobby"}); !errors.Is(err, ErrAlertsOff) {
		t.Errorf("chat off: %v", err)
	}
	alpha.cfg.CentralChat, alpha.cfg.CentralEnabled = true, false
	if err := alpha.SendAlert(context.Background(), AlertSpec{Kind: protocol.CentralAlertLogoff, ID: id, Handle: "Alice"}); !errors.Is(err, ErrAlertsOff) {
		t.Errorf("Central off: %v", err)
	}
	alpha.cfg.CentralEnabled = true

	// Beta's sysop has posts off: Alpha's post alert is not handed on.
	beta.cfg.CentralAlerts[protocol.CentralAlertPost] = false
	if err := alpha.SendAlert(context.Background(), AlertSpec{Kind: protocol.CentralAlertPost, ID: id, Handle: "Alice", Area: "fel.general"}); err != nil {
		t.Fatal(err)
	}
	noAlert(t, betaGot, "kind off on the receiving board")
}

// On a hub that does not relay alerts, SendAlert sends nothing.
func TestCentralAlertUnsupportedHub(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	alpha := subscribedLeaf(t, ts, "Alpha")
	alpha.cfg.CentralAlerts = allAlerts()
	alpha.noteHubFeatures([]string{protocol.FeatureCentralDirectory})
	id, _ := protocol.NewCentralAlertID()
	if err := alpha.SendAlert(context.Background(), AlertSpec{Kind: protocol.CentralAlertLogon, ID: id, Handle: "Alice"}); !errors.Is(err, ErrAlertsUnsupported) {
		t.Errorf("got %v", err)
	}
}

// The receiving board drops an alert that is expired, forged, from a
// blocked caller, or already handed on, whatever the hub relayed.
func TestCentralAlertDispatchChecks(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	alpha := subscribedLeaf(t, ts, "Alpha")
	beta := subscribedLeaf(t, ts, "Beta")
	got := receiveAlerts(t, beta)

	signed := func(handle string, publish time.Time) protocol.CentralAlert {
		id, _ := protocol.NewCentralAlertID()
		now := publish.UTC().Truncate(time.Second)
		a := protocol.CentralAlert{
			V3NetCentral: protocol.CentralAlertVersion, Kind: protocol.CentralAlertLogon, ID: id, Network: "testnet",
			BBS: "Alpha", Handle: handle, PublishedAt: now.Format(time.RFC3339), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339),
		}
		if err := a.Sign(alpha.cfg.Keystore); err != nil {
			t.Fatal(err)
		}
		return a
	}
	deliver := func(a protocol.CentralAlert) {
		data, _ := json.Marshal(a)
		beta.dispatchAlertEvent(protocol.Event{Type: protocol.EventCentralAlert, Data: data})
	}

	fresh := signed("Alice", time.Now())
	deliver(fresh)
	select {
	case <-got:
	default:
		t.Fatal("fresh alert dropped")
	}
	deliver(fresh)
	noAlert(t, got, "a replay")

	deliver(signed("Alice", time.Now().Add(-2*time.Minute)))
	noAlert(t, got, "an expired alert")

	forged := signed("Alice", time.Now())
	forged.Handle = "Mallory"
	deliver(forged)
	noAlert(t, got, "a forged alert")

	beta.chatSessions.blocked = chatBlockSet([]string{"Troll@" + alpha.cfg.Keystore.NodeID()})
	deliver(signed("Troll", time.Now()))
	noAlert(t, got, "a blocked caller")
}

// A send that cannot reach the hub is retried, then given up once the
// alert would be stale, rather than sent late.
func TestCentralAlertGivesUpWhenStale(t *testing.T) {
	l, _ := setupLeaf(t, deadHubURL(t), &mockJAMWriter{})
	l.cfg.CentralEnabled = true
	l.cfg.CentralAlerts = allAlerts()
	l.noteHubFeatures([]string{protocol.FeatureCentralAlerts})
	oldTTL, oldMin, oldMax, oldMargin := centralAlertTTL, centralAlertRetryMin, centralAlertRetryMax, centralAlertMargin
	centralAlertTTL, centralAlertRetryMin, centralAlertRetryMax, centralAlertMargin = 3*time.Second, 50*time.Millisecond, 100*time.Millisecond, time.Second
	t.Cleanup(func() {
		centralAlertTTL, centralAlertRetryMin, centralAlertRetryMax, centralAlertMargin = oldTTL, oldMin, oldMax, oldMargin
	})
	id, _ := protocol.NewCentralAlertID()
	start := time.Now()
	err := l.SendAlert(context.Background(), AlertSpec{Kind: protocol.CentralAlertLogon, ID: id, Handle: "Alice"})
	if err == nil {
		t.Fatal("send to a dead hub succeeded")
	}
	if time.Since(start) > 4*time.Second {
		t.Errorf("kept retrying for %s", time.Since(start))
	}
	if st := l.AlertStatus(); st.LastError == "" {
		t.Error("no error recorded")
	}
}
