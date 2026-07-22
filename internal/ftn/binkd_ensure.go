package ftn

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// buildBinkdRegen derives everything needed to regenerate a missing
// binkd.conf from configuration alone: identity from the server config,
// domains (zone parsed from each network's own address), address lines, and
// one node per link with a hostname. ok is false when no network has a
// parseable own address — then there is nothing meaningful to write.
func buildBinkdRegen(ftnCfg config.FTNConfig, server config.ServerConfig, bbsRoot string) (BinkdConfig, []BinkdNode, bool) {
	cfg := BinkdConfig{
		BBSRoot:   bbsRoot,
		BoardName: server.BoardName,
		SysopName: server.SysOpName,
		Location:  server.BBSLocation,
		Domains:   make(map[string]int),
	}
	var nodes []BinkdNode

	for netKey, nc := range ftnCfg.Networks {
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
	if err := SyncBinkdSettings(confPath, ftnCfg.Binkd.Port, ftnCfg.Binkd.LogLevel); err != nil {
		return true, err
	}
	return true, nil
}
