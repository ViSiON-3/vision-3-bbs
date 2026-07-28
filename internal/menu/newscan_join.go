package menu

import (
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// newscanJoinPlan describes newscan additions computed at login for areas
// flagged AutoJoin that the user has not been offered before.
type newscanJoinPlan struct {
	Grandfather      bool                // user predates seen-set tracking; initialize only
	SilentTags       []string            // local (non-network) tags to add without prompting
	NetworkTags      map[string][]string // lowercase network -> unseen AutoJoin tags, gated on a prompt
	NetworkNames     map[string]string   // lowercase network -> display name for the prompt
	ResidualSeenTags []string            // tags of unseen AutoJoin areas whose network was already seen: mark seen, never tag
}

// buildNewscanJoinPlan computes which AutoJoin areas are new to the user.
// canRead filters areas by the user's read access.
func buildNewscanJoinPlan(areas []*message.MessageArea, u *user.User, canRead func(*message.MessageArea) bool) newscanJoinPlan {
	if !u.NewscanSeenInitialized {
		return newscanJoinPlan{Grandfather: true}
	}

	seenTags := make(map[string]bool, len(u.SeenNewscanAreaTags))
	for _, t := range u.SeenNewscanAreaTags {
		seenTags[t] = true
	}
	seenNets := make(map[string]bool, len(u.SeenNewscanNetworks))
	for _, n := range u.SeenNewscanNetworks {
		seenNets[strings.ToLower(n)] = true
	}

	plan := newscanJoinPlan{
		NetworkTags:  map[string][]string{},
		NetworkNames: map[string]string{},
	}
	promptedNets := map[string]bool{}
	for _, area := range areas {
		if !area.AutoJoin || seenTags[area.Tag] || !canRead(area) {
			continue
		}
		if area.Network == "" {
			plan.SilentTags = append(plan.SilentTags, area.Tag)
			continue
		}
		net := strings.ToLower(area.Network)
		if seenNets[net] {
			plan.ResidualSeenTags = append(plan.ResidualSeenTags, area.Tag)
			continue // network-level decision already made; area is seen, not tagged
		}
		plan.NetworkTags[net] = append(plan.NetworkTags[net], area.Tag)
		if !promptedNets[net] {
			promptedNets[net] = true
			plan.NetworkNames[net] = area.Network
		}
	}
	return plan
}

// initNewscanSeen marks the current world as offered: every AutoJoin area
// tag and every network name (AutoJoin or not) becomes "seen", so only
// genuinely new areas/networks trigger login-time handling later. It also
// sets the explicit init marker, which is what future logins check instead
// of slice emptiness — an initialized user in an empty world (zero AutoJoin
// areas, zero networks) must not be re-grandfathered forever.
func initNewscanSeen(u *user.User, areas []*message.MessageArea) {
	var tags []string
	netSet := map[string]bool{}
	var nets []string
	for _, area := range areas {
		if area.AutoJoin {
			tags = append(tags, area.Tag)
		}
		if area.Network != "" {
			net := strings.ToLower(area.Network)
			if !netSet[net] {
				netSet[net] = true
				nets = append(nets, net)
			}
		}
	}
	u.SeenNewscanAreaTags = tags
	u.SeenNewscanNetworks = nets
	u.NewscanSeenInitialized = true
}
