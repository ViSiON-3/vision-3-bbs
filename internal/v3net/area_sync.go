// Package v3net provides the V3Net networking service.
package v3net

import (
	"log/slog"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// SyncAreas ensures that every V3Net leaf subscription has a corresponding
// message area in message_areas.json. Missing areas are auto-created with
// type "v3net" and persisted to disk. Returns the number of areas created.
func SyncAreas(leaves []config.V3NetLeafConfig, mgr *message.MessageManager, confMgr *conference.ConferenceManager) int {
	created := 0
	for _, lcfg := range leaves {
		for _, board := range lcfg.Boards {
			if board == "" {
				continue
			}
			if _, ok := mgr.GetAreaByTag(board); ok {
				continue
			}

			area := message.MessageArea{
				Tag:          board,
				Name:         areaNameFromTag(board, lcfg.Network),
				AreaType:     "v3net",
				Network:      lcfg.Network,
				EchoTag:      board,
				ConferenceID: inferConferenceID(mgr, confMgr, lcfg.Network),
				AutoJoin:     true,
				ACSRead:      "s10",
				ACSWrite:     "s20",
			}

			id, err := mgr.AddArea(area)
			if err != nil {
				slog.Error("v3net: auto-create area failed",
					"tag", board, "network", lcfg.Network, "error", err)
				continue
			}
			slog.Info("v3net: auto-created message area",
				"id", id, "tag", board, "network", lcfg.Network,
				"conference_id", area.ConferenceID)
			created++
		}
	}
	return created
}

// areaNameFromTag generates a friendly display name from a v3net area tag.
// e.g. "fel.general" with network "felonynet" → "FelonyNet General".
func areaNameFromTag(tag, network string) string {
	// Split on the dot: prefix.name
	parts := strings.SplitN(tag, ".", 2)
	if len(parts) != 2 {
		return tag
	}
	suffix := parts[1]

	// Title-case the suffix (replace hyphens with spaces).
	words := strings.Split(suffix, "-")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}

	// Try to use a nice network name prefix.
	if len(network) == 0 {
		return strings.Join(words, " ")
	}
	netName := strings.ToUpper(network[:1]) + network[1:]
	return netName + " " + strings.Join(words, " ")
}

// inferConferenceID determines the conference ID for a new v3net area.
// First checks existing v3net areas in the same network, then falls back
// to matching the network name against conference tags (case-insensitive).
func inferConferenceID(mgr *message.MessageManager, confMgr *conference.ConferenceManager, network string) int {
	// Check existing v3net areas in the same network.
	for _, a := range mgr.ListAreas() {
		if a.AreaType == "v3net" && a.Network == network && a.ConferenceID != 0 {
			return a.ConferenceID
		}
	}

	// Fall back to matching conference tag by network name.
	if confMgr != nil {
		upperNet := strings.ToUpper(network)
		if conf, ok := confMgr.GetByTag(upperNet); ok {
			return conf.ID
		}
	}

	return 0
}
