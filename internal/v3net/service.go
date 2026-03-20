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

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
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
// leaves, leafByNetwork, and areaToNetwork are populated during initialization
// (via AddLeaf/RegisterArea) before Start() is called, and are read-only after
// that point. No mutex is needed for concurrent read access.
type Service struct {
	cfg      config.V3NetConfig
	ks       *keystore.Keystore
	dedupIdx *dedup.Index
	hub      *hub.Hub
	leaves   []*leaf.Leaf

	// leafByNetwork maps network name to its leaf for message sending.
	// Populated at init time only; read-only after Start().
	leafByNetwork map[string]*leaf.Leaf

	// areaToNetwork maps message area ID to its V3Net network name.
	// Populated at init time only; read-only after Start().
	areaToNetwork map[int]string

	// BBSName and BBSHost are sent in subscribe requests.
	BBSName string
	BBSHost string
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

	// Step 2: seed NAL from InitialAreas if no NAL exists yet.
	if len(cfg.Hub.InitialAreas) == 0 {
		return
	}
	for _, n := range cfg.Hub.Networks {
		existing, err := h.NALStore().Get(n.Name)
		if err != nil {
			slog.Warn("v3net: could not check NAL for seeding", "network", n.Name, "error", err)
			continue
		}
		if existing != nil {
			continue // NAL already exists — skip seeding.
		}

		var areas []protocol.Area
		for _, a := range cfg.Hub.InitialAreas {
			areas = append(areas, protocol.Area{
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
		}

		nalDoc := &protocol.NAL{
			V3NetNAL: "1.0",
			Network:  n.Name,
			Areas:    areas,
		}
		if err := nal.Sign(nalDoc, ks); err != nil {
			slog.Error("v3net: could not sign initial NAL", "network", n.Name, "error", err)
			continue
		}
		if err := h.NALStore().Put(n.Name, nalDoc); err != nil {
			slog.Error("v3net: could not store initial NAL", "network", n.Name, "error", err)
			continue
		}
		slog.Info("v3net: seeded initial NAL", "network", n.Name, "areas", len(areas))
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
		leafByNetwork: make(map[string]*leaf.Leaf),
		areaToNetwork: make(map[int]string),
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
			ix.Close()
			return nil, fmt.Errorf("v3net: create hub data dir: %w", err)
		}
		h, err := hub.New(hub.Config{
			ListenAddr:  cfg.Hub.ListenAddr(),
			TLSCertFile: cfg.Hub.TLSCert,
			TLSKeyFile:  cfg.Hub.TLSKey,
			DataDir:     cfg.Hub.DataDir,
			Keystore:    ks,
			AutoApprove: cfg.Hub.AutoApprove,
			Networks:    networks,
		})
		if err != nil {
			ix.Close()
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

	s.leaves = append(s.leaves, l)
	s.leafByNetwork[lcfg.Network] = l
	return nil
}

// Start launches the hub (if enabled) and all leaf clients. Blocks until ctx is cancelled.
func (s *Service) Start(ctx context.Context) {
	var wg sync.WaitGroup

	if s.hub != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.hub.Start(ctx); err != nil {
				slog.Error("v3net: hub error", "error", err)
			}
		}()
	}

	for _, l := range s.leaves {
		wg.Add(1)
		go func(lf *leaf.Leaf) {
			defer wg.Done()
			lf.Start(ctx)
		}(l)
	}

	wg.Wait()
}

// Close releases resources held by the service.
func (s *Service) Close() error {
	for _, l := range s.leaves {
		l.Close()
	}
	if s.hub != nil {
		s.hub.Close()
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
	l, ok := s.leafByNetwork[network]
	if !ok {
		return nil
	}
	if err := l.SendMessage(msg); err != nil {
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
	for _, l := range s.leaves {
		go func(lf *leaf.Leaf) {
			if err := lf.SendLogon(handle); err != nil {
				slog.Warn("v3net: SendLogon failed", "error", err)
			}
		}(l)
	}
}

// SendLogoff notifies all connected hubs of a user logoff.
// Runs asynchronously so it never blocks the caller's session.
func (s *Service) SendLogoff(handle string) {
	for _, l := range s.leaves {
		go func(lf *leaf.Leaf) {
			if err := lf.SendLogoff(handle); err != nil {
				slog.Warn("v3net: SendLogoff failed", "error", err)
			}
		}(l)
	}
}

// HubActive returns true if the hub is running.
func (s *Service) HubActive() bool {
	return s.hub != nil
}

// LeafCount returns the number of configured leaf subscriptions.
func (s *Service) LeafCount() int {
	return len(s.leaves)
}

// LeafNetworks returns the names of all subscribed networks.
func (s *Service) LeafNetworks() []string {
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

// RegisterArea associates a message area ID with a V3Net network name.
// Called during startup so the message reader can identify V3Net areas.
func (s *Service) RegisterArea(areaID int, network string) {
	slog.Info("v3net: registering area", "area_id", areaID, "network", network)
	s.areaToNetwork[areaID] = network
}

// NetworkForArea returns the V3Net network name for a message area, or empty
// string if the area is not a V3Net-networked area.
func (s *Service) NetworkForArea(areaID int) string {
	return s.areaToNetwork[areaID]
}

// ProposeArea submits an area proposal to the hub for the given network.
func (s *Service) ProposeArea(network string, req protocol.AreaProposalRequest) (*protocol.ProposalResponse, error) {
	l, ok := s.leafByNetwork[network]
	if !ok {
		return nil, fmt.Errorf("v3net: no leaf configured for network %q", network)
	}
	return l.ProposeArea(req)
}

// FetchNALForNetwork fetches and verifies the NAL for the given network.
func (s *Service) FetchNALForNetwork(ctx context.Context, network string) (*protocol.NAL, error) {
	l, ok := s.leafByNetwork[network]
	if !ok {
		return nil, fmt.Errorf("v3net: no leaf configured for network %q", network)
	}
	return l.FetchNAL(ctx)
}

// HubURLForNetwork returns the hub URL for the given network, or empty string.
func (s *Service) HubURLForNetwork(network string) string {
	l, ok := s.leafByNetwork[network]
	if !ok {
		return ""
	}
	return l.HubURL()
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
