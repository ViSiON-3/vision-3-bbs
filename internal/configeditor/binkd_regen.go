package configeditor

import (
	"fmt"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

// buildBinkdRegen derives everything needed to regenerate a missing
// binkd.conf from configuration alone: identity from the server config,
// domains (zone parsed from each network's own address), address lines, and
// one node per link with a hostname. ok is false when no network has a
// parseable own address — then there is nothing meaningful to write.
func buildBinkdRegen(ftnCfg config.FTNConfig, server config.ServerConfig, bbsRoot string) (ftn.BinkdConfig, []ftn.BinkdNode, bool) {
	cfg := ftn.BinkdConfig{
		BBSRoot:   bbsRoot,
		BoardName: server.BoardName,
		SysopName: server.SysOpName,
		Location:  server.BBSLocation,
		Domains:   make(map[string]int),
	}
	var nodes []ftn.BinkdNode

	for netKey, nc := range ftnCfg.Networks {
		if nc.OwnAddress == "" {
			continue
		}
		addr, err := ftn.ParseAddress(nc.OwnAddress)
		if err != nil {
			continue
		}
		cfg.Domains[netKey] = addr.Zone
		cfg.Addresses = append(cfg.Addresses, fmt.Sprintf("%s@%s", nc.OwnAddress, netKey))
		for _, lnk := range nc.Links {
			if lnk.HostPort() == "" {
				continue
			}
			nodes = append(nodes, ftn.BinkdNode{
				Address:     fmt.Sprintf("%s@%s", lnk.Address, netKey),
				Hostname:    lnk.HostPort(),
				SessionPwd:  lnk.SessionPassword,
				NetworkName: netKey,
			})
		}
	}

	return cfg, nodes, len(cfg.Domains) > 0
}
