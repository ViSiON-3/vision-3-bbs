package menu

import (
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"golang.org/x/term"
)

// doorUserInfo holds the user fields needed for dropfile generation.
type doorUserInfo struct {
	ID            int
	Handle        string
	RealName      string
	AccessLevel   int
	TimeLimit     int
	TimesCalled   int
	GroupLocation string
	ScreenWidth   int
	ScreenHeight  int
}

// DoorCtx holds all context needed to execute a door program.
type DoorCtx struct {
	Executor         *MenuExecutor
	Session          ssh.Session
	Terminal         *term.Terminal
	User             doorUserInfo
	UserManager      *user.UserMgr   // For V3 scripts that need user DB access
	CurrentUser      *user.User      // Live pointer to current session's user record
	NodeNumber       int
	SessionStartTime time.Time
	OutputMode       ansi.OutputMode
	Config           config.DoorConfig
	DoorName         string
	// Pre-computed values
	NodeNumStr  string
	PortStr     string
	TimeLeftMin int
	TimeLeftStr string
	BaudStr     string
	UserIDStr   string
	Subs        map[string]string
}
