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

// saveSysopNotices writes the queues atomically (temp file + rename) so a crash
// mid-write cannot corrupt the store.
func saveSysopNotices(path string, m map[int][]sysopNotice) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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

// drainSysopNotices returns and removes one user's queued notices, so each is
// shown exactly once.
func drainSysopNotices(path string, userID int) ([]sysopNotice, error) {
	sysopNoticesMu.Lock()
	defer sysopNoticesMu.Unlock()

	m, err := loadSysopNotices(path)
	if err != nil {
		return nil, err
	}
	notices := m[userID]
	if len(notices) == 0 {
		return nil, nil
	}
	delete(m, userID)
	if err := saveSysopNotices(path, m); err != nil {
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

	path := sysopNoticesPath(e.ServerCfg.DataDir)
	notices, err := drainSysopNotices(path, currentUser.ID)
	if err != nil {
		slog.Warn("failed to read sysop notices", "node", nodeNumber, "handle", currentUser.Handle, "error", err)
		return currentUser, "", nil
	}
	if len(notices) == 0 {
		return currentUser, "", nil
	}

	terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
	for _, n := range notices {
		terminalio.WriteStringCP437(terminal, ansi.ReplacePipeCodes([]byte(n.Text)), outputMode)
		terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
	}
	slog.Info("delivered queued sysop notices at login", "node", nodeNumber, "handle", currentUser.Handle, "count", len(notices))

	// Pause so the notices are not scrolled off by the rest of the login
	// sequence before the sysop can read them.
	e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
	return currentUser, "", nil
}
