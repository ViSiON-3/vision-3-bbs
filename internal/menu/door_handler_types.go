package menu

import (
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
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
	UserManager      *user.UserMgr // For V3 scripts that need user DB access
	CurrentUser      *user.User    // Live pointer to current session's user record
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
	// IdleTimeout is how long the caller may send nothing before the door
	// is ended and the caller logged off; 0 for a caller exempt from the
	// idle timeout.
	IdleTimeout time.Duration
	BaudStr     string
	UserIDStr   string
	Subs        map[string]string

	// watch decides when the BBS must end the door. Set by executeDoor.
	watch *doorWatch
	// enterTrailer says the session's last reader took the CR of an Enter
	// whose LF or NUL had not arrived, so the door's input drops a leading
	// one (takeEnterTrailer). Set by runDoorWatched; whatever reads the
	// session for the door takes it with takeDoorEnterTrailer.
	enterTrailer bool
	// baseCOM1Virtual records that a DOS door's base dosemurc sets
	// $_com1 = "virtual". Set by writeDosemurc.
	baseCOM1Virtual bool
}

// takeDoorEnterTrailer returns enterTrailer for the reader the door starts
// on the session, which then owns the mark, and clears it. Call it before
// starting that reader, on the goroutine that runs the door: a mark nothing
// took is passed on to the next reader when the door ends (runDoorWatched),
// as it is when the door fails before reading anything.
func (ctx *DoorCtx) takeDoorEnterTrailer() bool {
	t := ctx.enterTrailer
	ctx.enterTrailer = false
	return t
}

// dosSerial reports whether a DOS door talks to the caller through COM1,
// either via the configured FOSSIL driver or because its base dosemurc maps
// COM1 to the terminal, rather than through the DOS screen.
func (ctx *DoorCtx) dosSerial() bool {
	return ctx.Config.FossilDriver != "" || ctx.baseCOM1Virtual
}
