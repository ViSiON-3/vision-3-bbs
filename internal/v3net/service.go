// Package v3net provides the top-level V3Net service that wires together the
// keystore, dedup index, hub server, and leaf clients for Vision/3 integration.
package v3net

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/dedup"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/hub"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/leaf"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/registry"
)

// Service manages V3Net hub and leaf lifecycle.
//
// Leaf subscriptions and area bindings were historically populated before
// Start() and read-only afterwards; ReloadLeaves changed that, so mu now
// guards them. The hub, keystore, and dedup index remain fixed for the life
// of the process — reconfiguring those still requires a restart.
type Service struct {
	cfg      config.V3NetConfig
	ks       *keystore.Keystore
	dedupIdx *dedup.Index
	hub      *hub.Hub

	// mu guards leaves, leafByNetwork, areaBindings, and runCtx.
	mu     sync.RWMutex
	leaves []*leafRun

	// leafByNetwork maps network name to its running leaf for message sending.
	leafByNetwork map[string]*leafRun

	// areaBindings maps message area ID to its V3Net network and origin line.
	areaBindings map[int]AreaBinding

	// runCtx is the context Start was called with; nil until then. A leaf
	// added while the service runs is started against a child of it.
	runCtx context.Context

	// reloadMu serializes ReloadLeaves calls; it is never held together with mu.
	reloadMu sync.Mutex

	// BBSName and BBSHost are sent in subscribe requests.
	BBSName string
	BBSHost string
}

// AreaBinding ties a message area to the V3Net network it is subscribed on
// and the origin line its outbound messages carry.
type AreaBinding struct {
	Network string
	Origin  string
}

// leafRun is a leaf plus its runtime handles. cancel and done are nil until
// the leaf is started; stopping cancels the leaf's own context and waits for
// its goroutine, so a stop never strands a mid-backoff subscribe or an open
// SSE stream.
type leafRun struct {
	l      *leaf.Leaf
	cancel context.CancelFunc
	done   chan struct{}
	// stop makes stopping idempotent and concurrency-safe: shutdown, Close,
	// and a reload can all try to stop the same run, and sync.Once blocks
	// the latecomers until the first stop has fully completed.
	stop sync.Once
}

// hubAutoInit performs idempotent hub initialization steps:
// 1. Self-registers the hub node as an active subscriber for each network.
// 2. Seeds the initial NAL from cfg.Hub.InitialAreas if none exists.
//
// Note: The hub data directory is created by New() before calling hub.New(),
// which is the correct location for SQLite database initialization.
func hubAutoInit(cfg config.V3NetConfig, h *hub.Hub, ks *keystore.Keystore) {
	// Step 1: self-register the hub node for each network.
	for _, n := range cfg.Hub.Networks {
		sub := hub.Subscriber{
			NodeID:    ks.NodeID(),
			Network:   n.Name,
			PubKeyB64: ks.PubKeyBase64(),
			BBSName:   "hub",
			BBSHost:   "",
			Status:    "active",
		}
		if _, err := h.Subscribers().Add(sub); err != nil {
			slog.Warn("v3net: hub self-registration failed", "network", n.Name, "error", err)
		} else {
			slog.Info("v3net: hub self-registered", "node_id", ks.NodeID(), "network", n.Name)
		}
	}

	// Step 2: merge InitialAreas into the NAL. If no NAL exists, create one.
	// If a NAL already exists, only add areas not already present — this lets
	// new areas be added to initialAreas after the hub has been running.
	if len(cfg.Hub.InitialAreas) == 0 {
		return
	}
	for _, n := range cfg.Hub.Networks {
		existing, err := h.NALStore().Get(n.Name)
		if err != nil {
			slog.Warn("v3net: could not check NAL for seeding", "network", n.Name, "error", err)
			continue
		}

		nalDoc := existing
		if nalDoc == nil {
			nalDoc = &protocol.NAL{
				V3NetNAL: "1.0",
				Network:  n.Name,
			}
		}

		added := 0
		for _, a := range cfg.Hub.InitialAreas {
			if nalDoc.FindArea(a.Tag) != nil {
				continue // already in NAL
			}
			nalDoc.Areas = append(nalDoc.Areas, protocol.Area{
				Tag:              a.Tag,
				Name:             a.Name,
				Description:      a.Description,
				Language:         "en",
				ManagerNodeID:    ks.NodeID(),
				ManagerPubKeyB64: ks.PubKeyBase64(),
				Access:           protocol.AreaAccess{Mode: protocol.AccessModeOpen},
				Policy: protocol.AreaPolicy{
					MaxBodyBytes: 64000,
					AllowANSI:    true,
				},
			})
			added++
		}

		if added == 0 {
			continue // nothing new to add
		}

		if err := nal.Sign(nalDoc, ks); err != nil {
			slog.Error("v3net: could not sign initial NAL", "network", n.Name, "error", err)
			continue
		}
		if err := h.NALStore().Put(n.Name, nalDoc); err != nil {
			slog.Error("v3net: could not store initial NAL", "network", n.Name, "error", err)
			continue
		}
		slog.Info("v3net: merged initial areas into NAL", "network", n.Name, "added", added, "total", len(nalDoc.Areas))
	}

	// Clear initialAreas from the saved config file so we don't re-seed.
	updatedCfg := cfg
	updatedCfg.Hub.InitialAreas = nil
	if cfg.ConfigPath != "" {
		if err := config.SaveV3NetConfig(cfg.ConfigPath, updatedCfg); err != nil {
			slog.Warn("v3net: could not remove initialAreas from config after seeding", "error", err)
		}
	}
}

// New creates a V3Net service from the given config. Call Start to begin operations.
func New(cfg config.V3NetConfig) (*Service, error) {
	ks, created, err := keystore.Load(cfg.KeystorePath)
	if err != nil {
		return nil, fmt.Errorf("v3net: load keystore: %w", err)
	}
	slog.Info("v3net: node identity", "node_id", ks.NodeID())

	if created {
		slog.Warn("v3net: NEW IDENTITY CREATED — back up your recovery seed phrase",
			"node_id", ks.NodeID(),
			"action", "Run ./config > V3Net > Node Identity to view and export your seed phrase",
		)
	}

	ix, err := dedup.Open(cfg.DedupDBPath)
	if err != nil {
		return nil, fmt.Errorf("v3net: open dedup index: %w", err)
	}

	s := &Service{
		cfg:           cfg,
		ks:            ks,
		dedupIdx:      ix,
		leafByNetwork: make(map[string]*leafRun),
		areaBindings:  make(map[int]AreaBinding),
	}

	// Initialize hub if enabled.
	if cfg.Hub.Enabled {
		var networks []hub.NetworkConfig
		for _, n := range cfg.Hub.Networks {
			networks = append(networks, hub.NetworkConfig{
				Name:        n.Name,
				Description: n.Description,
			})
		}
		// Create data dir before hub.New opens the SQLite database.
		if err := os.MkdirAll(cfg.Hub.DataDir, 0755); err != nil {
			_ = ix.Close() // cleanup on error path
			return nil, fmt.Errorf("v3net: create hub data dir: %w", err)
		}
		h, err := hub.New(hub.Config{
			ListenAddr:  cfg.Hub.ListenAddr(),
			DataDir:     cfg.Hub.DataDir,
			Keystore:    ks,
			AutoApprove: cfg.Hub.AutoApprove,
			Networks:    networks,
		})
		if err != nil {
			_ = ix.Close() // cleanup on error path
			return nil, fmt.Errorf("v3net: create hub: %w", err)
		}
		hubAutoInit(cfg, h, ks)
		s.hub = h
	}

	return s, nil
}

// JAMWriter is the interface for writing V3Net messages to the local JAM base.
// This must be set before calling Start via SetJAMWriter.
type JAMWriter = leaf.JAMWriter

// AddLeaf configures a leaf client for a network subscription.
func (s *Service) AddLeaf(lcfg config.V3NetLeafConfig, writer JAMWriter, onEvent func(protocol.Event)) error {
	interval, err := time.ParseDuration(lcfg.PollInterval)
	if err != nil || interval <= 0 {
		interval = leaf.DefaultPollInterval
	}

	l := leaf.New(leaf.Config{
		HubURL:       lcfg.HubURL,
		Network:      lcfg.Network,
		AreaTags:     lcfg.Boards,
		PollInterval: interval,
		Keystore:     s.ks,
		DedupIndex:   s.dedupIdx,
		JAMWriter:    writer,
		OnEvent:      onEvent,
		BBSName:      s.BBSName,
		BBSHost:      s.BBSHost,
	})

	run := &leafRun{l: l}
	s.mu.Lock()
	s.leaves = append(s.leaves, run)
	s.leafByNetwork[lcfg.Network] = run
	if s.runCtx != nil {
		// The service is already running (a reload added this leaf); start it
		// now rather than waiting for a Start that already happened.
		s.startLeafLocked(run)
	}
	s.mu.Unlock()
	return nil
}

// startLeafLocked launches a leaf against a child of the service context.
// Caller must hold s.mu, and s.runCtx must be set.
func (s *Service) startLeafLocked(run *leafRun) {
	lctx, cancel := context.WithCancel(s.runCtx)
	run.cancel = cancel
	run.done = make(chan struct{})
	go func() {
		defer close(run.done)
		run.l.Start(lctx)
	}()
}

// stopLeaf cancels a running leaf, waits for its goroutine to exit, and
// closes its idle connections. Safe on a leaf that was never started. Must
// be called without holding s.mu — the wait can take as long as the leaf's
// current HTTP request.
func (s *Service) stopLeaf(run *leafRun) {
	run.stop.Do(func() {
		if run.cancel != nil {
			run.cancel()
			<-run.done
		}
		run.l.Close()
	})
}

// Start launches the hub (if enabled) and all leaf clients, then blocks
// until ctx is cancelled, at which point every running leaf is stopped and
// waited for.
func (s *Service) Start(ctx context.Context) {
	hubDone := make(chan struct{})
	if s.hub != nil {
		go func() {
			defer close(hubDone)
			if err := s.hub.Start(ctx); err != nil {
				slog.Error("v3net: hub error", "error", err)
			}
		}()
	} else {
		close(hubDone)
	}

	s.mu.Lock()
	s.runCtx = ctx
	for _, run := range s.leaves {
		if run.done == nil {
			s.startLeafLocked(run)
		}
	}
	s.mu.Unlock()

	<-ctx.Done()

	s.mu.Lock()
	running := append([]*leafRun(nil), s.leaves...)
	s.mu.Unlock()
	for _, run := range running {
		s.stopLeaf(run)
	}
	<-hubDone
}

// Close releases resources held by the service.
func (s *Service) Close() error {
	s.mu.Lock()
	running := append([]*leafRun(nil), s.leaves...)
	s.mu.Unlock()
	for _, run := range running {
		s.stopLeaf(run)
	}
	if s.hub != nil {
		_ = s.hub.Close() // best-effort shutdown
	}
	return s.dedupIdx.Close()
}

// NodeID returns the local node's 16-char hex identifier.
func (s *Service) NodeID() string {
	return s.ks.NodeID()
}

// SendMessage sends a message to the hub for the given network.
// Returns nil if no leaf is configured for that network.
// Also marks the message as seen in the dedup index so the local leaf
// does not re-import it when polling.
func (s *Service) SendMessage(network string, msg protocol.Message) error {
	s.mu.RLock()
	run, ok := s.leafByNetwork[network]
	s.mu.RUnlock()
	if !ok {
		return nil
	}
	if err := run.l.SendMessage(msg); err != nil {
		return err
	}
	// Mark as seen so our own leaf won't write it back to JAM.
	if err := s.dedupIdx.MarkSeen(msg.MsgUUID, network, nil); err != nil {
		slog.Warn("v3net: failed to mark outbound message as seen", "uuid", msg.MsgUUID, "error", err)
	}
	return nil
}

// SendLogon notifies all connected hubs of a user logon.
// Runs asynchronously so it never blocks the caller's session.
func (s *Service) SendLogon(handle string) {
	for _, lf := range s.Leaves() {
		go func(lf *leaf.Leaf) {
			if err := lf.SendLogon(handle); err != nil {
				slog.Warn("v3net: SendLogon failed", "error", err)
			}
		}(lf)
	}
}

// SendLogoff notifies all connected hubs of a user logoff.
// Runs asynchronously so it never blocks the caller's session.
func (s *Service) SendLogoff(handle string) {
	for _, lf := range s.Leaves() {
		go func(lf *leaf.Leaf) {
			if err := lf.SendLogoff(handle); err != nil {
				slog.Warn("v3net: SendLogoff failed", "error", err)
			}
		}(lf)
	}
}

// HubActive returns true if the hub is running.
func (s *Service) HubActive() bool {
	return s.hub != nil
}

// LeafCount returns the number of configured leaf subscriptions.
func (s *Service) LeafCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.leaves)
}

// LeafNetworks returns the names of all subscribed networks.
func (s *Service) LeafNetworks() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var names []string
	for name := range s.leafByNetwork {
		names = append(names, name)
	}
	return names
}

// Hub returns the hub instance, or nil if no hub is configured.
func (s *Service) Hub() *hub.Hub {
	return s.hub
}

// Leaves returns a snapshot of the current leaf clients. The slice is the
// caller's to keep; a reload does not mutate it.
func (s *Service) Leaves() []*leaf.Leaf {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*leaf.Leaf, 0, len(s.leaves))
	for _, run := range s.leaves {
		out = append(out, run.l)
	}
	return out
}

// RegisterArea associates a message area ID with a V3Net network name and
// the origin line its outbound messages carry.
func (s *Service) RegisterArea(areaID int, network, origin string) {
	slog.Info("v3net: registering area", "area_id", areaID, "network", network)
	s.mu.Lock()
	s.areaBindings[areaID] = AreaBinding{Network: network, Origin: origin}
	s.mu.Unlock()
}

// NetworkForArea returns the V3Net network name for a message area, or empty
// string if the area is not a V3Net-networked area.
func (s *Service) NetworkForArea(areaID int) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.areaBindings[areaID].Network
}

// AreaBindingFor returns the full binding for a message area, and whether
// the area is V3Net-networked at all.
func (s *Service) AreaBindingFor(areaID int) (AreaBinding, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.areaBindings[areaID]
	return b, ok
}

// ProposeArea submits an area proposal to the hub for the given network.
func (s *Service) ProposeArea(network string, req protocol.AreaProposalRequest) (*protocol.ProposalResponse, error) {
	s.mu.RLock()
	run, ok := s.leafByNetwork[network]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("v3net: no leaf configured for network %q", network)
	}
	return run.l.ProposeArea(req)
}

// FetchNALForNetwork fetches and verifies the NAL for the given network.
func (s *Service) FetchNALForNetwork(ctx context.Context, network string) (*protocol.NAL, error) {
	s.mu.RLock()
	run, ok := s.leafByNetwork[network]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("v3net: no leaf configured for network %q", network)
	}
	return run.l.FetchNAL(ctx)
}

// HubURLForNetwork returns the hub URL for the given network, or empty string.
func (s *Service) HubURLForNetwork(network string) string {
	s.mu.RLock()
	run, ok := s.leafByNetwork[network]
	s.mu.RUnlock()
	if !ok {
		return ""
	}
	return run.l.HubURL()
}

// RegistryURL returns the configured registry URL, or the default if not set.
func (s *Service) RegistryURL() string {
	if s.cfg.RegistryURL != "" {
		return s.cfg.RegistryURL
	}
	return registry.DefaultURL
}

// ConfigPath returns the path used to load the V3Net config (for saving).
func (s *Service) ConfigPath() string {
	return s.cfg.KeystorePath // parent dir derived at call site
}

// ConfigureLeaves builds and registers a leaf client for every subscription
// in leafCfgs: board tags are resolved against the message manager, a JAM
// router is wired per network, and each resolved area is bound to its
// network and origin. Unresolvable boards and networks with no resolvable
// boards are skipped with a warning, matching historical startup behavior.
// If the service is already running, new leaves start immediately.
func (s *Service) ConfigureLeaves(leafCfgs []config.V3NetLeafConfig, mgr *message.MessageManager, defaultOrigin string) {
	for _, lcfg := range leafCfgs {
		router := NewJAMRouter()
		origin := lcfg.Origin
		if origin == "" {
			origin = defaultOrigin
		}
		var resolvedBoards []string
		for _, tag := range lcfg.Boards {
			area, ok := mgr.GetAreaByTag(tag)
			if !ok {
				slog.Warn("V3Net leaf: message area not found, skipping", "network", lcfg.Network, "area", tag)
				continue
			}
			router.Add(tag, NewJAMAdapter(mgr, area.ID))
			resolvedBoards = append(resolvedBoards, tag)
			s.RegisterArea(area.ID, lcfg.Network, origin)
		}
		if len(resolvedBoards) == 0 {
			slog.Warn("V3Net leaf: no resolvable boards, skipping", "network", lcfg.Network)
			continue
		}
		leafCfg := lcfg
		leafCfg.Boards = resolvedBoards
		if err := s.AddLeaf(leafCfg, router, nil); err != nil {
			slog.Error("V3Net leaf error", "network", lcfg.Network, "error", err)
		}
	}
}

// ReloadLeaves replaces the running leaf subscriptions with those in
// leafCfgs: message areas for new subscriptions are auto-created (as at
// startup), every current leaf is stopped and waited for, the area bindings
// are cleared, and the new set is configured and started.
//
// This is a full stop-and-rebuild rather than a per-network diff — a
// subscription change is a rare, sysop-initiated event, and rebuilding is
// the simplest shape that cannot leave a half-old, half-new set behind. The
// visible costs: unchanged networks re-subscribe to their hub (idempotent),
// and a caller mid-chat on a V3Net leaf has that chat's stream cancelled.
//
// Hub settings, the enabled flag, and keystore/dedup paths are NOT touched;
// those still require a restart.
func (s *Service) ReloadLeaves(leafCfgs []config.V3NetLeafConfig, mgr *message.MessageManager, confMgr *conference.ConferenceManager, defaultOrigin string) error {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()

	if created := SyncAreas(leafCfgs, mgr, confMgr); created > 0 {
		slog.Info("v3net: created message areas for new subscriptions", "count", created)
	}

	// Detach the current set under the lock, then stop it outside the lock —
	// waiting for a leaf mid-request while holding mu would stall SendMessage
	// and the status providers.
	s.mu.Lock()
	old := s.leaves
	s.leaves = nil
	s.leafByNetwork = make(map[string]*leafRun)
	s.areaBindings = make(map[int]AreaBinding)
	s.mu.Unlock()

	for _, run := range old {
		s.stopLeaf(run)
	}
	slog.Info("v3net: leaf subscriptions stopped for reload", "count", len(old))

	s.ConfigureLeaves(leafCfgs, mgr, defaultOrigin)

	s.mu.RLock()
	count := len(s.leaves)
	s.mu.RUnlock()
	slog.Info("v3net: leaf subscriptions reloaded", "count", count)
	return nil
}
