package tosser

import (
	"log/slog"
	"sort"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// OrphanFTNAreas returns the FTN (echomail and netmail) areas whose network
// names no network in ftn.json, sorted by tag. Such an area never receives
// mail: the tosser finds a network's netmail area, and gates its echo-tag
// lookups, by comparing the area's network with the ftn.json network name. A
// one-character slip (zer0net vs zeronet) looks right on screen and silently
// drops that network's netmail.
func OrphanFTNAreas(cfg config.FTNConfig, areas []*message.MessageArea) []*message.MessageArea {
	var orphans []*message.MessageArea
	for _, a := range areas {
		t := strings.ToLower(a.AreaType)
		if t != "echomail" && t != "echo" && t != "netmail" {
			continue
		}
		if ftnNetworkKnown(cfg, a.Network) {
			continue
		}
		orphans = append(orphans, a)
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i].Tag < orphans[j].Tag })
	return orphans
}

func ftnNetworkKnown(cfg config.FTNConfig, network string) bool {
	for name := range cfg.Networks {
		if strings.EqualFold(name, network) {
			return true
		}
	}
	return false
}

// WarnOrphanFTNAreas logs one warning per area OrphanFTNAreas reports. It is a
// no-op when ftn.json defines no networks, since then FTN is simply not in use.
func WarnOrphanFTNAreas(cfg config.FTNConfig, areas []*message.MessageArea) {
	if len(cfg.Networks) == 0 {
		return
	}
	known := make([]string, 0, len(cfg.Networks))
	for name := range cfg.Networks {
		known = append(known, name)
	}
	sort.Strings(known)
	for _, a := range OrphanFTNAreas(cfg, areas) {
		slog.Warn("FTN message area names a network that is not in ftn.json — it will receive no mail; set its network to one of the configured networks",
			"area", a.Tag, "type", a.AreaType, "network", a.Network, "ftn_networks", strings.Join(known, ","))
	}
}
