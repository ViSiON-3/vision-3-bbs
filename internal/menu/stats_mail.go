package menu

import (
	"log/slog"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// statsWaitingMail uses the same private-mail and last-read rules as NMAILSCAN.
// An unavailable mail store is unknown rather than an empty mailbox.
func (e *MenuExecutor) statsWaitingMail(u *user.User) string {
	if e.MessageMgr == nil {
		return "N/A"
	}
	area, ok := e.MessageMgr.GetAreaByTag("PRIVMAIL")
	if !ok {
		return "0"
	}
	base, err := e.MessageMgr.GetBase(area.ID)
	if err != nil {
		return "N/A"
	}
	defer func() {
		if err := base.Close(); err != nil {
			slog.Warn("closing stats private-mail base", "error", err)
		}
	}()
	total, err := e.MessageMgr.GetMessageCountForArea(area.ID)
	if err != nil {
		return "N/A"
	}
	last, err := e.MessageMgr.GetLastRead(area.ID, u.Handle)
	if err != nil {
		return "N/A"
	}
	count := 0
	for n := last + 1; n <= total; n++ {
		msg, err := base.ReadMessage(n)
		if err != nil {
			return "N/A"
		}
		if !msg.IsDeleted() && msg.IsPrivate() && strings.EqualFold(msg.To, u.Handle) {
			count++
		}
	}
	return strconv.Itoa(count)
}
