package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// subscribeWithSoftware subscribes ks with report, signed unless unsigned
// is set, and fails the test on anything but 200.
func subscribeWithSoftware(t *testing.T, ts *httptest.Server, ks *keystore.Keystore, report *protocol.SoftwareReport, unsigned bool) {
	t.Helper()
	body, err := json.Marshal(protocol.SubscribeRequest{
		Network: "testnet", NodeID: ks.NodeID(), PubKeyB64: ks.PubKeyBase64(),
		BBSName: "Test BBS", BBSHost: "test.example.net", Software: report,
	})
	if err != nil {
		t.Fatal(err)
	}
	url := ts.URL + "/v3net/v1/subscribe"
	var code int
	if unsigned {
		resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		code = resp.StatusCode
	} else {
		code = postSubscribe(t, signedRequest(t, ks, "POST", url, string(body)))
	}
	if code != http.StatusOK {
		t.Fatalf("subscribe: status %d, want 200", code)
	}
}

// listedNode returns the operator's node list entry for nodeID.
func listedNode(t *testing.T, ts *httptest.Server, hubKS *keystore.Keystore, nodeID string) protocol.NodeInfo {
	t.Helper()
	resp := doSigned(t, hubKS, "GET", ts.URL+"/v3net/v1/testnet/nodes")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list nodes: status %d", resp.StatusCode)
	}
	var nodes []protocol.NodeInfo
	if err := json.NewDecoder(resp.Body).Decode(&nodes); err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n.NodeID == nodeID {
			return n
		}
	}
	t.Fatalf("node %s not listed", nodeID)
	return protocol.NodeInfo{}
}

// softwareHub is a hub whose operator can list nodes, with one leaf key.
func softwareHub(t *testing.T) (*Hub, *httptest.Server, *keystore.Keystore, *keystore.Keystore) {
	t.Helper()
	h, hubKS := setupTestHub(t)
	if _, err := h.subscribers.Add(Subscriber{
		NodeID: hubKS.NodeID(), Network: "testnet", PubKeyB64: hubKS.PubKeyBase64(), BBSName: "hub", Status: "active",
	}); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h.newMux())
	t.Cleanup(ts.Close)
	return h, ts, hubKS, loadTestKeystore(t, "leaf.key")
}

func allFeatures() []string {
	return []string{protocol.FeatureCentralDirectory, protocol.FeatureCentralPresence,
		protocol.FeatureCentralLastCallers, protocol.FeatureCentralChat}
}

// A signed subscribe's report reaches the operator's node list with when it
// came, and a later report replaces it, an upgrade or a downgrade alike. A
// subscribe that carries none leaves it as it was.
func TestSoftwareReport_RecordedAndReplaced(t *testing.T) {
	_, ts, hubKS, leafKS := softwareHub(t)

	// Never reported: unknown, not out of date.
	registerLeaf(t, ts, leafKS)
	n := listedNode(t, ts, hubKS, leafKS.NodeID())
	if n.Software != nil {
		t.Errorf("before any report: software %+v", n.Software)
	}
	for _, f := range n.Features {
		if f.State != protocol.FeatureUnknown {
			t.Errorf("before any report: %+v, want unknown", f)
		}
	}

	before := time.Now().Add(-time.Second)
	subscribeWithSoftware(t, ts, leafKS, &protocol.SoftwareReport{
		Name: protocol.SoftwareNameVision3, Version: "0.9.7", Build: "a23f12a7c0de",
		Capabilities: []string{protocol.FeatureCentralDirectory},
	}, false)
	n = listedNode(t, ts, hubKS, leafKS.NodeID())
	if n.Software.String() != "ViSiON/3 0.9.7 (a23f12a7c0de)" {
		t.Errorf("software = %q", n.Software.String())
	}
	if at, err := time.Parse(time.RFC3339, n.SoftwareReportedAt); err != nil || at.Before(before.Truncate(time.Second)) {
		t.Errorf("reported at %q (%v), want about now", n.SoftwareReportedAt, err)
	}
	if n.Online {
		t.Error("node with no event stream listed online")
	}
	states := map[string]string{}
	for _, f := range n.Features {
		states[f.Feature] = f.State
	}
	if states[protocol.FeatureCentralDirectory] != protocol.FeatureSupported ||
		states[protocol.FeatureCentralChat] != protocol.FeatureNeedsUpgrade {
		t.Errorf("features = %v", states)
	}

	// Upgraded.
	subscribeWithSoftware(t, ts, leafKS, &protocol.SoftwareReport{
		Name: protocol.SoftwareNameVision3, Version: "0.9.8", Capabilities: allFeatures(),
	}, false)
	n = listedNode(t, ts, hubKS, leafKS.NodeID())
	if n.Software.String() != "ViSiON/3 0.9.8" {
		t.Errorf("after upgrade: software = %q", n.Software.String())
	}
	for _, f := range n.Features {
		if f.State != protocol.FeatureSupported {
			t.Errorf("after upgrade: %+v, want supported", f)
		}
	}

	// Downgraded to a release that still reports.
	subscribeWithSoftware(t, ts, leafKS, &protocol.SoftwareReport{
		Name: protocol.SoftwareNameVision3, Version: "0.9.7", Capabilities: []string{protocol.FeatureCentralDirectory},
	}, false)
	n = listedNode(t, ts, hubKS, leafKS.NodeID())
	if n.Software.String() != "ViSiON/3 0.9.7" {
		t.Errorf("after downgrade: software = %q", n.Software.String())
	}

	// A signed subscribe with no report: the config editor subscribing to an
	// area in the board's name, or a leaf from before reports. The record,
	// and when it came, stay as they were.
	reportedAt := n.SoftwareReportedAt
	subscribeWithSoftware(t, ts, leafKS, nil, false)
	n = listedNode(t, ts, hubKS, leafKS.NodeID())
	if n.Software.String() != "ViSiON/3 0.9.7" || n.SoftwareReportedAt != reportedAt {
		t.Errorf("subscribe without a report changed the record to %q at %q", n.Software.String(), n.SoftwareReportedAt)
	}
}

// The config editor's area browser subscribes in the board's name, signed,
// with area tags and no report. Managing areas must not erase what the
// running board reported.
func TestSoftwareReport_AreaSubscribeKeepsReport(t *testing.T) {
	h, ts, hubKS, leafKS := softwareHub(t)
	seedTestNAL(t, h, hubKS)
	subscribeWithSoftware(t, ts, leafKS, &protocol.SoftwareReport{Name: protocol.SoftwareNameVision3, Version: "0.9.8"}, false)

	registerLeafWithAreas(t, ts, leafKS, []string{"gen.general"})
	if n := listedNode(t, ts, hubKS, leafKS.NodeID()); n.Software.String() != "ViSiON/3 0.9.8" {
		t.Errorf("area subscribe changed the report to %q", n.Software.String())
	}
}

// Anyone can send an unsigned subscribe naming a known node, so it must not
// change what the hub records about that node's software.
func TestSoftwareReport_UnsignedIgnored(t *testing.T) {
	h, ts, hubKS, leafKS := softwareHub(t)
	h.cfg.RequireSignedSubscribe = false
	subscribeWithSoftware(t, ts, leafKS, &protocol.SoftwareReport{Name: protocol.SoftwareNameVision3, Version: "0.9.8"}, false)

	subscribeWithSoftware(t, ts, leafKS, &protocol.SoftwareReport{Name: "Forged", Version: "6.6.6"}, true)
	if n := listedNode(t, ts, hubKS, leafKS.NodeID()); n.Software.String() != "ViSiON/3 0.9.8" {
		t.Errorf("unsigned subscribe changed the report to %q", n.Software.String())
	}
}

// A report that breaks the limits is dropped whole and the subscribe still
// succeeds; the node then reads as not reported, rather than keeping a report
// it no longer stands by.
func TestSoftwareReport_MalformedDropped(t *testing.T) {
	_, ts, hubKS, leafKS := softwareHub(t)
	subscribeWithSoftware(t, ts, leafKS, &protocol.SoftwareReport{Name: protocol.SoftwareNameVision3, Version: "0.9.8"}, false)

	for _, bad := range []*protocol.SoftwareReport{
		{Name: "ViSiON/3\x1b]0;owned\x07", Version: "0.9.8"},
		{Name: protocol.SoftwareNameVision3, Version: strings.Repeat("9", 200)},
		{Name: protocol.SoftwareNameVision3, Capabilities: []string{"NOT A FEATURE"}},
	} {
		subscribeWithSoftware(t, ts, leafKS, bad, false)
		if n := listedNode(t, ts, hubKS, leafKS.NodeID()); n.Software != nil || n.SoftwareReportedAt == "" {
			t.Errorf("malformed report %+v: record %+v at %q, want cleared with its time", bad, n.Software, n.SoftwareReportedAt)
		}
	}
}

// A node with its event stream open is listed online, so the operator can
// tell its report is from its current run.
func TestSoftwareReport_OnlineNode(t *testing.T) {
	h, ts, hubKS, leafKS := softwareHub(t)
	subscribeWithSoftware(t, ts, leafKS, &protocol.SoftwareReport{Name: protocol.SoftwareNameVision3, Version: "0.9.8"}, false)
	h.online.add(leafKS.NodeID(), "testnet")
	if n := listedNode(t, ts, hubKS, leafKS.NodeID()); !n.Online {
		t.Error("node with an open stream not listed online")
	}
	h.online.remove(leafKS.NodeID(), "testnet")
	if n := listedNode(t, ts, hubKS, leafKS.NodeID()); n.Online {
		t.Error("node listed online after its stream closed")
	}
}

// The report survives a hub restart: it is in the database, not only in
// memory.
func TestSoftwareStore_Persists(t *testing.T) {
	h, _ := setupTestHub(t)
	if _, err := h.subscribers.Add(Subscriber{NodeID: "n1", Network: "testnet", PubKeyB64: "k", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	if err := h.subscribers.SetSoftware("n1", "testnet", &protocol.SoftwareReport{Name: "ViSiON/3", Version: "0.9.8"}, at); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSubscriberStore(h.subscribers.db)
	if err != nil {
		t.Fatal(err)
	}
	subs, err := reopened.List("testnet")
	if err != nil || len(subs) != 1 {
		t.Fatalf("List = %v, %v", subs, err)
	}
	if subs[0].Software.String() != "ViSiON/3 0.9.8" || subs[0].SoftwareReportedAt != "2026-11-10T12:00:00Z" {
		t.Errorf("reopened row: %+v at %q", subs[0].Software, subs[0].SoftwareReportedAt)
	}
	if err := h.subscribers.SetSoftware("nobody", "testnet", nil, at); err == nil {
		t.Error("SetSoftware on an unknown node succeeded")
	}
}
