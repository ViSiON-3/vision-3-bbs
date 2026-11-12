package menu

import (
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// CanAccessSponsorMenu returns true if u is permitted to enter the sponsor menu
// for the given message area.
//
// Access is granted when any of the following is true:
//   - u.AccessLevel >= cfg.SysOpLevel   (full sysop)
//   - u.AccessLevel >= cfg.CoSysOpLevel (co-sysop)
//   - area.Sponsor is non-empty and matches u.Handle (case-insensitive)
func CanAccessSponsorMenu(u *user.User, area *message.MessageArea, cfg config.ServerConfig) bool {
	if u == nil || area == nil {
		return false
	}
	if u.AccessLevel >= cfg.SysOpLevel {
		return true
	}
	if u.AccessLevel >= cfg.CoSysOpLevel {
		return true
	}
	if area.Sponsor != "" && strings.EqualFold(area.Sponsor, u.Handle) {
		return true
	}
	return false
}

// areaLookup is the subset of MessageManager's method set the sponsor keyword needs,
// defined here (at the consumer) so tests can substitute a fake.
type areaLookup interface {
	GetAreaByID(id int) (*message.MessageArea, bool)
}

// sponsorKeyword resolves the {{SPONSOR}} conditional-region keyword: true
// when u could enter the sponsor menu for their currently selected message
// area. Nil user, nil lookup, or no selected/known area all yield false.
func sponsorKeyword(u *user.User, areas areaLookup, cfg config.ServerConfig) bool {
	if u == nil || areas == nil || u.CurrentMessageAreaID <= 0 {
		return false
	}
	area, found := areas.GetAreaByID(u.CurrentMessageAreaID)
	if !found {
		return false
	}
	return CanAccessSponsorMenu(u, area, cfg)
}

// sponsorsArea reports whether u is the sponsor of message area areaID, and
// so may moderate it in the message reader: delete any message there, as a
// co-sysop can anywhere. The area is looked up afresh, so a sponsor the
// sysop has just removed loses it at once. Private mail has no moderator:
// PRIVMAIL's sponsor field gives nobody other callers' mail.
func sponsorsArea(u *user.User, areas areaLookup, areaID int) bool {
	if u == nil || areas == nil {
		return false
	}
	area, found := areas.GetAreaByID(areaID)
	if !found || area == nil || strings.EqualFold(area.Tag, "PRIVMAIL") {
		return false
	}
	return area.Sponsor != "" && strings.EqualFold(area.Sponsor, u.Handle)
}
