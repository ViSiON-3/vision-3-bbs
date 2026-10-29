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
	return e.queueSysopKeyNotice(userManager, network, fmt.Sprintf(format, v3netNetworkLabel(network), kind))
}

// NoteV3NetKeyHandover tells every sysop at login that a leaf moved the
// pinned coordinator or hub key (kind) for network along a signed
// coordinator handover and saved the new key in v3net.json. The service
// calls it once per stored move; as with NoteV3NetKeyChange, a sysop who
// already has the same notice queued (after two moves in a row, say) is not
// given a second one. It returns the number of notices queued.
func (e *MenuExecutor) NoteV3NetKeyHandover(userManager *user.UserMgr, network, kind string) int {
	if userManager == nil {
		return 0
	}
	// Filled from StringFallbacks like v3netKeyChangedNotice.
	format := e.Strings().V3NetKeyHandoverNotice
	if format == "" {
		slog.Warn("v3net: key handover not queued for sysops: v3netKeyHandoverNotice has no text", "network", network)
		return 0
	}
	return e.queueSysopKeyNotice(userManager, network, fmt.Sprintf(format, v3netNetworkLabel(network), kind))
}

// queueSysopKeyNotice queues text for every sysop who does not already have
// it queued, and returns how many it queued.
func (e *MenuExecutor) queueSysopKeyNotice(userManager *user.UserMgr, network, text string) int {
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
			slog.Warn("v3net: could not queue a key notice", "network", network, "recipient", u.Handle, "error", err)
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
