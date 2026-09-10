package v3net_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// newReloadFixture builds a leaf-only service plus a message manager holding
// the given area tags. Hub URLs point at an unroutable address, so leaves sit
// in their subscribe-retry loop — which is exactly the state a stop must be
// able to interrupt.
func newReloadFixture(t *testing.T, tags ...string) (*v3net.Service, *message.MessageManager) {
	t.Helper()
	dir := t.TempDir()
	keystorePath := filepath.Join(dir, "v3net.key")
	if _, _, err := keystore.Load(keystorePath); err != nil {
		t.Fatal(err)
	}
	svc, err := v3net.New(config.V3NetConfig{
		Enabled:      true,
		KeystorePath: keystorePath,
		DedupDBPath:  filepath.Join(dir, "dedup.sqlite"),
		ConfigPath:   dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })

	configDir := filepath.Join(dir, "configs")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	body := "["
	for i, tag := range tags {
		if i > 0 {
			body += ","
		}
		body += `{"id":` + strconv.Itoa(i+1) + `,"tag":"` + tag + `","name":"` + tag + `"}`
	}
	body += "]"
	if err := os.WriteFile(filepath.Join(configDir, "message_areas.json"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	mm, err := message.NewMessageManager(filepath.Join(dir, "data"), configDir, "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	return svc, mm
}

func leafCfg(network, board string) config.V3NetLeafConfig {
	return config.V3NetLeafConfig{
		Network: network,
		HubURL:  "http://127.0.0.1:1", // unroutable: leaves park in subscribe retry
		Boards:  []string{board},
	}
}

// TestReloadLeavesSwapsSubscriptions covers the whole contract: the old
// leaf set stops (even mid-subscribe-backoff), the new set is configured and
// running, and the area bindings follow.
func TestReloadLeavesSwapsSubscriptions(t *testing.T) {
	svc, mm := newReloadFixture(t, "ALPHA", "BETA")
	svc.ConfigureLeaves([]config.V3NetLeafConfig{leafCfg("net-a", "ALPHA")}, mm, "TestBBS")

	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	go func() {
		close(started)
		svc.Start(ctx)
	}()
	<-started
	time.Sleep(20 * time.Millisecond) // let the leaf enter its retry loop

	if got := svc.LeafNetworks(); len(got) != 1 || got[0] != "net-a" {
		t.Fatalf("LeafNetworks() = %v, want [net-a]", got)
	}
	alphaArea, _ := mm.GetAreaByTag("ALPHA")
	if got := svc.NetworkForArea(alphaArea.ID); got != "net-a" {
		t.Fatalf("NetworkForArea(ALPHA) = %q, want net-a", got)
	}

	done := make(chan error, 1)
	go func() {
		done <- svc.ReloadLeaves([]config.V3NetLeafConfig{leafCfg("net-b", "BETA")}, mm, nil, "TestBBS")
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ReloadLeaves: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ReloadLeaves hung — stopping a leaf mid-backoff did not interrupt it")
	}

	if got := svc.LeafNetworks(); len(got) != 1 || got[0] != "net-b" {
		t.Errorf("LeafNetworks() = %v after reload, want [net-b]", got)
	}
	if got := svc.NetworkForArea(alphaArea.ID); got != "" {
		t.Errorf("old binding survived reload: NetworkForArea(ALPHA) = %q", got)
	}
	betaArea, _ := mm.GetAreaByTag("BETA")
	if b, ok := svc.AreaBindingFor(betaArea.ID); !ok || b.Network != "net-b" || b.Origin != "TestBBS" {
		t.Errorf("AreaBindingFor(BETA) = %+v ok=%v, want net-b/TestBBS", b, ok)
	}

	// SendMessage to the removed network is a silent no-op, matching the
	// unconfigured-network contract.
	if err := svc.SendMessage("net-a", protocol.Message{}); err != nil {
		t.Errorf("SendMessage to removed network errored: %v", err)
	}

	cancel()
}

// TestReloadLeavesBeforeStart: a reload while the service has never started
// swaps configuration without starting anything.
func TestReloadLeavesBeforeStart(t *testing.T) {
	svc, mm := newReloadFixture(t, "ALPHA", "BETA")
	svc.ConfigureLeaves([]config.V3NetLeafConfig{leafCfg("net-a", "ALPHA")}, mm, "TestBBS")

	if err := svc.ReloadLeaves([]config.V3NetLeafConfig{leafCfg("net-b", "BETA")}, mm, nil, "TestBBS"); err != nil {
		t.Fatalf("ReloadLeaves before Start: %v", err)
	}
	if got := svc.LeafNetworks(); len(got) != 1 || got[0] != "net-b" {
		t.Errorf("LeafNetworks() = %v, want [net-b]", got)
	}
}

// TestReloadLeavesRaceWithReaders proves the swap is safe against the
// accessors real callers use, under -race.
func TestReloadLeavesRaceWithReaders(t *testing.T) {
	svc, mm := newReloadFixture(t, "ALPHA", "BETA")

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 60; i++ {
			cfgs := []config.V3NetLeafConfig{leafCfg("net-a", "ALPHA")}
			if i%2 == 1 {
				cfgs = []config.V3NetLeafConfig{leafCfg("net-b", "BETA")}
			}
			if err := svc.ReloadLeaves(cfgs, mm, nil, "TestBBS"); err != nil {
				t.Errorf("ReloadLeaves: %v", err)
				return
			}
		}
	}()
	for i := 0; ; i++ {
		select {
		case <-done:
			return
		default:
		}
		_ = svc.LeafNetworks()
		_ = svc.LeafCount()
		_ = svc.Leaves()
		_ = svc.NetworkForArea(1)
		_, _ = svc.AreaBindingFor(2)
		_ = svc.SendMessage("net-a", protocol.Message{})
		if i%64 == 0 {
			// SendLogon spawns a goroutine per leaf per call; unthrottled it
			// churns thousands of goroutines for no extra lock coverage.
			svc.SendLogon("caller")
		}
	}
}
