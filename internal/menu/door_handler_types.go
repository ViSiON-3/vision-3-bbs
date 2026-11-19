package menu

import (
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/scripting"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// doorUserInfo holds the user fields needed for dropfile generation.
type doorUserInfo struct {
	ID            int
	CreatedAt     time.Time // When the account was created; zero if unknown
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

	// Prompt is the kind of prompt a string's script stands in for (see
	// scripting.PromptYesNo); "" for a door run from a menu. Answer and
	// Answered carry back what the script passed to v3.prompt.answer.
	Prompt   string
	Answer   any
	Answered bool
	// Menu is the menu a script draws in place of its screen (v3.menu);
	// nil for any other door.
	Menu *scripting.MenuView
	// Notice stops a v3_script door's script at its key wait when it fires
	// (scripting.ScriptConfig.Notice); nil for any other door.
	Notice <-chan struct{}
	// IdleFrom, when set, is when the idle countdown started, before the
	// door: a menu script run again after a notice carries on the countdown
	// of the run before. Zero counts from the door's start.
	IdleFrom time.Time
	// ScriptDataDir is a v3_script door's data directory
	// (scripting.ScriptConfig.DataDir); "" for the one beside its scripts.
	ScriptDataDir string
	// Guest is set for a string's script run before login (see
	// runStringScript): User is empty, and a V3 script gets none of the
	// APIs that read or write the user, message, file or oneliner stores.
	Guest bool

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
