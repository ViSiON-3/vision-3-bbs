package wfcui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
)

// pending reports whether a page still waits for the sysop.
func pending(ev admin.Event) bool { return ev.Type == admin.EventPage }

// pendingPages counts the pages the sysop has not answered or lost.
func (m Model) pendingPages() int {
	n := 0
	for _, p := range m.pages {
		if pending(p) {
			n++
		}
	}
	return n
}

func (m Model) pageIndex(node int) int {
	for i, p := range m.pages {
		if p.NodeID == node {
			return i
		}
	}
	return -1
}

// pageKey identifies one page request.
type pageKey struct {
	node int
	at   int64
}

// bellWindow is how recent a page must be to ring the bell; older ones are
// history replayed by the server.
const bellWindow = 30 * time.Second

// firstSight records a page and reports whether this console has not seen it
// before and it is recent. Keys older than an hour are dropped.
func (m *Model) firstSight(ev admin.Event) bool {
	now := m.now()
	for k, t := range m.seenPages {
		if now.Sub(t) > time.Hour {
			delete(m.seenPages, k)
		}
	}
	k := pageKey{ev.NodeID, ev.Time.UnixNano()}
	if _, ok := m.seenPages[k]; ok {
		return false
	}
	m.seenPages[k] = now
	d := now.Sub(ev.Time)
	return d <= bellWindow && d >= -bellWindow
}

// applyPageEvent folds a page or page-cleared event into m.pages. It reports
// whether a new pending page arrived. The server replays recent events after
// a reconnect, so a page already held (same time) is ignored.
func (m *Model) applyPageEvent(ev admin.Event) bool {
	i := m.pageIndex(ev.NodeID)
	switch ev.Type {
	case admin.EventPage:
		if i >= 0 && m.pages[i].Time.Equal(ev.Time) {
			return false
		}
		ring := m.firstSight(ev)
		if i < 0 {
			m.pages = append(m.pages, ev)
			return ring
		}
		fresh := !pending(m.pages[i])
		m.pages[i] = ev
		return ring && fresh
	case admin.EventPageCleared:
		if i < 0 {
			return false
		}
		if ev.Message == "logoff" {
			m.pages = append(m.pages[:i], m.pages[i+1:]...)
			m.clampPageSel()
			return false
		}
		if pending(m.pages[i]) {
			m.pages[i].Type = admin.EventPageCleared
			m.pages[i].Message += " (" + ev.Message + ")"
		}
	}
	return false
}

// dropGonePages removes pages whose node is no longer in the snapshot.
func (m *Model) dropGonePages() {
	var kept []admin.Event
	for _, p := range m.pages {
		for _, n := range m.snapshot.Nodes {
			if n.NodeID == p.NodeID {
				kept = append(kept, p)
				break
			}
		}
	}
	m.pages = kept
	m.clampPageSel()
	if len(m.pages) == 0 && m.mode == modePages {
		m.mode = modeList
	}
}

func (m *Model) clampPageSel() {
	if m.pageSel >= len(m.pages) {
		m.pageSel = len(m.pages) - 1
	}
	if m.pageSel < 0 {
		m.pageSel = 0
	}
}

func (m Model) ringBell() tea.Cmd {
	bell := m.opts.bell
	return func() tea.Msg { bell(); return nil }
}

// snoopOpenedMsg carries the result of opening a snoop stream.
type snoopOpenedMsg struct {
	connID int
	node   admin.NodeState
	chat   bool
	client admin.AdminClient
	st     *admin.SnoopStream
	err    error
}

// beginSnoop opens a snoop on n, the session the sysop picked. With chat set
// the snoop screen starts in chat (answering a page).
func (m Model) beginSnoop(n admin.NodeState, chat bool) (tea.Model, tea.Cmd) {
	if m.readOnly() {
		m.setStatus("Read-only console: snoop is disabled", true)
		return m, nil
	}
	if m.conn != connConnected || m.client == nil {
		m.setStatus("Not connected", true)
		return m, nil
	}
	sn, ok := m.client.(admin.Snooper)
	if !ok {
		m.setStatus("Snoop is not available on this connection", true)
		return m, nil
	}
	if m.snoopPending {
		m.setStatus("A snoop is already opening", true)
		return m, nil
	}
	m.snoopPending = true
	c, id := m.client, m.connID
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		st, err := sn.OpenSnoop(ctx, n.NodeID, n.ConnectedAt)
		return snoopOpenedMsg{connID: id, node: n, chat: chat, client: c, st: st, err: err}
	}
}

// snoopOpened starts the snoop screen once the stream is open.
func (m Model) snoopOpened(msg snoopOpenedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil || msg.st == nil || msg.connID != m.connID {
		m.snoopPending = false
	}
	if msg.connID != m.connID {
		if msg.st != nil {
			_ = msg.st.Close()
		}
		return m, nil
	}
	if msg.err != nil {
		m.setStatus("Snoop failed: "+errText(msg.err), true)
		return m, nil
	}
	if msg.st == nil {
		return m, nil
	}
	cmd := newSnoopCmd(msg.st, rpcControl{client: msg.client, node: msg.node}, msg.node.NodeID, stdRaw{})
	cmd.startChat = msg.chat
	return m, tea.Exec(cmd, func(err error) tea.Msg {
		res := cmd.Result()
		if err != nil {
			res.reason = "Snoop failed: " + errText(err)
		}
		return res
	})
}

// rpcControl drives type-in and chat on one session through the admin client.
type rpcControl struct {
	client admin.AdminClient
	node   admin.NodeState
}

func (c rpcControl) run(cmd admin.CommandType, key string, v bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	res, err := c.client.Execute(ctx, admin.AdminCommand{
		Command: cmd, NodeID: c.node.NodeID, ConnectedAt: c.node.ConnectedAt,
		Payload: map[string]any{key: v},
	})
	if err != nil {
		return err
	}
	if res != nil && !res.OK {
		return errors.New(res.Message)
	}
	return nil
}

func (c rpcControl) TypeIn(on bool) error  { return c.run(admin.CommandTypeIn, "on", on) }
func (c rpcControl) Chat(start bool) error { return c.run(admin.CommandChat, "start", start) }

// handleKeySnoop is S: snoop the selected caller.
func (m Model) handleKeySnoop() (tea.Model, tea.Cmd) {
	if m.focusBottom() {
		return m, nil
	}
	n, ok := m.selectedNode()
	if !ok {
		m.setStatus("No caller selected", true)
		return m, nil
	}
	return m.beginSnoop(n, false)
}

// handleKeyPages handles keys in the page list.
func (m Model) handleKeyPages(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyBackspace:
		m.mode = m.prevMode
	case tea.KeyDown:
		m.pageSel++
		m.clampPageSel()
	case tea.KeyUp:
		m.pageSel--
		m.clampPageSel()
	case tea.KeyEnter:
		return m.answerPage()
	case tea.KeyRunes:
		switch string(msg.Runes) {
		case "q", "Q":
			return m, tea.Quit
		case "p", "P":
			m.mode = m.prevMode
		}
	}
	return m, nil
}

// answerPage opens a snoop in chat on the selected page's caller.
func (m Model) answerPage() (tea.Model, tea.Cmd) {
	if m.pageSel < 0 || m.pageSel >= len(m.pages) {
		return m, nil
	}
	p := m.pages[m.pageSel]
	if !pending(p) {
		m.setStatus("That page is no longer pending", true)
		return m, nil
	}
	if m.snapshot != nil {
		for _, n := range m.snapshot.Nodes {
			if n.NodeID == p.NodeID {
				m.mode = modeList
				return m.beginSnoop(n, true)
			}
		}
	}
	m.setStatus("That caller is no longer online", true)
	return m, nil
}

// pageAge renders how long ago a page arrived.
func pageAge(t, now time.Time) string {
	d := max(now.Sub(t).Round(time.Second), 0)
	if d >= time.Hour {
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

// drawPages paints the page list overlay.
func (m Model) drawPages(s *screen, g geometry) {
	rows := min(max(len(m.pages), 1), 12)
	boxW := min(70, g.boxW)
	boxH := rows + 4
	x := (g.w - boxW) / 2
	y := max((g.h-boxH)/2, 1)
	s.fill(x, y, boxW, boxH, ' ', cLightGray, cBlack)
	s.box(x, y, boxW, boxH, boxColors{dim: cLightMagenta, bright: cLightMagenta})
	s.tab(x+1, y+1, boxW-2, "Pages", cMagenta, cWhite, cMagenta)
	if len(m.pages) == 0 {
		s.text(x+2, y+3, "No pages", cDarkGray, cBlack, x+boxW-2)
		return
	}
	first := 0
	if m.pageSel >= rows {
		first = m.pageSel - rows + 1
	}
	now := m.now()
	for i := 0; i < rows && first+i < len(m.pages); i++ {
		p := m.pages[first+i]
		fg, bg := cLightGray, cBlack
		if pending(p) {
			fg = cYellow
		}
		if first+i == m.pageSel {
			fg, bg = cBlack, cLightGray
		}
		handle := []rune(sanitizeTerminal(p.Handle))
		if len(handle) > 16 {
			handle = handle[:16]
		}
		line := fmt.Sprintf("%3d  %-16s %-6s  %s", p.NodeID, string(handle), pageAge(p.Time, now), sanitizeTerminal(p.Message))
		s.textPad(x+2, y+3+i, boxW-4, line, fg, bg)
	}
}
