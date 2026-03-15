package scripting

import (
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
}
