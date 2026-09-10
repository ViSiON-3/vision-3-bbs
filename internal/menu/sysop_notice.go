package menu

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// sysopNotice is one queued message for a sysop, waiting to be shown at their
// next login.
//
// Text is fully rendered (pipe codes included) at enqueue time and is what any
// notice without structured fields displays verbatim — entries queued by an
// older build, and any future producer that has nothing to re-render.
//
// A new-user notice also stores Handle and Node so the display side can render
// it fresh against the clock: how long ago the signup happened is only knowable
// when the sysop actually reads the notice, which may be days later. See
// MenuExecutor.renderSysopNotice.
type sysopNotice struct {
	Text      string    `json:"text"`
	Handle    string    `json:"handle,omitempty"`
	Node      int       `json:"node,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// sysopNoticesMu guards the on-disk notices file against concurrent node writes.
var sysopNoticesMu sync.Mutex

// sysopNoticesPath returns the notices file path for the given data dir, falling
// back to the conventional "data" dir when none is configured.
func sysopNoticesPath(dataDir string) string {
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "data"
	}
	return filepath.Join(dataDir, "sysop_notices.json")
}

// loadSysopNotices reads the per-user notice queues. A missing or empty file is
// an empty map, not an error, so first use needs no setup.
func loadSysopNotices(path string) (map[int][]sysopNotice, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[int][]sysopNotice{}, nil
		}
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return map[int][]sysopNotice{}, nil
	}
	var m map[int][]sysopNotice
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[int][]sysopNotice{}
	}
	return m, nil
}

// saveSysopNotices writes the queues atomically so a crash mid-write cannot
// corrupt the store. It uses the shared atomicfile helper, which also handles
// the Windows case where the destination is briefly open.
func saveSysopNotices(path string, m map[int][]sysopNotice) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(path, data, 0o644)
}

// enqueueSysopNotice appends a notice to one user's queue, stamping CreatedAt
// when the caller left it zero.
func enqueueSysopNotice(path string, userID int, n sysopNotice) error {
	sysopNoticesMu.Lock()
	defer sysopNoticesMu.Unlock()

	m, err := loadSysopNotices(path)
	if err != nil {
		return err
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}
	m[userID] = append(m[userID], n)
	return saveSysopNotices(path, m)
}

// peekSysopNotices returns one user's queued notices without removing them, so a
// caller can display them and clear the queue only once delivery has succeeded.
func peekSysopNotices(path string, userID int) ([]sysopNotice, error) {
	sysopNoticesMu.Lock()
	defer sysopNoticesMu.Unlock()

	m, err := loadSysopNotices(path)
	if err != nil {
		return nil, err
	}
	return m[userID], nil
}

// clearSysopNotices removes one user's queued notices.
func clearSysopNotices(path string, userID int) error {
	sysopNoticesMu.Lock()
	defer sysopNoticesMu.Unlock()

	m, err := loadSysopNotices(path)
	if err != nil {
		return err
	}
	if _, ok := m[userID]; !ok {
		return nil
	}
	delete(m, userID)
	return saveSysopNotices(path, m)
}

// drainSysopNotices returns and removes one user's queued notices in a single
// step. runSysopNotices deliberately does not use this — it peeks, displays,
// then clears, so a disconnect mid-display does not drop undelivered notices.
func drainSysopNotices(path string, userID int) ([]sysopNotice, error) {
	notices, err := peekSysopNotices(path, userID)
	if err != nil || len(notices) == 0 {
		return notices, err
	}
	if err := clearSysopNotices(path, userID); err != nil {
		return nil, err
	}
	return notices, nil
}

// humanizeAge renders how long ago something happened, as the age alone
// ("2 hours") so a string can place it — "signed up %s ago" — rather than the
// helper baking in the wording.
//
// Resolution coarsens with age on purpose: a sysop reading a three-day-old
// signup cares that it was three days, not three days and four hours. A
// negative age (a clock that moved backwards between enqueue and display)
// reads as the smallest unit rather than as a nonsense future date.
func humanizeAge(d time.Duration) string {
	plural := func(n int, unit string) string {
		if n == 1 {
			return fmt.Sprintf("%d %s", n, unit)
		}
		return fmt.Sprintf("%d %ss", n, unit)
	}
	switch {
	case d < time.Minute:
		return "less than a minute"
	case d < time.Hour:
		return plural(int(d/time.Minute), "minute")
	case d < 24*time.Hour:
		return plural(int(d/time.Hour), "hour")
	case d < 7*24*time.Hour:
		return plural(int(d/(24*time.Hour)), "day")
	default:
		return plural(int(d/(7*24*time.Hour)), "week")
	}
}

// renderSysopNotice produces the line shown for one queued notice.
//
// A new-user notice (one carrying a Handle) is rendered here rather than at
// enqueue time so it can state how long ago the signup happened — the live page
// says "just signed up", which stops being true the moment the notice is
// queued for a sysop who is not online to read it.
//
// Everything else falls back to the text stored at enqueue time: notices queued
// by a build that predates the structured fields, and a sysop who has blanked
// newUserSysopNotice to opt out of the wording.
func (e *MenuExecutor) renderSysopNotice(n sysopNotice, now time.Time) string {
	if n.Handle == "" {
		return n.Text
	}
	format := e.Strings().NewUserSysopNotice
	if format == "" {
		return n.Text
	}
	age := humanizeAge(now.Sub(n.CreatedAt))
	return fmt.Sprintf(format, n.Handle, age, n.Node)
}

// runSysopNotices is the SYSOPNOTICES login-sequence handler: it shows any
// queued notices for a co-sysop-or-above caller and clears them. It is quiet
// (prints nothing, no pause) for ordinary users or when the queue is empty, so
// it can sit in the login sequence without disturbing regular logins.
func runSysopNotices(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil || !e.isCoSysOpOrAbove(currentUser) {
		return currentUser, "", nil
	}

	// Read config under lock: the executor hot-reloads it, so a direct field
	// read would race an update (and trip the race detector).
	path := sysopNoticesPath(e.GetServerConfig().DataDir)

	// Peek, not drain: the queue is cleared only after the notices are actually
	// written, so a disconnect or write error mid-display leaves them queued for
	// the next login rather than losing them.
	notices, err := peekSysopNotices(path, currentUser.ID)
	if err != nil {
		slog.Warn("failed to read sysop notices", "node", nodeNumber, "handle", currentUser.Handle, "error", err)
		return currentUser, "", nil
	}
	if len(notices) == 0 {
		return currentUser, "", nil
	}

	if werr := terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode); werr != nil {
		return currentUser, "", nil // not delivered — leave queued
	}
	now := time.Now()
	for _, n := range notices {
		text := e.renderSysopNotice(n, now)
		if werr := terminalio.WriteStringCP437(terminal, ansi.ReplacePipeCodes([]byte(text)), outputMode); werr != nil {
			slog.Warn("failed to write a sysop notice; leaving the queue for next login",
				"node", nodeNumber, "handle", currentUser.Handle, "error", werr)
			return currentUser, "", nil
		}
		if werr := terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode); werr != nil {
			return currentUser, "", nil // leave queued
		}
	}

	// Delivered — safe to clear now.
	if cerr := clearSysopNotices(path, currentUser.ID); cerr != nil {
		slog.Warn("failed to clear delivered sysop notices", "node", nodeNumber, "handle", currentUser.Handle, "error", cerr)
	}
	slog.Info("delivered queued sysop notices at login", "node", nodeNumber, "handle", currentUser.Handle, "count", len(notices))

	// Pause so the notices are not scrolled off by the rest of the login
	// sequence before the sysop can read them.
	e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
	return currentUser, "", nil
}
