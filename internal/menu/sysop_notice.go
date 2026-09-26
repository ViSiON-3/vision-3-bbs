package menu

import (
	"context"
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
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
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
//
// A new V3Net area notice carries the network, tag and name of the area, and
// is shown as an "Add?" question rather than a line of text: answering yes
// subscribes the BBS to the area. See NoteV3NetNAL.
type sysopNotice struct {
	Text      string    `json:"text"`
	Handle    string    `json:"handle,omitempty"`
	Node      int       `json:"node,omitempty"`
	CreatedAt time.Time `json:"created_at"`

	V3NetNetwork string `json:"v3net_network,omitempty"`
	V3NetTag     string `json:"v3net_tag,omitempty"`
	V3NetName    string `json:"v3net_name,omitempty"`
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

// removeSysopNotices removes the given notices from one user's queue and keeps
// the rest, including any queued since the caller read the queue.
func removeSysopNotices(path string, userID int, done []sysopNotice) error {
	sysopNoticesMu.Lock()
	defer sysopNoticesMu.Unlock()

	m, err := loadSysopNotices(path)
	if err != nil {
		return err
	}
	queue, ok := m[userID]
	if !ok {
		return nil
	}
	var kept []sysopNotice
	for _, n := range queue {
		match := false
		for _, d := range done {
			if sameSysopNotice(n, d) {
				match = true
				break
			}
		}
		if !match {
			kept = append(kept, n)
		}
	}
	if len(kept) == 0 {
		delete(m, userID)
	} else {
		m[userID] = kept
	}
	return saveSysopNotices(path, m)
}

// sameSysopNotice reports whether two notices are the same queued entry.
func sameSysopNotice(a, b sysopNotice) bool {
	return a.Text == b.Text && a.Handle == b.Handle && a.Node == b.Node &&
		a.CreatedAt.Equal(b.CreatedAt) && a.V3NetNetwork == b.V3NetNetwork &&
		a.V3NetTag == b.V3NetTag && a.V3NetName == b.V3NetName
}

// drainSysopNotices returns and removes one user's queued notices in a single
// step. runSysopNotices deliberately does not use this — it peeks, displays,
// then removes only what it delivered, so a disconnect mid-display does not
// drop undelivered notices.
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
// queued notices for a co-sysop-or-above caller, asks about any new V3Net
// areas, and removes what it delivered from the queue. It is quiet (prints
// nothing, no pause) for ordinary users or when the queue is empty, so it can
// sit in the login sequence without disturbing regular logins.
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

	// Peek, not drain: a notice leaves the queue only once it has actually
	// been written or answered, so a disconnect or write error mid-display
	// leaves the rest queued for the next login rather than losing them.
	notices, err := peekSysopNotices(path, currentUser.ID)
	if err != nil {
		slog.Warn("failed to read sysop notices", "node", nodeNumber, "handle", currentUser.Handle, "error", err)
		return currentUser, "", nil
	}
	if len(notices) == 0 {
		return currentUser, "", nil
	}

	var plain, offers []sysopNotice
	for _, n := range notices {
		if n.V3NetTag != "" {
			offers = append(offers, n)
		} else {
			plain = append(plain, n)
		}
	}

	var done []sysopNotice
	defer func() {
		if len(done) == 0 {
			return
		}
		if rerr := removeSysopNotices(path, currentUser.ID, done); rerr != nil {
			slog.Warn("failed to clear delivered sysop notices", "node", nodeNumber, "handle", currentUser.Handle, "error", rerr)
		}
		slog.Info("delivered queued sysop notices at login", "node", nodeNumber, "handle", currentUser.Handle, "count", len(done))
	}()

	wrote := false
	if len(plain) > 0 {
		if werr := terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode); werr != nil {
			return currentUser, "", nil // not delivered — leave queued
		}
		now := time.Now()
		for _, n := range plain {
			text := e.renderSysopNotice(n, now)
			if werr := terminalio.WriteStringCP437(terminal, ansi.ReplacePipeCodes([]byte(text)), outputMode); werr != nil {
				slog.Warn("failed to write a sysop notice; leaving the rest queued for next login",
					"node", nodeNumber, "handle", currentUser.Handle, "error", werr)
				return currentUser, "", nil
			}
			if werr := terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode); werr != nil {
				return currentUser, "", nil // leave queued
			}
			done = append(done, n)
			wrote = true
		}
	}

	if len(offers) > 0 {
		handled, asked, aborted := e.offerV3NetAreas(c, offers, !wrote)
		done = append(done, handled...)
		wrote = wrote || asked
		if aborted {
			return currentUser, "", nil
		}
	}

	// Pause so the notices are not scrolled off by the rest of the login
	// sequence before the sysop can read them.
	if wrote {
		e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
	}
	return currentUser, "", nil
}

// offerV3NetAreas asks, one area at a time, whether to add each offered V3Net
// area, and subscribes the BBS to the ones the sysop accepts. It returns the
// notices that are settled and can leave the queue, whether anything was
// written, and whether the caller disconnected part way. leadIn starts the
// first question on a fresh line, for when nothing was printed above it.
//
// An offer is settled without asking when the BBS already carries the area
// (another sysop or the area browser got there first) or the hub has since
// dropped it. It stays queued when V3Net is not running, or when adding the
// area fails, so it is asked again at the next login.
func (e *MenuExecutor) offerV3NetAreas(c *cmdCtx, offers []sysopNotice, leadIn bool) (handled []sysopNotice, asked, aborted bool) {
	terminal := c.terminal
	outputMode := c.outputMode
	write := func(text string) {
		_ = terminalio.WriteStringCP437(terminal, ansi.ReplacePipeCodes([]byte(text)), outputMode)
	}

	if !e.isSysOpOrAbove(c.currentUser) {
		// Queued while this account was a sysop; adding areas is a sysop's
		// call, so drop the offers rather than ask.
		return offers, false, false
	}
	svc := e.V3NetStatus
	if svc == nil {
		return nil, false, false
	}

	format := e.Strings().V3NetNewAreaNotice
	nals := map[string]*protocol.NAL{}
	var added []string
	for _, n := range offers {
		if v3netSubscribedBoards(e.RootConfigPath, n.V3NetNetwork)[n.V3NetTag] {
			handled = append(handled, n)
			continue
		}
		current, fetched := nals[n.V3NetNetwork]
		if !fetched {
			ctx, cancel := context.WithTimeout(context.Background(), v3netManageTimeout)
			current, _ = svc.FetchNALForNetwork(ctx, n.V3NetNetwork)
			cancel()
			nals[n.V3NetNetwork] = current
		}
		area := protocol.Area{Tag: n.V3NetTag, Name: n.V3NetName}
		if current != nil {
			found := current.FindArea(n.V3NetTag)
			if found == nil {
				handled = append(handled, n) // dropped by the hub since
				continue
			}
			area = *found
		}

		prompt := n.Text
		if format != "" {
			prompt = fmt.Sprintf(format, v3netNetworkLabel(n.V3NetNetwork), area.Name)
		}
		// The lightbar ends its own line once answered, so only the first
		// question may need a line break in front of it.
		if leadIn && !asked {
			write("\r\n")
		}
		yes, err := e.PromptYesNo(c.s, terminal, prompt, outputMode, c.nodeNumber, c.termWidth, c.termHeight, true)
		if err != nil {
			return handled, asked, true
		}
		asked = true
		if !yes {
			handled = append(handled, n)
			continue
		}
		if err := v3netSubscribe(e.RootConfigPath, e.MessageMgr, n.V3NetNetwork, svc.HubURLForNetwork(n.V3NetNetwork), area, true); err != nil {
			slog.Warn("v3net: could not add offered area", "network", n.V3NetNetwork, "tag", area.Tag, "error", err)
			write(fmt.Sprintf("\r\n|04Could not add %s: %s. You will be asked again next time.|07", area.Tag, err))
			continue
		}
		handled = append(handled, n)
		added = append(added, area.Tag)
		slog.Info("v3net: sysop added offered area", "network", n.V3NetNetwork, "tag", area.Tag, "handle", c.currentUser.Handle)
	}

	if len(added) > 0 {
		list := strings.Join(added, ", ")
		switch {
		case e.V3NetReload == nil:
			write(fmt.Sprintf("\r\n|10Added %s. Restart to activate.|07\r\n", list))
		default:
			if rerr := e.V3NetReload(); rerr != nil {
				write(fmt.Sprintf("\r\n|10Added %s.|07 |04Live apply failed (%s); restart to activate.|07\r\n", list, rerr))
			} else {
				write(fmt.Sprintf("\r\n|10Added %s. Now active.|07\r\n", list))
			}
		}
	}
	return handled, asked, false
}
