package menu

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// NoteV3NetKeyChange is the V3Net service's key-change observer. A leaf
// calls it when a hub offers a coordinator or hub key (kind) for network
// that differs from the key pinned in v3net.json. The leaf has already
// rejected the key and logged both keys; this queues a login warning for
// every sysop so the change is not missed in the log. A sysop who already
// has the same warning queued is not given a second one. It returns the
// number of notices queued.
func (e *MenuExecutor) NoteV3NetKeyChange(userManager *user.UserMgr, network, kind string) int {
	if userManager == nil {
		return 0
	}
	// LoadStrings fills a blank v3netKeyChangedNotice from StringFallbacks,
	// so this is only empty for an executor built without loaded strings.
	format := e.Strings().V3NetKeyChangedNotice
	if format == "" {
		slog.Warn("v3net: key change not queued for sysops: v3netKeyChangedNotice has no text", "network", network)
		return 0
	}
	text := fmt.Sprintf(format, v3netNetworkLabel(network), kind)

	path := sysopNoticesPath(e.GetServerConfig().DataDir)
	queued := 0
	for _, u := range userManager.GetAllUsers() {
		if u == nil || u.DeletedUser || !e.isSysOpOrAbove(u) {
			continue
		}
		pending, err := peekSysopNotices(path, u.ID)
		if err != nil {
			slog.Warn("v3net: could not read sysop notices", "recipient", u.Handle, "error", err)
			continue
		}
		if hasNoticeText(pending, text) {
			continue
		}
		if err := enqueueSysopNotice(path, u.ID, sysopNotice{Text: text, CreatedAt: time.Now()}); err != nil {
			slog.Warn("v3net: could not queue a key-change notice", "network", network, "recipient", u.Handle, "error", err)
			continue
		}
		queued++
	}
	return queued
}

// hasNoticeText reports whether queue holds a plain-text notice with text.
func hasNoticeText(queue []sysopNotice, text string) bool {
	for _, n := range queue {
		if n.Text == text && n.Handle == "" && n.V3NetTag == "" {
			return true
		}
	}
	return false
}
