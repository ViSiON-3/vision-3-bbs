// Package wfcui is the transport-agnostic Bubble Tea TUI for the WFC console.
// It depends only on internal/admin (interface + types).
package wfcui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
)

type viewMode int

const (
	modeList viewMode = iota
	modeDetails
	modeConfirmKick
)

// group is one of the tabbed views in the lower box. The upper box always
// shows live callers.
type group int

const (
	groupLog    group = iota // the callers' activity log
	groupBots                // log of anonymous connections (scanners, probes)
	groupEvents              // the event scheduler's entries
	groupCount
)

func (g group) String() string {
	switch g {
	case groupLog:
		return "Callers"
	case groupBots:
		return "Bots"
	case groupEvents:
		return "Events"
	}
	return "?"
}

// connState is where the console stands with the daemon.
type connState int

const (
	// connConnected: a live client; snapshots and events are flowing.
	connConnected connState = iota
	// connConnecting: a dial is in flight.
	connConnecting
	// connLost: no client; waiting for nextRetryAt (or for the operator to
	// press R) before dialling again.
	connLost
)

const (
	minWidth  = 80
	minHeight = 25

	// dialTimeout bounds one reconnect attempt end to end.
	dialTimeout = 15 * time.Second
	// rpcTimeout bounds one Snapshot/Execute round trip.
	rpcTimeout = 10 * time.Second
	// maxBackoff caps the delay between reconnect attempts.
	maxBackoff = 30 * time.Second
	// staleAfter is how long the server's snapshot clock may stand still
	// before the title bar flags the feed as stale; staleLostAfter is when a
	// stalled feed is treated as a dead link and reconnected.
	staleAfter     = 5 * time.Second
	staleLostAfter = 30 * time.Second
	// statusTTL is how long a transient status line stays in the command bar.
	statusTTL = 4 * time.Second
)

// eventConsole marks events the console generates locally (connection
// changes, kick results) so they can be told apart from server events.
const eventConsole admin.EventType = "console"

var (
	errEventStreamClosed = errors.New("event stream closed")
	errSnapshotStalled   = errors.New("no snapshot from server for 30s")
)

// Options configures rendering and behavior.
type Options struct {
	ASCII     bool
	NoColor   bool
	ReadOnly  bool
	MaxEvents int
	// Refresh is the poll interval. If zero, defaults to 1 second.
	Refresh time.Duration
	// Version is shown in the title bar ("WFC v1.2.3").
	Version string
	// Dial opens a fresh connection to the daemon. When set, the console
	// reconnects on its own after the link drops; when nil it can only
	// report the loss.
	Dial func(ctx context.Context) (admin.AdminClient, error)

	// now is the clock; tests override it. Nil means time.Now.
	now func() time.Time
}

// Model is the WFC TUI model.
type Model struct {
	client   admin.AdminClient
	opts     Options
	snapshot *admin.SystemSnapshot
	events   []admin.Event
	tab      group // which view the lower box shows
	selected int   // row within the focused list (callers, or the lower tab)
	mode     viewMode
	prevMode viewMode // mode to return to after a confirm prompt
	width    int
	height   int
	// scrollBack is how many log lines the lower box is held back from
	// the newest entry (PgUp/PgDn); zero follows the tail.
	scrollBack int

	conn        connState
	connID      int // bumped whenever the current link is abandoned
	everLinked  bool
	lastErr     error
	attempt     int
	nextRetryAt time.Time
	// lastSnapAt is the local time a snapshot with a new server timestamp
	// last arrived; lastSnapServer is that server timestamp.
	lastSnapAt     time.Time
	lastSnapServer time.Time

	status      string
	statusErr   bool
	statusUntil time.Time
}

// New builds a Model. client may be nil, in which case the console starts
// disconnected and dials through opts.Dial (or, in tests, stays offline).
func New(client admin.AdminClient, opts Options) Model {
	if opts.MaxEvents <= 0 {
		opts.MaxEvents = 200
	}
	if opts.Refresh <= 0 {
		opts.Refresh = time.Second
	}
	if opts.now == nil {
		opts.now = time.Now
	}
	m := Model{client: client, opts: opts, mode: modeList, conn: connLost}
	if client != nil {
		m.conn = connConnected
		m.everLinked = true
		m.lastSnapAt = opts.now()
	}
	return m
}

func (m Model) now() time.Time { return m.opts.now() }

// Init starts the heartbeat and, when a client is present, the per-link
// commands (snapshot, event subscription, liveness watch).
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.tick()}
	if m.client != nil {
		cmds = append(cmds, m.linkCmds()...)
	}
	return tea.Batch(cmds...)
}

// tick schedules the next heartbeat.
func (m Model) tick() tea.Cmd {
	return tea.Tick(m.opts.Refresh, func(t time.Time) tea.Msg { return tickMsg{at: t} })
}

// linkCmds returns the commands that service the current client.
func (m Model) linkCmds() []tea.Cmd {
	return []tea.Cmd{m.readSnapshot(), m.subscribe(), m.watchDone()}
}

// readSnapshot fetches the latest snapshot once.
func (m Model) readSnapshot() tea.Cmd {
	c, id := m.client, m.connID
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		snap, err := c.Snapshot(ctx)
		return snapshotMsg{connID: id, snap: snap, err: err}
	}
}

// refreshNow asks the server to poll immediately, then reads the result.
// A refused refresh is not itself treated as a dead link; the Snapshot call
// that follows is what decides that.
func (m Model) refreshNow() tea.Cmd {
	c, id := m.client, m.connID
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		_, _ = c.Execute(ctx, admin.AdminCommand{Command: admin.CommandRefresh})
		snap, err := c.Snapshot(ctx)
		return snapshotMsg{connID: id, snap: snap, err: err}
	}
}

// subscribe opens the event stream once per link.
func (m Model) subscribe() tea.Cmd {
	c, id := m.client, m.connID
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		ch, err := c.Subscribe(context.Background())
		return subscribedMsg{connID: id, ch: ch, err: err}
	}
}

// waitForEvent reads one event from ch (subscribe-once, read-one pattern).
func waitForEvent(id int, ch <-chan admin.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		return eventMsg{connID: id, ch: ch, ev: ev, ok: ok}
	}
}

// watchDone blocks on the client's liveness channel, if it has one, so a
// dropped transport is noticed the moment it happens.
func (m Model) watchDone() tea.Cmd {
	lv, ok := m.client.(admin.Liveness)
	if !ok || lv.Done() == nil {
		return nil
	}
	id, done := m.connID, lv.Done()
	return func() tea.Msg {
		<-done
		return connLostMsg{connID: id, err: lv.Err()}
	}
}

// kick issues node.kick for n.
func (m Model) kick(n admin.NodeState) tea.Cmd {
	c, id := m.client, m.connID
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		// ConnectedAt pins the command to this session so a reused node
		// number cannot drop whoever connected next.
		res, err := c.Execute(ctx, admin.AdminCommand{Command: admin.CommandKick, NodeID: n.NodeID, ConnectedAt: n.ConnectedAt})
		return kickResultMsg{connID: id, nodeID: n.NodeID, handle: n.Handle, addr: n.RemoteAddr, res: res, err: err}
	}
}

// closeClient closes a client off the Update goroutine (Close may block on a
// network write).
func closeClient(c admin.AdminClient) tea.Cmd {
	if c == nil {
		return nil
	}
	return func() tea.Msg { _ = c.Close(); return nil }
}

// backoff returns the delay before reconnect attempt n (1-based):
// 1s, 2s, 4s, 8s, 16s, then 30s.
func backoff(n int) time.Duration {
	if n < 1 {
		n = 1
	}
	d := time.Second << uint(n-1)
	if n > 6 || d > maxBackoff {
		return maxBackoff
	}
	return d
}

// loseConnection abandons the current link: it bumps connID so in-flight
// results for the old link are ignored, schedules the next dial, and logs
// the loss to the event feed. It is idempotent while already offline.
func (m *Model) loseConnection(err error) tea.Cmd {
	if m.conn == connLost {
		return nil
	}
	old := m.client
	m.client = nil
	m.connID++
	m.conn = connLost
	m.lastErr = err
	m.attempt++
	m.nextRetryAt = m.now().Add(backoff(m.attempt))
	if m.mode == modeConfirmKick {
		m.mode = m.prevMode
	}
	m.pushLocalEvent("Connection lost: " + errText(err))
	return closeClient(old)
}

// startDial begins a reconnect attempt for the current connID.
func (m *Model) startDial() tea.Cmd {
	if m.opts.Dial == nil {
		return nil
	}
	m.conn = connConnecting
	id, dial := m.connID, m.opts.Dial
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
		defer cancel()
		c, err := dial(ctx)
		return dialResultMsg{connID: id, client: c, err: err}
	}
}

// pushLocalEvent appends a console-originated line to the event feed.
func (m *Model) pushLocalEvent(text string) {
	m.appendEvent(admin.Event{Time: m.now(), Type: eventConsole, Handle: "WFC", Message: text})
}

// appendServerEvent adds an event from the daemon unless the feed already
// holds an identical one. Subscribe replays the server's ring buffer on every
// new link, so after a reconnect the recent history arrives again; without
// this, each reconnect would duplicate the log. The whole bounded feed is
// scanned: console-originated lines carry the local clock, so timestamp order
// is not slice order and an early exit could miss the match.
func (m *Model) appendServerEvent(ev admin.Event) {
	for i := len(m.events) - 1; i >= 0; i-- {
		e := m.events[i]
		if e.Time.Equal(ev.Time) && e.Type == ev.Type && e.NodeID == ev.NodeID &&
			e.Handle == ev.Handle && e.Addr == ev.Addr && e.Message == ev.Message {
			return
		}
	}
	m.appendEvent(ev)
}

func (m *Model) appendEvent(ev admin.Event) {
	m.events = append(m.events, ev)
	if len(m.events) > m.opts.MaxEvents {
		m.events = m.events[len(m.events)-m.opts.MaxEvents:]
	}
}

// setStatus shows a transient line in the command bar.
func (m *Model) setStatus(text string, isErr bool) {
	m.status = text
	m.statusErr = isErr
	m.statusUntil = m.now().Add(statusTTL)
}

// callers are the nodes with someone logged in; bots the rest.
func (m Model) callers() []admin.NodeState { return m.nodesWhere(false) }
func (m Model) bots() []admin.NodeState    { return m.nodesWhere(true) }

func (m Model) nodesWhere(bot bool) []admin.NodeState {
	if m.snapshot == nil {
		return nil
	}
	var out []admin.NodeState
	for _, n := range m.snapshot.Nodes {
		if isBot(n) == bot {
			out = append(out, n)
		}
	}
	return out
}

// isBot: a connection with nobody authenticated. Port scanners and probes
// sit at the login prompt; grouping them apart keeps the caller list honest.
func isBot(n admin.NodeState) bool { return n.Status == admin.StatusLogin || n.Handle == "" }

func (m Model) scheduled() []admin.ScheduledEvent {
	if m.snapshot == nil {
		return nil
	}
	return m.snapshot.ScheduledEvents
}

// focusBottom reports whether the cursor lives in the lower box. The two
// logs have nothing to select, so while either is showing the cursor stays
// on the callers; opening the Events tab moves it there.
func (m Model) focusBottom() bool { return m.tab == groupEvents }

// tabNodes returns the node rows the cursor moves over (nil on the events tab).
func (m Model) tabNodes() []admin.NodeState {
	if m.focusBottom() {
		return nil
	}
	return m.callers()
}

// lowerRows is how many data rows the lower box has at the current size.
func (m Model) lowerRows() int {
	return layout(max(m.width, minWidth), max(m.height, minHeight), len(m.callers())).eventRows
}

// logLen is the length of the log the lower box is showing (0 on Events).
func (m Model) logLen() int {
	switch m.tab {
	case groupLog:
		return len(m.callerEvents())
	case groupBots:
		return len(m.botEvents())
	}
	return 0
}

// clampScroll keeps scrollBack within the log.
func (m *Model) clampScroll() {
	maxBack := m.logLen() - m.lowerRows()
	if maxBack < 0 {
		maxBack = 0
	}
	if m.scrollBack > maxBack {
		m.scrollBack = maxBack
	}
	if m.scrollBack < 0 {
		m.scrollBack = 0
	}
}

// hasSchema reports whether the daemon stamps snapshots with at least
// schema n, i.e. whether it reports the fields that version added.
func (m Model) hasSchema(n int) bool { return m.snapshot != nil && m.snapshot.Schema >= n }

// isBotEvent: an event about an anonymous connection. Console-originated
// lines belong with the callers' log.
func isBotEvent(ev admin.Event) bool { return ev.Handle == "" && ev.Type != eventConsole }

// callerEvents and botEvents split the feed between the two log tabs.
func (m Model) callerEvents() []admin.Event { return m.eventsWhere(false) }
func (m Model) botEvents() []admin.Event    { return m.eventsWhere(true) }

func (m Model) eventsWhere(bot bool) []admin.Event {
	var out []admin.Event
	for _, ev := range m.events {
		if isBotEvent(ev) == bot {
			out = append(out, ev)
		}
	}
	return out
}

// tabCount is how many rows the focused list has.
func (m Model) tabCount() int {
	if m.tab == groupEvents {
		return len(m.scheduled())
	}
	return len(m.tabNodes())
}

// clampSelection keeps the cursor on an existing row of the current tab.
func (m *Model) clampSelection() {
	n := m.tabCount()
	if m.selected >= n {
		m.selected = n - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
	if n == 0 && m.mode == modeDetails {
		m.mode = modeList
	}
}

// selectedNode returns the node under the cursor on the callers/bots tabs.
func (m Model) selectedNode() (admin.NodeState, bool) {
	nodes := m.tabNodes()
	if m.selected < 0 || m.selected >= len(nodes) {
		return admin.NodeState{}, false
	}
	return nodes[m.selected], true
}

// selectedEvent returns the scheduled event under the cursor on the events tab.
func (m Model) selectedEvent() (admin.ScheduledEvent, bool) {
	if m.tab != groupEvents {
		return admin.ScheduledEvent{}, false
	}
	evs := m.scheduled()
	if m.selected < 0 || m.selected >= len(evs) {
		return admin.ScheduledEvent{}, false
	}
	return evs[m.selected], true
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		now := msg.at
		var cmds []tea.Cmd
		switch m.conn {
		case connConnected:
			if !m.lastSnapAt.IsZero() && now.Sub(m.lastSnapAt) > staleLostAfter {
				cmds = append(cmds, m.loseConnection(errSnapshotStalled))
			} else {
				cmds = append(cmds, m.readSnapshot())
			}
		case connLost:
			if m.opts.Dial != nil && !now.Before(m.nextRetryAt) {
				cmds = append(cmds, m.startDial())
			}
		}
		if m.status != "" && !now.Before(m.statusUntil) {
			m.status = ""
		}
		cmds = append(cmds, m.tick())
		return m, tea.Batch(cmds...)

	case snapshotMsg:
		if msg.connID != m.connID {
			return m, nil
		}
		if msg.err != nil {
			return m, m.loseConnection(msg.err)
		}
		if msg.snap != nil {
			m.snapshot = msg.snap
			if !msg.snap.Time.Equal(m.lastSnapServer) {
				m.lastSnapServer = msg.snap.Time
				m.lastSnapAt = m.now()
			}
			m.clampSelection()
		}
		return m, nil

	case subscribedMsg:
		if msg.connID != m.connID {
			return m, nil
		}
		if msg.err != nil {
			return m, m.loseConnection(msg.err)
		}
		return m, waitForEvent(msg.connID, msg.ch)

	case eventMsg:
		if msg.connID != m.connID {
			return m, nil
		}
		if !msg.ok {
			return m, m.loseConnection(errEventStreamClosed)
		}
		m.appendServerEvent(msg.ev)
		return m, waitForEvent(msg.connID, msg.ch)

	case connLostMsg:
		if msg.connID != m.connID {
			return m, nil
		}
		err := msg.err
		if err == nil {
			err = errors.New("connection closed")
		}
		return m, m.loseConnection(err)

	case dialResultMsg:
		if msg.connID != m.connID {
			// A dial we gave up on (R pressed, or a later loss) still
			// produced a client; do not leak it.
			return m, closeClient(msg.client)
		}
		if msg.err != nil {
			m.conn = connLost
			m.lastErr = msg.err
			m.attempt++
			m.nextRetryAt = m.now().Add(backoff(m.attempt))
			return m, nil
		}
		m.client = msg.client
		m.conn = connConnected
		m.attempt = 0
		m.lastErr = nil
		m.lastSnapAt = m.now()
		if m.everLinked {
			m.pushLocalEvent("Reconnected")
		} else {
			m.pushLocalEvent("Connected")
		}
		m.everLinked = true
		return m, tea.Batch(m.linkCmds()...)

	case kickResultMsg:
		if msg.connID != m.connID {
			return m, nil // issued on a link that has since been replaced
		}
		// Name the target by handle, else by address (a bot), else by node.
		who := sanitizeTerminal(msg.handle)
		if who == "" {
			who = sanitizeTerminal(msg.addr)
		}
		if who == "" {
			who = fmt.Sprintf("node %d", msg.nodeID)
		} else {
			who = fmt.Sprintf("%s (node %d)", who, msg.nodeID)
		}
		if errors.Is(msg.err, context.DeadlineExceeded) {
			// The daemon may still complete the kick; the reply, if it
			// comes, is discarded by the transport as stale.
			m.setStatus("Kick of "+who+" timed out; outcome unknown", true)
			m.pushLocalEvent("Kick of " + who + " timed out; outcome unknown")
			return m, nil
		}
		if msg.err != nil {
			m.setStatus("Kick failed: "+errText(msg.err), true)
			m.pushLocalEvent(fmt.Sprintf("Kick of %s failed: %s", who, errText(msg.err)))
			return m, nil
		}
		m.setStatus("Kicked "+who, false)
		m.pushLocalEvent("Kicked " + who)
		return m, m.readSnapshot()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// errText renders an error for the operator, stripped of control characters.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return sanitizeTerminal(err.Error())
}
