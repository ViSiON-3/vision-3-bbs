package scripting

import (
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Providers holds optional manager references for V3 script API access.
// These are nil for Phase 1 (console-only) scripts and populated as
// later phases add user, message, and file bindings.
type Providers struct {
	UserMgr         *user.UserMgr
	CurrentUser     *user.User // live pointer to current session's user
	MessageMgr      *message.MessageManager
	FileMgr         *file.FileManager
	SessionRegistry *session.SessionRegistry
	Oneliners       OnelinerStore
	// CanDownload reports whether the caller may download from an area,
	// by its download ACS. v3.file.read, members, readMember and mark need
	// it; nil means no area is readable or markable.
	CanDownload func(*file.FileArea) bool
	// CanList reports whether the caller may see an area and its files, by
	// its list ACS. v3.file.areas, area, list, count, search and totalCount
	// leave out the areas it refuses; nil means no area is visible.
	CanList func(*file.FileArea) bool
	// Menu is the menu a script draws in place of its screen, read as
	// v3.menu; nil for any other script.
	Menu *MenuView
}

// OnelinerView is one oneliner as the script sees it.
type OnelinerView struct {
	Text      string
	Handle    string
	Anonymous bool
	PostedAt  time.Time
}

// OnelinerStore reads and appends oneliners for the v3.oneliners binding.
// Recent returns the newest first, up to limit (0 or less means all).
type OnelinerStore interface {
	Recent(limit int) []OnelinerView
	Add(handle, username, text string, anonymous bool) error
}
