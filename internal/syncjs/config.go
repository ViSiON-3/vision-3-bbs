package syncjs

import (
	"io"
	"time"

	"github.com/stlalpha/vision3/internal/ansi"
)

// SyncJSDoorConfig holds the configuration specific to Synchronet JS door execution.
type SyncJSDoorConfig struct {
	Script       string   // Main JS file to execute (relative to WorkingDir)
	WorkingDir   string   // Working directory for the game
	LibraryPaths []string // Search paths for load()/require()
	Args         []string // Script arguments (available as argv)
	ExecDir      string   // Synchronet exec directory (where standard libs live)
	DataDir      string   // Data directory for game data files
	NodeDir      string   // Per-node temp directory
}

// SessionContext holds the BBS session state needed by the JS engine.
// This avoids a circular dependency between syncjs and menu packages.
type SessionContext struct {
	// I/O
	Session    io.ReadWriter // SSH/Telnet session for reading input and writing output
	OutputMode ansi.OutputMode

	// User info
	UserID       int
	UserHandle   string
	UserRealName string
	AccessLevel  int
	TimeLimit    int    // minutes
	TimesCalled  int
	Location     string
	ScreenWidth  int
	ScreenHeight int

	// Session info
	NodeNumber       int
	SessionStartTime time.Time

	// BBS info
	BoardName string
	SysOpName string
}
