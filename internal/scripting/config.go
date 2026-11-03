package scripting

import (
	"io"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// ScriptConfig holds the configuration for a V3 script execution.
type ScriptConfig struct {
	Script     string   // Main JS file to execute
	WorkingDir string   // Working directory (sandbox root)
	Args       []string // Script arguments (available as v3.args)
	// MaxRunTime is the wall-clock limit on one run of the script: zero
	// means the default of 30 minutes, and a negative value means no limit.
	// It is not a CPU budget; a script waiting on the caller uses it up too.
	// Disconnect and whatever else ends the parent context still stop the
	// script either way.
	MaxRunTime time.Duration
	// Prompt is the kind of prompt the script stands in for when it runs in
	// place of a strings.json value: PromptYesNo, PromptPause, or "" for a
	// script that is not replacing a prompt. Exposed as v3.prompt.kind.
	Prompt string
}

// SessionContext holds the BBS session state needed by the V3 script engine.
type SessionContext struct {
	// I/O
	Session    io.ReadWriter
	OutputMode ansi.OutputMode

	// User info
	UserID       int
	UserHandle   string
	UserRealName string
	AccessLevel  int
	TimeLimit    int // minutes
	TimesCalled  int
	Location     string
	ScreenWidth  int
	ScreenHeight int
	// ArtWidth is the width art is fitted to: the client's real column count
	// when it exceeds ScreenWidth (a saved preference), else ScreenWidth.
	// Zero means ScreenWidth.
	ArtWidth int

	// Session info
	NodeNumber       int
	SessionStartTime time.Time
	// ChatCredit reports sysop chat time not charged to the caller; may be nil.
	ChatCredit func() time.Duration

	// BBS info
	BoardName  string
	SysOpName  string
	BBSVersion string
}
