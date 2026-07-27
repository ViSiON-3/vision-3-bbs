package menu

import (
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runNewscanAutoJoin adds newly flagged AutoJoin areas to an existing
// user's newscan at login: silently for local areas, gated on a one-time
// Y/n prompt per new network. Users with no seen-set state (pre-feature
// accounts) are grandfathered: the current world is marked offered without
// changing their newscan. Returns the updated user (nil if unchanged).
func runNewscanAutoJoin(c *cmdCtx) (*user.User, error) {
	e := c.e
	u := c.currentUser
	if e.MessageMgr == nil || u == nil {
		return nil, nil
	}

	allAreas := e.MessageMgr.ListAreas()
	canRead := func(a *message.MessageArea) bool {
		return checkACS(a.ACSRead, u, c.s, c.terminal, c.sessionStartTime)
	}
	plan := buildNewscanJoinPlan(allAreas, u, canRead)

	if plan.Grandfather {
		initNewscanSeen(u, allAreas)
		if len(u.SeenNewscanAreaTags) == 0 && len(u.SeenNewscanNetworks) == 0 {
			return nil, nil // nothing to record yet
		}
		if err := c.userManager.UpdateUser(u); err != nil {
			slog.Error("failed to save grandfathered newscan seen-sets", "node", c.nodeNumber, "handle", u.Handle, "error", err)
			return nil, nil
		}
		return u, nil
	}

	if len(plan.SilentTags) == 0 && len(plan.NetworkTags) == 0 && len(plan.SeenTags) == 0 {
		return nil, nil
	}

	since := time.Now().AddDate(0, 0, -message.NewscanSeedDays)
	tagged := make(map[string]bool, len(u.TaggedMessageAreaTags))
	for _, t := range u.TaggedMessageAreaTags {
		tagged[t] = true
	}
	addTag := func(tag string) {
		if !tagged[tag] {
			tagged[tag] = true
			u.TaggedMessageAreaTags = append(u.TaggedMessageAreaTags, tag)
		}
		if area, ok := e.MessageMgr.GetAreaByTag(tag); ok {
			if err := e.MessageMgr.SeedLastRead(area.ID, u.Handle, since); err != nil {
				slog.Warn("failed to seed lastread on auto-join", "node", c.nodeNumber, "area", area.ID, "handle", u.Handle, "error", err)
			}
		}
	}

	for _, tag := range plan.SilentTags {
		addTag(tag)
	}
	if len(plan.SilentTags) > 0 {
		slog.Info("silently added local areas to newscan", "node", c.nodeNumber, "handle", u.Handle, "count", len(plan.SilentTags))
	}

	promptTpl := e.LoadedStrings.NewscanNewNetworkPrompt
	if promptTpl == "" {
		promptTpl = "|15New network |14%s|15 is available - add to your newscan?"
	}
	nets := make([]string, 0, len(plan.NetworkTags))
	for net := range plan.NetworkTags {
		nets = append(nets, net)
	}
	sort.Strings(nets)
	for _, net := range nets {
		prompt := fmt.Sprintf(promptTpl, plan.NetworkNames[net])
		yes, err := e.PromptYesNo(c.s, c.terminal, prompt, c.outputMode, c.nodeNumber, c.termWidth, c.termHeight, true)
		if err != nil {
			return nil, err // EOF/disconnect — caller handles
		}
		if yes {
			for _, tag := range plan.NetworkTags[net] {
				addTag(tag)
			}
		}
	}

	u.SeenNewscanAreaTags = append(u.SeenNewscanAreaTags, plan.SeenTags...)
	u.SeenNewscanNetworks = append(u.SeenNewscanNetworks, plan.SeenNets...)
	if err := c.userManager.UpdateUser(u); err != nil {
		slog.Error("failed to save newscan auto-join", "node", c.nodeNumber, "handle", u.Handle, "error", err)
	}
	return u, nil
}
