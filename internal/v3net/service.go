// Package v3net provides the top-level V3Net service that wires together the
// keystore, dedup index, hub server, and leaf clients for Vision/3 integration.
package v3net

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/dedup"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/hub"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/leaf"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// Service manages V3Net hub and leaf lifecycle.
type Service struct {
	cfg      config.V3NetConfig
	ks       *keystore.Keystore
	dedupIdx *dedup.Index
	hub      *hub.Hub
	leaves   []*leaf.Leaf

	// leafByNetwork maps network name to its leaf for message sending.
	leafByNetwork map[string]*leaf.Leaf

	// areaToNetwork maps message area ID to its V3Net network name.
	areaToNetwork map[int]string

	// BBSName and BBSHost are sent in subscribe requests.
	BBSName string
	BBSHost string
}

// New creates a V3Net service from the given config. Call Start to begin operations.
func New(cfg config.V3NetConfig) (*Service, error) {
	ks, err := keystore.Load(cfg.KeystorePath)
	if err != nil {
		return nil, fmt.Errorf("v3net: load keystore: %w", err)
	}
	slog.Info("v3net: node identity", "node_id", ks.NodeID())

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

// ConfigPath returns the path used to load the V3Net config (for saving).
func (s *Service) ConfigPath() string {
	return s.cfg.KeystorePath // parent dir derived at call site
}
