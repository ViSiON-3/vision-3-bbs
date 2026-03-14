package scripting

import (
	"github.com/stlalpha/vision3/internal/file"
	"github.com/stlalpha/vision3/internal/message"
	"github.com/stlalpha/vision3/internal/session"
	"github.com/stlalpha/vision3/internal/user"
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
