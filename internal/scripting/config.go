package scripting

import (
	"io"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// ScriptConfig holds the configuration for a V3 script execution.
type ScriptConfig struct {
	Script     string        // Main JS file to execute
	WorkingDir string        // Working directory (sandbox root)
	Args       []string      // Script arguments (available as v3.args)
	MaxRunTime time.Duration // Maximum execution time (0 = use default of 30 minutes)
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

	// Session info
	NodeNumber       int
	SessionStartTime time.Time

	// BBS info
	BoardName  string
	SysOpName  string
	BBSVersion string
}
