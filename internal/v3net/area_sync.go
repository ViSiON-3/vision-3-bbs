// Package v3net provides the V3Net networking service.
package v3net

import (
	"log/slog"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// SyncAreas ensures that every V3Net leaf subscription has a corresponding
// message area in message_areas.json. Missing areas are auto-created with
// type "v3net" and persisted to disk. Returns the number of areas created.
func SyncAreas(leaves []config.V3NetLeafConfig, mgr *message.MessageManager) int {
	created := 0
	for _, lcfg := range leaves {
		if lcfg.Board == "" {
			continue
		}
		if _, ok := mgr.GetAreaByTag(lcfg.Board); ok {
			continue
		}

		area := message.MessageArea{
			Tag:      lcfg.Board,
			Name:     lcfg.Board,
			AreaType: "v3net",
			Network:  lcfg.Network,
			EchoTag:  lcfg.Board,
		}

		id, err := mgr.AddArea(area)
		if err != nil {
			slog.Error("v3net: auto-create area failed",
				"tag", lcfg.Board, "network", lcfg.Network, "error", err)
			continue
		}
		slog.Info("v3net: auto-created message area",
			"id", id, "tag", lcfg.Board, "network", lcfg.Network)
		created++
	}
	return created
}
