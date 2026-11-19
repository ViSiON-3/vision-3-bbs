package scripting

import (
	"io"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/pace"
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
	// MenuSetPath is the menu set the session draws from, where the script
	// looks up art: the caller's own theme (#1099), or "" for the one the
	// server runs.
	MenuSetPath string
	// DataDir is the script's data directory: the root of v3.fs, where
	// v3.data keeps its store and v3.gfx finds pictures. "" means
	// scripts/data as resolveDataDir finds it from WorkingDir. A theme's
	// menu script sets it to the theme's data/.
	DataDir string
	// Notice, when it fires while the script waits for a key (getkey,
	// readkey, yesno, noyes, pause), stops the script there, and Run returns
	// ErrNotice: the BBS has something to show the caller over the script's
	// screen and runs the script again afterwards, which draws its screen
	// anew, starting over from its first statement: whatever the script did
	// before that key wait, such as a write to v3.data, is done again, so
	// such work should be safe to repeat. Keys the engine has already read
	// in are given to the script
	// first; the rest are returned by Close. getstr and getnum are not
	// stopped, so a line being typed is not lost. nil never fires.
	Notice <-chan struct{}
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
	// SysOp reports that the caller is at the board's sysop level; only a
	// sysop sees test logins in v3.nodes.
	SysOp        bool
	TimeLimit    int // minutes
	TimesCalled  int
	Location     string
	ScreenWidth  int
	ScreenHeight int
	// ArtWidth is the width art is fitted to: the client's real column count
	// when it exceeds ScreenWidth (a saved preference), else ScreenWidth.
	// Zero means ScreenWidth.
	ArtWidth int
	// Sixel reports that the caller's terminal draws sixel graphics, read
	// by scripts as v3.gfx.sixel.
	Sixel bool
	// Audio reports that the caller's terminal said at connect that it plays
	// the board's audio format, read by scripts as v3.gfx.audio.
	Audio bool
	// Frames paces the canvas's picture frames (see pace.Gate); nil sends
	// every frame.
	Frames *pace.Gate
	// StripReplies takes the terminal's replies, the answers to Frames'
	// queries among them, out of input read from Session. nil leaves the
	// input as read.
	StripReplies func([]byte) []byte
	// TakeReply takes a whole terminal reply that came apart across reads
	// and so passed StripReplies, and reports whether it was one. nil
	// takes none.
	TakeReply func([]byte) bool

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
