package ftn

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// buildBinkdRegen derives everything needed to regenerate a missing
// binkd.conf from configuration alone: identity from the server config,
// domains (zone parsed from each network's own address), address lines, and
// one node per link with a hostname. ok is false when no network has a
// parseable own address — then there is nothing meaningful to write.
func buildBinkdRegen(ftnCfg config.FTNConfig, server config.ServerConfig, bbsRoot string) (BinkdConfig, []BinkdNode, bool) {
	cfg := BinkdConfig{
		BBSRoot:      bbsRoot,
		BoardName:    server.BoardName,
		SysopName:    server.SysOpName,
		Location:     server.BBSLocation,
		Domains:      make(map[string]int),
		OutboundPath: ftnCfg.BinkdOutboundPath,
		// Per-network outbound overrides, so a regenerated conf keeps each
		// network's own queue rather than collapsing them onto the global one.
		NetworkOutbound: NetworkOutbounds(ftnCfg),
	}
	var nodes []BinkdNode

	// Sorted iteration so repeated regenerations produce identical files
	// (map order would otherwise shuffle address and node lines).
	netKeys := make([]string, 0, len(ftnCfg.Networks))
	for k := range ftnCfg.Networks {
		netKeys = append(netKeys, k)
	}
	sort.Strings(netKeys)

	for _, netKey := range netKeys {
		nc := ftnCfg.Networks[netKey]
		if nc.OwnAddress == "" {
			continue
		}
		addr, err := ParseAddress(nc.OwnAddress)
		if err != nil {
			continue
		}
		cfg.Domains[netKey] = addr.Zone
		cfg.Addresses = append(cfg.Addresses, fmt.Sprintf("%s@%s", nc.OwnAddress, netKey))
		for _, lnk := range nc.Links {
			if lnk.HostPort() == "" {
				continue
			}
			nodes = append(nodes, BinkdNode{
				Address:     fmt.Sprintf("%s@%s", lnk.Address, netKey),
				Hostname:    lnk.HostPort(),
				SessionPwd:  lnk.SessionPassword,
				NetworkName: netKey,
			})
		}
	}

	return cfg, nodes, len(cfg.Domains) > 0
}

// EnsureBinkdConf regenerates <bbsRoot>/data/ftn/binkd.conf from
// configuration when the file is missing (e.g. deleted for a reset — the
// FTN Setup Wizard refuses to re-run for an existing network, so this is
// the only recovery path). It is a no-op when the file exists or when no
// network has a parseable own address. created is true only when a new
// file was written.
func EnsureBinkdConf(bbsRoot string, ftnCfg config.FTNConfig, server config.ServerConfig) (created bool, err error) {
	confPath := filepath.Join(bbsRoot, "data", "ftn", "binkd.conf")
	if _, statErr := os.Stat(confPath); statErr == nil {
		return false, nil
	} else if !os.IsNotExist(statErr) {
		return false, fmt.Errorf("checking binkd.conf: %w", statErr)
	}
	cfg, nodes, ok := buildBinkdRegen(ftnCfg, server, bbsRoot)
	if !ok {
		return false, nil
	}
	if err := RegenerateBinkdConf(confPath, cfg, nodes); err != nil {
		return false, err
	}
	// The regenerated conf carries template defaults for port/loglevel;
	// bring them in line with the configured values.
	if err := SyncBinkdSettings(confPath, ftnCfg.Binkd.Port, ftnCfg.Binkd.LogLevel, cfg.outbound()); err != nil {
		return true, err
	}
	return true, nil
}

// NetworkOutbounds collects the per-network binkd_outbound_path overrides from
// an FTN config, keyed by lower-cased network name. Networks that share the
// global outbound are omitted, so a nil result means "no split configured".
func NetworkOutbounds(ftnCfg config.FTNConfig) map[string]string {
	out := make(map[string]string, len(ftnCfg.Networks))
	for name, netCfg := range ftnCfg.Networks {
		if netCfg.BinkdOutboundPath == "" {
			continue
		}
		out[strings.ToLower(name)] = netCfg.BinkdOutboundPath
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
