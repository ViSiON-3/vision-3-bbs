package v3net

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/dedup"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/leaf"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/registry"
)

// newHubService builds a service that hosts "testnet" with one open area,
// gen.general, and serves its hub over httptest. Subscribers are approved on
// arrival; area proposals wait for the coordinator.
func newHubService(t *testing.T, port int) (*Service, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	reviewAreas := false
	svc, err := New(config.V3NetConfig{
		Enabled:      true,
		KeystorePath: filepath.Join(dir, "v3net.key"),
		DedupDBPath:  filepath.Join(dir, "dedup.sqlite"),
		Hub: config.V3NetHubConfig{
			Enabled:          true,
			Host:             "127.0.0.1",
			Port:             port,
			DataDir:          filepath.Join(dir, "hub"),
			AutoApprove:      true,
			AutoApproveAreas: &reviewAreas,
			Networks:         []config.V3NetHubNetwork{{Name: "testnet"}},
			InitialAreas:     []config.V3NetHubArea{{Tag: "gen.general", Name: "General"}},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ts := httptest.NewServer(svc.Hub().Mux())
	t.Cleanup(func() {
		ts.Close()
		_ = svc.Close()
	})
	return svc, ts
}

// remoteNode is another BBS on the network: its own identity and leaf client.
type remoteNode struct {
	l      *leaf.Leaf
	nodeID string
	pubKey string
	areas  []protocol.AreaSubscriptionStatus // hub's answer to the subscribe
}

// newRemoteNode registers a fresh node with the hub, asking for areaTags.
func newRemoteNode(t *testing.T, hubURL, bbsName string, onEvent func(protocol.Event), areaTags ...string) *remoteNode {
	t.Helper()
	dir := t.TempDir()
	ks, _, err := keystore.Load(filepath.Join(dir, "node.key"))
	if err != nil {
		t.Fatalf("load keystore: %v", err)
	}
	ix, err := dedup.Open(filepath.Join(dir, "dedup.sqlite"))
	if err != nil {
		t.Fatalf("open dedup: %v", err)
	}
	t.Cleanup(func() { _ = ix.Close() })

	body, _ := json.Marshal(protocol.SubscribeRequest{
		Network: "testnet", NodeID: ks.NodeID(), PubKeyB64: ks.PubKeyBase64(),
		BBSName: bbsName, BBSHost: bbsName + ".example.net", AreaTags: areaTags,
	})
	resp, err := http.Post(hubURL+"/v3net/v1/subscribe", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("subscribe %s: %v", bbsName, err)
	}
	defer resp.Body.Close()
	var sr protocol.SubscribeWithAreasResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("subscribe %s: status %d, decode error %v", bbsName, resp.StatusCode, err)
	}

	l := leaf.New(leaf.Config{
		HubURL: hubURL, Network: "testnet", Keystore: ks, DedupIndex: ix,
		JAMWriter: &mockWriter{}, OnEvent: onEvent,
	})
	t.Cleanup(l.Close)
	return &remoteNode{l: l, nodeID: ks.NodeID(), pubKey: ks.PubKeyBase64(), areas: sr.Areas}
}

func mustFetchNAL(t *testing.T, svc *Service) *protocol.NAL {
	t.Helper()
	n, err := svc.FetchNALForNetwork(context.Background(), "testnet")
	if err != nil {
		t.Fatalf("FetchNALForNetwork: %v", err)
	}
	return n
}

func TestService_Accessors(t *testing.T) {
	svc, ts := newHubService(t, 0)
	ks, _, err := keystore.Load(svc.cfg.KeystorePath)
	if err != nil {
		t.Fatalf("reload keystore: %v", err)
	}

	if svc.NodeID() != ks.NodeID() {
		t.Errorf("NodeID = %q, want the keystore's %q", svc.NodeID(), ks.NodeID())
	}
	if !svc.HubActive() {
		t.Error("HubActive = false for a hub-enabled service")
	}
	if svc.LeafCount() != 0 || len(svc.Leaves()) != 0 || svc.HubURLForNetwork("testnet") != "" {
		t.Error("a service with no leaves should report none")
	}
	if got := svc.RegistryURL(); got != registry.DefaultURL {
		t.Errorf("RegistryURL = %q, want the default", got)
	}
	if got := svc.ConfigPath(); got != svc.cfg.KeystorePath {
		t.Errorf("ConfigPath = %q, want %q", got, svc.cfg.KeystorePath)
	}

	// An unparseable poll interval falls back to the default rather than failing.
	err = svc.AddLeaf(config.V3NetLeafConfig{
		Network: "testnet", HubURL: ts.URL, Boards: []string{"gen.general"}, PollInterval: "soon",
	}, &mockWriter{}, nil)
	if err != nil {
		t.Fatalf("AddLeaf: %v", err)
	}
	if svc.LeafCount() != 1 || len(svc.Leaves()) != 1 {
		t.Errorf("LeafCount/Leaves = %d/%d, want 1/1", svc.LeafCount(), len(svc.Leaves()))
	}
	if got := svc.HubURLForNetwork("testnet"); got != ts.URL {
		t.Errorf("HubURLForNetwork = %q, want %q", got, ts.URL)
	}
	if got := svc.HubURLForNetwork("nonet"); got != "" {
		t.Errorf("HubURLForNetwork for unknown network = %q, want empty", got)
	}

	svc.cfg.RegistryURL = "https://registry.example.net/r.json"
	if got := svc.RegistryURL(); got != "https://registry.example.net/r.json" {
		t.Errorf("RegistryURL = %q, want the configured URL", got)
	}

	leafOnly, err := New(config.V3NetConfig{
		KeystorePath: filepath.Join(t.TempDir(), "k.key"),
		DedupDBPath:  filepath.Join(t.TempDir(), "d.sqlite"),
	})
	if err != nil {
		t.Fatalf("New leaf-only: %v", err)
	}
	defer leafOnly.Close()
	if leafOnly.HubActive() || leafOnly.Hub() != nil {
		t.Error("a service without a hub config should not report an active hub")
	}
}

func TestService_CoordinatorReviewsProposals(t *testing.T) {
	svc, ts := newHubService(t, 0)
	ctx := context.Background()
	if err := svc.AddLeaf(config.V3NetLeafConfig{Network: "testnet", HubURL: ts.URL}, &mockWriter{}, nil); err != nil {
		t.Fatalf("AddLeaf: %v", err)
	}

	// The seeded NAL is served, verified, and names this node as manager.
	n := mustFetchNAL(t, svc)
	if area := n.FindArea("gen.general"); area == nil || area.ManagerNodeID != svc.NodeID() {
		t.Fatalf("seeded NAL areas = %+v", n.Areas)
	}

	proposer := newRemoteNode(t, ts.URL, "proposer", nil)
	for _, tag := range []string{"gen.retro", "gen.junk"} {
		resp, err := proposer.l.ProposeArea(protocol.AreaProposalRequest{Tag: tag, Name: strings.ToUpper(tag)})
		if err != nil || resp.Status != "pending" {
			t.Fatalf("propose %s: %+v, %v; want pending", tag, resp, err)
		}
	}
	// The service can propose on its own behalf too.
	own, err := svc.ProposeArea("testnet", protocol.AreaProposalRequest{Tag: "gen.own", Name: "Own"})
	if err != nil || own.Status != "pending" {
		t.Fatalf("ProposeArea: %+v, %v; want pending", own, err)
	}

	proposals, err := svc.ListProposals(ctx, "testnet")
	if err != nil {
		t.Fatalf("ListProposals: %v", err)
	}
	ids := map[string]string{}
	for _, p := range proposals {
		ids[p.Tag] = p.ID
	}
	if len(proposals) != 3 || ids["gen.retro"] == "" || ids["gen.junk"] == "" || ids["gen.own"] != own.ProposalID {
		t.Fatalf("ListProposals = %+v", proposals)
	}
	if proposals[0].FromNode != proposer.nodeID || proposals[0].FromBBS != "proposer" {
		t.Errorf("first proposal came from %q (%q)", proposals[0].FromNode, proposals[0].FromBBS)
	}

	// Approve one with overrides, reject another.
	err = svc.ApproveProposal(ctx, "testnet", ids["gen.retro"], protocol.ProposalApproveRequest{
		AccessMode: protocol.AccessModeApproval, ManagerNodeID: svc.NodeID(),
	})
	if err != nil {
		t.Fatalf("ApproveProposal: %v", err)
	}
	if err := svc.RejectProposal(ctx, "testnet", ids["gen.junk"], protocol.ProposalRejectRequest{Reason: "off topic"}); err != nil {
		t.Fatalf("RejectProposal: %v", err)
	}
	if err := svc.ApproveProposal(ctx, "testnet", ids["gen.junk"], protocol.ProposalApproveRequest{}); err == nil {
		t.Error("approving a rejected proposal should fail")
	}

	n = mustFetchNAL(t, svc)
	retro := n.FindArea("gen.retro")
	if retro == nil || retro.Access.Mode != protocol.AccessModeApproval || retro.ManagerNodeID != svc.NodeID() {
		t.Errorf("approved area = %+v", retro)
	}
	if n.FindArea("gen.junk") != nil || n.FindArea("gen.own") != nil {
		t.Errorf("rejected or pending proposals reached the NAL: %+v", n.Areas)
	}
	if proposals, err = svc.ListProposals(ctx, "testnet"); err != nil || len(proposals) != 1 || proposals[0].Tag != "gen.own" {
		t.Errorf("remaining proposals = %+v, %v; want only gen.own", proposals, err)
	}
}

func TestService_ManagerReviewsAccessRequests(t *testing.T) {
	svc, ts := newHubService(t, 0)
	ctx := context.Background()
	if err := svc.AddLeaf(config.V3NetLeafConfig{Network: "testnet", HubURL: ts.URL}, &mockWriter{}, nil); err != nil {
		t.Fatalf("AddLeaf: %v", err)
	}

	// Make gen.general approval-only so a subscriber has to ask.
	n, err := svc.Hub().NALStore().Get("testnet")
	if err != nil || n == nil {
		t.Fatalf("read seeded NAL: %v", err)
	}
	n.FindArea("gen.general").Access.Mode = protocol.AccessModeApproval
	if err := nal.Sign(n, svc.ks); err != nil {
		t.Fatalf("sign NAL: %v", err)
	}
	if err := svc.Hub().NALStore().Put("testnet", n); err != nil {
		t.Fatalf("store NAL: %v", err)
	}

	requester := newRemoteNode(t, ts.URL, "requester", nil, "gen.general")
	if len(requester.areas) != 1 || requester.areas[0].Status != "pending" {
		t.Fatalf("subscribe to approval area = %+v, want pending", requester.areas)
	}

	reqs, err := svc.ListAccessRequests(ctx, "testnet", "gen.general")
	if err != nil {
		t.Fatalf("ListAccessRequests: %v", err)
	}
	if len(reqs) != 1 || reqs[0].NodeID != requester.nodeID || reqs[0].BBSName != "requester" {
		t.Fatalf("ListAccessRequests = %+v", reqs)
	}

	if err := svc.ApproveAccess(ctx, "testnet", "gen.general", []string{requester.nodeID}); err != nil {
		t.Fatalf("ApproveAccess: %v", err)
	}
	if reqs, err = svc.ListAccessRequests(ctx, "testnet", "gen.general"); err != nil || len(reqs) != 0 {
		t.Errorf("requests after approval = %+v, %v; want none", reqs, err)
	}
	access := mustFetchNAL(t, svc).FindArea("gen.general").Access
	if len(access.AllowList) != 1 || access.AllowList[0] != requester.nodeID {
		t.Errorf("allow list after approval = %v", access.AllowList)
	}

	if err := svc.DenyAccess(ctx, "testnet", "gen.general", []string{requester.nodeID}, "spam"); err != nil {
		t.Fatalf("DenyAccess: %v", err)
	}
	access = mustFetchNAL(t, svc).FindArea("gen.general").Access
	if len(access.AllowList) != 0 || len(access.DenyList) != 1 || access.DenyList[0] != requester.nodeID {
		t.Errorf("access after denial = %+v", access)
	}

	// Handing the area to another node ends this node's authority over it.
	if err := svc.SetAreaManager(ctx, "testnet", "gen.general", requester.nodeID); err != nil {
		t.Fatalf("SetAreaManager: %v", err)
	}
	area := mustFetchNAL(t, svc).FindArea("gen.general")
	if area.ManagerNodeID != requester.nodeID || area.ManagerPubKeyB64 != requester.pubKey {
		t.Errorf("manager after reassignment = %q", area.ManagerNodeID)
	}
	if _, err := svc.ListAccessRequests(ctx, "testnet", "gen.general"); err == nil {
		t.Error("the former manager should no longer be able to list access requests")
	}
}

func TestService_UnknownNetwork(t *testing.T) {
	svc, _ := newHubService(t, 0)
	ctx := context.Background()

	_, errPropose := svc.ProposeArea("nonet", protocol.AreaProposalRequest{Tag: "gen.x", Name: "X"})
	_, errList := svc.ListProposals(ctx, "nonet")
	_, errRequests := svc.ListAccessRequests(ctx, "nonet", "gen.general")
	_, errNAL := svc.FetchNALForNetwork(ctx, "nonet")
	errs := map[string]error{
		"ProposeArea":        errPropose,
		"ListProposals":      errList,
		"ApproveProposal":    svc.ApproveProposal(ctx, "nonet", "id", protocol.ProposalApproveRequest{}),
		"RejectProposal":     svc.RejectProposal(ctx, "nonet", "id", protocol.ProposalRejectRequest{}),
		"SetAreaManager":     svc.SetAreaManager(ctx, "nonet", "gen.general", "abc"),
		"ListAccessRequests": errRequests,
		"ApproveAccess":      svc.ApproveAccess(ctx, "nonet", "gen.general", []string{"abc"}),
		"DenyAccess":         svc.DenyAccess(ctx, "nonet", "gen.general", []string{"abc"}, ""),
		"FetchNALForNetwork": errNAL,
	}
	for name, err := range errs {
		if err == nil || !strings.Contains(err.Error(), `"nonet"`) {
			t.Errorf("%s on an unsubscribed network = %v, want an error naming it", name, err)
		}
	}

	// Sending to a network with no leaf is a silent no-op by contract.
	msg := BuildWireMessage("nonet", "gen.general", svc.NodeID(), "General", "Alice", "All", "Hi", "Body", "")
	if err := svc.SendMessage("nonet", msg); err != nil {
		t.Errorf("SendMessage to unsubscribed network = %v, want nil", err)
	}
	if seen, _ := svc.dedupIdx.Seen(msg.MsgUUID); seen {
		t.Error("a message that was never sent must not be marked seen")
	}
}

func TestService_StartRunsHubAndLeaf(t *testing.T) {
	// Reserve a port for the hub's own listener.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	svc, ts := newHubService(t, port)
	svc.BBSName = "Service BBS"
	svc.BBSHost = "svc.example.net"
	nals := make(chan string, 8)
	svc.SetNALObserver(func(network string, n *protocol.NAL) {
		if n.FindArea("gen.general") != nil {
			nals <- network
		}
	})
	if err := svc.AddLeaf(config.V3NetLeafConfig{
		Network: "testnet", HubURL: ts.URL, Boards: []string{"gen.general"},
	}, &mockWriter{}, nil); err != nil {
		t.Fatalf("AddLeaf: %v", err)
	}

	// A second node watches the network's event stream.
	events := make(chan protocol.Event, 256)
	observer := newRemoteNode(t, ts.URL, "observer", func(ev protocol.Event) {
		select {
		case events <- ev:
		default:
		}
	})
	sseCtx, stopSSE := context.WithCancel(context.Background())
	sseDone := make(chan struct{})
	go func() {
		defer close(sseDone)
		observer.l.RunSSE(sseCtx)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.Start(ctx)
	}()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		stopSSE()
		<-sseDone
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("Start did not return after context cancel")
		}
	}
	defer stop()

	// The leaf has subscribed once its first NAL reaches the observer.
	select {
	case network := <-nals:
		if network != "testnet" {
			t.Errorf("NAL observer called for %q, want testnet", network)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("NAL observer was not called after Start")
	}

	// Start also brings up the hub's own listener.
	hubURL := fmt.Sprintf("http://127.0.0.1:%d/v3net/v1/networks", port)
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(hubURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("hub listener answered %d", resp.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("hub never listened on port %d: %v", port, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// An outbound message reaches the hub and is marked seen locally, so the
	// leaf will not import its own post on the next poll.
	msg := BuildWireMessage("testnet", "gen.general", svc.NodeID(), "General", "Alice", "All", "Hi", "Body", "")
	if err := svc.SendMessage("testnet", msg); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if seen, err := svc.dedupIdx.Seen(msg.MsgUUID); err != nil || !seen {
		t.Errorf("outbound message Seen = %v, %v; want true", seen, err)
	}
	resp, err := http.Get(ts.URL + "/v3net/v1/testnet/info")
	if err != nil {
		t.Fatalf("GET info: %v", err)
	}
	var info protocol.NetworkInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("decode info: %v", err)
	}
	resp.Body.Close()
	if info.MessageCount != 1 {
		t.Errorf("hub message count = %d, want 1", info.MessageCount)
	}

	// A message the hub refuses is reported and not marked seen.
	bad := BuildWireMessage("testnet", "gen.missing", svc.NodeID(), "General", "Alice", "All", "Hi", "Body", "")
	if err := svc.SendMessage("testnet", bad); err == nil {
		t.Error("SendMessage to an area not in the NAL should fail")
	}
	if seen, _ := svc.dedupIdx.Seen(bad.MsgUUID); seen {
		t.Error("a refused message must not be marked seen")
	}

	// Presence is sent in the background; resend until the observer's stream,
	// which connects asynchronously, delivers it.
	awaitPresence := func(eventType string, send func(string)) {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for {
			send("alice")
			retry := time.After(100 * time.Millisecond)
			for waiting := true; waiting; {
				select {
				case ev := <-events:
					var p protocol.LogonPayload
					if ev.Type == eventType && json.Unmarshal(ev.Data, &p) == nil && p.Handle == "alice" {
						return
					}
				case <-retry:
					waiting = false
				case <-deadline:
					t.Fatalf("observer never saw a %s event for alice", eventType)
				}
			}
		}
	}
	awaitPresence(protocol.EventLogon, svc.SendLogon)
	awaitPresence(protocol.EventLogoff, svc.SendLogoff)

	stop()
}

func TestNew_Errors(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	under := filepath.Join(blocker, "child") // a path beneath a regular file

	cases := []struct {
		name string
		cfg  config.V3NetConfig
		want string
	}{
		{"keystore unwritable", config.V3NetConfig{
			KeystorePath: filepath.Join(under, "v3net.key"),
			DedupDBPath:  filepath.Join(dir, "a.sqlite"),
		}, "load keystore"},
		{"dedup index unopenable", config.V3NetConfig{
			KeystorePath: filepath.Join(dir, "b.key"),
			DedupDBPath:  filepath.Join(under, "dedup.sqlite"),
		}, "open dedup index"},
		{"hub data dir uncreatable", config.V3NetConfig{
			KeystorePath: filepath.Join(dir, "c.key"),
			DedupDBPath:  filepath.Join(dir, "c.sqlite"),
			Hub:          config.V3NetHubConfig{Enabled: true, DataDir: under},
		}, "create hub data dir"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, err := New(tc.cfg)
			if err == nil {
				svc.Close()
				t.Fatal("expected New to fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestHubAutoInit_MergesNewInitialAreas(t *testing.T) {
	dir := t.TempDir()
	cfg := config.V3NetConfig{
		KeystorePath: filepath.Join(dir, "v3net.key"),
		DedupDBPath:  filepath.Join(dir, "dedup.sqlite"),
		Hub: config.V3NetHubConfig{
			Enabled:      true,
			DataDir:      filepath.Join(dir, "hub"),
			Networks:     []config.V3NetHubNetwork{{Name: "testnet"}},
			InitialAreas: []config.V3NetHubArea{{Tag: "gen.general", Name: "General"}},
		},
	}
	first, err := New(cfg)
	if err != nil {
		t.Fatalf("first New: %v", err)
	}
	first.Close()

	// A later start lists one old and one new area: only the new one is added.
	cfg.Hub.InitialAreas = append(cfg.Hub.InitialAreas,
		config.V3NetHubArea{Tag: "gen.retro", Name: "Retro", Description: "Old iron"})
	second, err := New(cfg)
	if err != nil {
		t.Fatalf("second New: %v", err)
	}
	defer second.Close()

	n, err := second.Hub().NALStore().Get("testnet")
	if err != nil || n == nil {
		t.Fatalf("get NAL: %v, %v", n, err)
	}
	if len(n.Areas) != 2 || n.Areas[0].Tag != "gen.general" || n.Areas[1].Tag != "gen.retro" {
		t.Fatalf("merged areas = %+v", n.Areas)
	}
	retro := n.Areas[1]
	if retro.Description != "Old iron" || retro.ManagerNodeID != second.NodeID() ||
		retro.Access.Mode != protocol.AccessModeOpen || !retro.Policy.AllowANSI {
		t.Errorf("merged area = %+v", retro)
	}
	// The merged NAL is re-signed, so leaves will still accept it.
	if err := nal.Verify(n); err != nil {
		t.Errorf("merged NAL does not verify: %v", err)
	}
}

// newAreaFixture returns a message manager holding the given areas.
func newAreaFixture(t *testing.T, areas ...*message.MessageArea) (*message.MessageManager, string) {
	t.Helper()
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if len(areas) > 0 {
		data, _ := json.MarshalIndent(areas, "", "  ")
		if err := os.WriteFile(filepath.Join(configDir, "message_areas.json"), data, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	mgr, err := message.NewMessageManager(tmpDir, configDir, "TestBBS", nil)
	if err != nil {
		t.Fatalf("NewMessageManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr, configDir
}

func TestJAMAdapter_WriteMessage(t *testing.T) {
	mgr, _ := newAreaFixture(t, &message.MessageArea{
		ID: 1, Position: 1, Tag: "GENERAL", Name: "General", BasePath: "msgbases/general", AreaType: "v3net",
	})
	adapter := NewJAMAdapter(mgr, 1)

	msg := protocol.Message{
		MsgUUID: "550e8400-e29b-41d4-a716-446655440000", OriginNode: "abc123",
		From: "Alice", To: "All", Subject: "Greetings", DateUTC: "2026-03-16T04:20:00Z",
		Body: "Hello from afar", Tearline: "--- ViSiON/3 test", Origin: "Far BBS",
	}
	num, err := adapter.WriteMessage(msg)
	if err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if num != 1 {
		t.Errorf("first message number = %d, want 1", num)
	}

	stored, err := mgr.GetMessage(1, 1)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if stored.From != "Alice" || stored.To != "All" || stored.Subject != "Greetings" {
		t.Errorf("stored header = %q -> %q: %q", stored.From, stored.To, stored.Subject)
	}
	// The authored date is kept, not the time of import.
	if want := time.Date(2026, 3, 16, 4, 20, 0, 0, time.UTC); !stored.DateTime.Equal(want) {
		t.Errorf("stored date = %v, want %v", stored.DateTime, want)
	}
	for _, want := range []string{"Hello from afar", "--- ViSiON/3 test", " * Origin: Far BBS (abc123)"} {
		if !strings.Contains(stored.Body, want) {
			t.Errorf("stored body %q is missing %q", stored.Body, want)
		}
	}

	if num, err = adapter.WriteMessage(msg); err != nil || num != 2 {
		t.Errorf("second WriteMessage = %d, %v; want 2", num, err)
	}

	// Failures are reported, and nothing more is written.
	msg.DateUTC = "yesterday"
	if _, err := adapter.WriteMessage(msg); err == nil || !strings.Contains(err.Error(), "parse date") {
		t.Errorf("WriteMessage with bad date = %v, want a date error", err)
	}
	msg.DateUTC = "2026-03-16T04:20:00Z"
	if _, err := NewJAMAdapter(mgr, 99).WriteMessage(msg); err == nil || !strings.Contains(err.Error(), "write message") {
		t.Errorf("WriteMessage to a missing area = %v, want a write error", err)
	}
	if count, err := mgr.GetMessageCountForArea(1); err != nil || count != 2 {
		t.Errorf("message count = %d, %v; want 2", count, err)
	}
}

func TestAreaNameFromTag(t *testing.T) {
	cases := []struct{ tag, network, want string }{
		{"fel.general", "felonynet", "Felonynet General"},
		{"fel.retro-computing", "felonynet", "Felonynet Retro Computing"},
		{"fel.general", "", "General"},
		{"fel.a--b", "net", "Net A  B"},
		{"FELGEN", "felonynet", "FELGEN"}, // no prefix.name split: used as is
	}
	for _, tc := range cases {
		if got := areaNameFromTag(tc.tag, tc.network); got != tc.want {
			t.Errorf("areaNameFromTag(%q, %q) = %q, want %q", tc.tag, tc.network, got, tc.want)
		}
	}
}

func TestSyncAreas_InfersConference(t *testing.T) {
	mgr, configDir := newAreaFixture(t, &message.MessageArea{
		ID: 1, Position: 1, Tag: "fel.general", Name: "Felony General", BasePath: "msgbases/felgen",
		AreaType: "v3net", Network: "felonynet", ConferenceID: 7,
	})
	confs := `[{"id":3,"position":1,"tag":"RETRONET","name":"RetroNet"}]`
	if err := os.WriteFile(filepath.Join(configDir, "conferences.json"), []byte(confs), 0644); err != nil {
		t.Fatalf("write conferences: %v", err)
	}
	confMgr, err := conference.NewConferenceManager(configDir)
	if err != nil {
		t.Fatalf("NewConferenceManager: %v", err)
	}

	created := SyncAreas([]config.V3NetLeafConfig{
		{Network: "felonynet", Boards: []string{"fel.tech", ""}}, // blank boards are ignored
		{Network: "retronet", Boards: []string{"ret.general"}},
		{Network: "othernet", Boards: []string{"oth.general"}},
	}, mgr, confMgr)
	if created != 3 {
		t.Fatalf("SyncAreas created %d areas, want 3", created)
	}

	want := map[string]struct {
		conf int
		name string
	}{
		"fel.tech":    {7, "Felonynet Tech"},   // joins its network's existing areas
		"ret.general": {3, "Retronet General"}, // matched by conference tag
		"oth.general": {0, "Othernet General"}, // nothing to match
	}
	for tag, w := range want {
		area, ok := mgr.GetAreaByTag(tag)
		if !ok {
			t.Errorf("%s was not created", tag)
			continue
		}
		if area.ConferenceID != w.conf || area.Name != w.name {
			t.Errorf("%s: conference %d, name %q; want %d, %q", tag, area.ConferenceID, area.Name, w.conf, w.name)
		}
	}
}
