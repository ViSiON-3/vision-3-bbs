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
