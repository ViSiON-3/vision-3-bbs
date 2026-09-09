package menu

import (
	"encoding/json"
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
// next login. Text is fully rendered (pipe codes included) at enqueue time.
type sysopNotice struct {
	Text      string    `json:"text"`
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

// enqueueSysopNotice appends a notice to one user's queue.
func enqueueSysopNotice(path string, userID int, text string) error {
	sysopNoticesMu.Lock()
	defer sysopNoticesMu.Unlock()

	m, err := loadSysopNotices(path)
	if err != nil {
		return err
	}
	m[userID] = append(m[userID], sysopNotice{Text: text, CreatedAt: time.Now()})
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
	for _, n := range notices {
		if werr := terminalio.WriteStringCP437(terminal, ansi.ReplacePipeCodes([]byte(n.Text)), outputMode); werr != nil {
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
