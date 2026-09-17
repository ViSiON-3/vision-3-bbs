package wfcui

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
)

// Screen geometry. The frame is the mockup's: a two-column blue gutter down
// each side, a one-column black margin, and the boxes filling what remains.
const (
	gutterW = 2
	marginW = 1
	boxX    = gutterW + marginW // first column of a box border
	chromeW = 2 * (gutterW + marginW)

	titleY = 0
	statsY = 2
	boxesY = 3 // first row of the top box
	// Every box spends four rows on chrome: top border, tab, column
	// header, bottom border.
	boxChrome = 4
	// fixedRows are the rows outside the boxes: title, blank, stats, cmd bar.
	fixedRows = 4
	// minLogRows is the least the event log keeps when the top box is busy.
	minLogRows = 3
)

// geometry is the resolved layout for one frame.
type geometry struct {
	w, h      int
	boxW      int // border to border
	innerX    int
	innerW    int
	topY      int // top box border row
	topRows   int // data rows inside the top box
	topHeader int // row of the top box column header
	eventY    int // lower box top border row
	eventRows int // data rows inside the lower box
	cmdY      int
}

// layout splits the terminal between the caller box and the lower (tabbed)
// box. The caller box takes exactly the rows it needs for the callers online
// (at least one, for the placeholder) and the lower box flexes to fill the
// rest, never dropping below minLogRows.
func layout(w, h int, wantRows int) geometry {
	g := geometry{w: w, h: h, boxW: w - chromeW, innerX: boxX + 1, innerW: w - chromeW - 2}
	g.topY = boxesY
	g.cmdY = h - 1
	free := h - fixedRows
	if wantRows < 1 {
		wantRows = 1
	}
	content := free - 2*boxChrome
	if content < minLogRows+1 {
		content = minLogRows + 1
	}
	g.topRows = min(wantRows, content-minLogRows)
	g.eventRows = content - g.topRows
	g.eventY = g.topY + g.topRows + boxChrome
	g.topHeader = g.topY + 2
	return g
}

// nodeColumns are the caller/bot table column widths for an inner width.
// The mockup's 72-column layout is the floor; extra width goes to the
// handle, address and (mostly) activity columns.
type nodeColumns struct {
	handle, activity, on, addr, node int
}

func nodeColumnsFor(innerW int) nodeColumns {
	// 16+27+6+18+5 = 72. The address column shows the host only (see
	// hostOnly), so 18 fits any IPv4 address with room to spare.
	c := nodeColumns{handle: 16, on: 6, addr: 18, node: 5}
	extra := innerW - 72
	if extra > 0 {
		c.handle += extra / 4
		c.addr += extra / 4
	}
	c.activity = innerW - c.handle - c.on - c.addr - c.node
	if c.activity < 8 {
		c.activity = 8
	}
	return c
}

// eventColumns are the scheduled-events table column widths.
type eventColumns struct {
	name, schedule, next, last, status int
}

func eventColumnsFor(innerW int) eventColumns {
	c := eventColumns{name: 20, schedule: 15, next: 13, last: 13}
	if extra := innerW - 72; extra > 0 {
		c.name += extra / 3
	}
	c.status = innerW - c.name - c.schedule - c.next - c.last
	if c.status < 8 {
		c.status = 8
	}
	return c
}

// logColumns are the callers' log column widths.
type logColumns struct {
	time, user, text int
}

func logColumnsFor(innerW int) logColumns {
	c := logColumns{time: 10, user: 14}
	if extra := innerW - 72; extra > 0 {
		c.user += extra / 4
	}
	c.text = innerW - c.time - c.user
	if c.text < 8 {
		c.text = 8
	}
	return c
}

// botColumns are the bot log column widths.
type botColumns struct {
	time, addr, text, node int
}

func botColumnsFor(innerW int) botColumns {
	c := botColumns{time: 10, addr: 18, node: 5}
	if extra := innerW - 72; extra > 0 {
		c.addr += extra / 4
	}
	c.text = innerW - c.time - c.addr - c.node
	if c.text < 8 {
		c.text = 8
	}
	return c
}

// hostOnly strips the port from a remote address: the console lists who is
// connected, and the ephemeral port only widens the column. The full address
// stays in the details overlay.
func hostOnly(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// View renders the full-screen TUI.
func (m Model) View() string {
	st := newStyler(m.opts)
	w, h := m.width, m.height
	if w <= 0 || h <= 0 {
		return ""
	}
	if w < minWidth || h < minHeight {
		s := newScreen(w, h)
		s.text(0, 0, fmt.Sprintf("Terminal too small (need %dx%d, have %dx%d)", minWidth, minHeight, w, h), cLightGray, cBlack, w)
		return s.render(st)
	}

	g := layout(w, h, len(m.callers()))
	s := newScreen(w, h)

	m.drawFrame(s, g)
	m.drawTitle(s, g)
	m.drawStats(s, g)
	m.drawCallerBox(s, g)
	m.drawLowerBox(s, g)
	if m.mode == modeDetails {
		m.drawDetails(s, g)
	}
	m.drawCmdBar(s, g)
	return s.render(st)
}

// drawFrame paints the gutters and their half-block ornaments.
func (m Model) drawFrame(s *screen, g geometry) {
	s.fill(0, 0, gutterW, g.h, ' ', cBlue, cBlue)
	s.fill(g.w-gutterW, 0, gutterW, g.h, ' ', cBlue, cBlue)
	if m.opts.NoColor || m.opts.ASCII {
		return
	}
	// Ornaments from the mockup, positioned by their fraction of a 25-row
	// screen so a taller terminal keeps the same silhouette.
	type dot struct {
		x, y int // x relative to the gutter's own left edge; y on a 25-row screen
		ch   rune
		bg   uint8
	}
	scale := func(y int) int { return 1 + y*(g.h-3)/22 } // keep clear of title/cmd rows
	left := []dot{{1, 3, gLower, cBlack}, {0, 12, gLower, cBlack}, {1, 12, gFull, cBlack}, {1, 14, ' ', cBlack}, {0, 20, gUpper, cBlack}, {1, 21, gLower, cBlack}}
	right := []dot{{0, 5, gUpper, cBlack}, {1, 5, gLower, cBlack}, {0, 13, gLower, cBlack}, {1, 21, gUpper, cBlack}}
	for _, d := range left {
		s.set(d.x, scale(d.y), d.ch, cBlue, d.bg)
	}
	for _, d := range right {
		s.set(g.w-gutterW+d.x, scale(d.y), d.ch, cBlue, d.bg)
	}
}

// drawTitle paints the title bar: system name centred, link state at right.
func (m Model) drawTitle(s *screen, g geometry) {
	s.fill(0, titleY, g.w, 1, ' ', cWhite, cBlue)
	name := "ViSiON/3 WFC"
	if m.snapshot != nil && m.snapshot.SystemName != "" {
		name = sanitizeTerminal(m.snapshot.SystemName)
	}
	s.textCenter(0, titleY, g.w, name, cWhite, cBlue)

	if m.snapshot != nil && len(m.snapshot.PendingReloads) > 0 {
		// A structural config change is queued for the next idle window;
		// the sysop should know saves are pending rather than silently held.
		note := "RELOAD PENDING: " + sanitizeTerminal(strings.Join(m.snapshot.PendingReloads, ", "))
		s.text(boxX, titleY, note, cYellow, cBlue, g.w/2-runeCount(name)/2-1)
	}

	// The state segment must not run into the centred name; it gets the
	// room to the right of it and picks the longest wording that fits.
	nameEnd := (g.w+runeCount(name))/2 + 2
	right, fg := m.linkStatus(g.w - marginW - nameEnd)
	s.textRight(0, titleY, g.w-marginW, right, fg, cBlue)
}

// fitFirst returns the first candidate no wider than room, or "" when none
// fits: the title-bar state is omitted rather than drawn over the board name.
func fitFirst(room int, candidates ...string) string {
	for _, c := range candidates {
		if runeCount(c) <= room {
			return c
		}
	}
	return ""
}

// linkStatus is the title-bar text and colour for the connection state.
// Every state offers progressively shorter wordings so it fits within room
// cells beside the centred board name, or disappears if even the shortest
// would not.
func (m Model) linkStatus(room int) (string, uint8) {
	now := m.now()
	switch m.conn {
	case connConnecting:
		return fitFirst(room, "connecting...", "conn..."), cYellow
	case connLost:
		if m.opts.Dial == nil {
			return fitFirst(room, "OFFLINE", "OFF"), cLightRed
		}
		wait := m.nextRetryAt.Sub(now).Round(time.Second)
		if wait < 0 {
			wait = 0
		}
		secs := int(wait / time.Second)
		return fitFirst(room,
			fmt.Sprintf("OFFLINE - retry in %ds", secs),
			fmt.Sprintf("OFFLINE %ds", secs),
			"OFFLINE", "OFF",
		), cLightRed
	}
	if !m.lastSnapAt.IsZero() {
		if age := now.Sub(m.lastSnapAt); age > staleAfter {
			return fitFirst(room, fmt.Sprintf("stale %ds", int(age/time.Second)), "stale"), cYellow
		}
	}
	v := m.opts.Version
	if v == "" {
		v = "dev"
	}
	if v[0] >= '0' && v[0] <= '9' {
		v = "v" + v
	}
	if m.drops > 0 {
		// A flapping link (a sleeping laptop, a lossy hop) shows up here as
		// a count rather than as a log line per event.
		noun := "drops"
		if m.drops == 1 {
			noun = "drop"
		}
		tally := fmt.Sprintf("%d %s", m.drops, noun)
		last := m.lastDropAt.Format("15:04")
		return fitFirst(room,
			fmt.Sprintf("WFC %s - %s, last %s", v, tally, last),
			fmt.Sprintf("WFC %s - %s", v, tally),
			tally+", last "+last,
			tally,
		), cLightBlue
	}
	return fitFirst(room, "WFC "+v, v), cLightBlue
}

// drawStats paints the counter pills, centred: " Label: value " on blue with
// one black column between pills. Full labels are used when they fit the
// box width, otherwise the short set, so an 80-column terminal still shows
// every counter.
func (m Model) drawStats(s *screen, g geometry) {
	values := []string{"-", "-", "-", "-", "-"}
	if m.snapshot != nil {
		c := m.snapshot.Counters
		values = []string{"-", "-", "-", counterText(c.CallsToday), formatUptime(m.snapshot.UptimeSecs)}
		// Schema 2 daemons report these; an older one would decode as zero.
		if m.hasSchema(2) {
			values[0] = counterText(c.TotalUsers)
			values[1] = counterText(c.NewUsers)
			values[2] = counterText(c.MailWaiting)
		}
	}
	labelSets := [][]string{
		{"Total Users", "New Users", "Mail Waiting", "Calls Today", "Uptime"},
		{"Users", "New", "Mail", "Calls", "Uptime"},
	}
	type pill struct{ label, value string }
	var pills []pill
	total := 0
	for _, labels := range labelSets {
		pills = pills[:0]
		total = -1 // no separator after the last pill
		for i, l := range labels {
			pills = append(pills, pill{l, values[i]})
			total += runeCount(l) + runeCount(values[i]) + 5 // " L: " + "V " + separator
		}
		if total <= g.boxW {
			break
		}
	}
	x := boxX
	if total < g.boxW {
		x += (g.boxW - total) / 2
	}
	limit := boxX + g.boxW
	for _, p := range pills {
		if x >= limit {
			break
		}
		x = s.text(x, statsY, " "+p.label+": ", cWhite, cBlue, limit)
		x = s.text(x, statsY, p.value+" ", cYellow, cBlue, limit)
		x++ // black separator
	}
}

func counterText(n int) string {
	if n < 0 {
		return "-"
	}
	return strconv.Itoa(n)
}

// drawCallerBox paints the "Online Now" table of logged-in callers.
func (m Model) drawCallerBox(s *screen, g geometry) {
	online := m.conn == connConnected
	boxH := g.topRows + boxChrome
	s.box(boxX, g.topY, g.boxW, boxH, boxColors{dim: cMagenta, bright: cLightMagenta})
	tabBg := cMagenta
	if !online {
		tabBg = cRed
	}
	s.tab(g.innerX, g.topY+1, g.innerW, "Online Now", cMagenta, cWhite, tabBg)
	m.drawNodeTable(s, g, g.topHeader, g.topRows, m.callers(), !m.focusBottom(), "...waiting...")
}

// drawLowerBox paints the tabbed box: the callers' log, the bot log, or the
// scheduled events.
func (m Model) drawLowerBox(s *screen, g geometry) {
	boxH := g.eventRows + boxChrome
	s.box(boxX, g.eventY, g.boxW, boxH, boxColors{dim: cCyan, bright: cCyan})
	m.drawTabs(s, g.innerX, g.eventY+1, g.innerW)
	header := g.eventY + 2
	switch m.tab {
	case groupBots:
		m.drawBotLog(s, g, header, g.eventRows)
	case groupEvents:
		m.drawEventsTable(s, g, header, g.eventRows)
	default:
		m.drawEventLog(s, g, header, g.eventRows)
	}
}

// drawTabs paints "▄▀▄ Callers  Bots 1  Events 3 ▄▀▄" with the active tab
// lit (white on magenta) and the others dimmed. The Bots count is the number
// of anonymous connections open right now.
func (m Model) drawTabs(s *screen, x, y, w int) {
	labels := []string{
		" Callers ",
		fmt.Sprintf(" Bots %d ", len(m.bots())),
		fmt.Sprintf(" Events %d ", len(m.scheduled())),
	}
	total := 6 // the two wings
	for _, l := range labels {
		total += runeCount(l)
	}
	if total > w {
		total = w
	}
	limit := x + w
	xx := x + (w-total)/2
	wings := string([]rune{gLower, gUpper, gLower})
	xx = s.text(xx, y, wings, cMagenta, cBlack, limit)
	for i, l := range labels {
		fg, bg := cDarkGray, cBlack
		if group(i) == m.tab {
			fg, bg = cWhite, cMagenta
		}
		xx = s.text(xx, y, l, fg, bg, limit)
	}
	s.text(xx, y, wings, cMagenta, cBlack, limit)
}

// drawNodeTable paints caller or bot rows under a column header at headerY,
// with rows data rows below it. focused decides whether the cursor row is
// highlighted; empty is the placeholder when there are no rows.
func (m Model) drawNodeTable(s *screen, g geometry, headerY, rows int, nodes []admin.NodeState, focused bool, empty string) {
	online := m.conn == connConnected
	cols := nodeColumnsFor(g.innerW)
	x := g.innerX
	hdr := func(label string, w int) {
		s.textPad(x, headerY, w, label, cDarkGray, cBlack)
		x += w
	}
	hdr("Handle", cols.handle)
	hdr("Activity", cols.activity)
	hdr("On", cols.on)
	hdr("Address", cols.addr)
	hdr("N#", cols.node)

	if len(nodes) == 0 {
		if m.snapshot == nil {
			empty = "(waiting for first snapshot)"
		}
		s.textCenter(g.innerX, headerY+1, g.innerW, empty, cDarkGray, cBlack)
		return
	}
	first := 0
	if focused && m.selected >= rows {
		first = m.selected - rows + 1
	}
	now := m.now()
	for row := 0; row < rows && first+row < len(nodes); row++ {
		y := headerY + 1 + row
		i := first + row
		n := nodes[i]
		fg, bg := cWhite, cBlack
		switch {
		case !online:
			fg = cDarkGray
		case focused && i == m.selected:
			bg = cCyan
		}
		handle := sanitizeTerminal(n.Handle)
		hfg := fg
		if handle == "" {
			handle = "(login)"
			if fg == cWhite {
				hfg = cLightGray
			}
		}
		x = g.innerX
		s.textPad(x, y, cols.handle, handle, hfg, bg)
		x += cols.handle
		s.textPad(x, y, cols.activity, activityText(n), fg, bg)
		x += cols.activity
		s.textPad(x, y, cols.on, formatOnline(now.Sub(n.ConnectedAt)), fg, bg)
		x += cols.on
		s.textPad(x, y, cols.addr, sanitizeTerminal(hostOnly(n.RemoteAddr)), fg, bg)
		x += cols.addr
		s.textPad(x, y, cols.node, strconv.Itoa(n.NodeID), fg, bg)
	}
}

// activityText is the caller's reported activity, falling back to the
// current menu so a row is never blank while someone is on.
func activityText(n admin.NodeState) string {
	if a := strings.TrimSpace(n.Activity); a != "" {
		return sanitizeTerminal(a)
	}
	if n.Status == admin.StatusLogin {
		return "At login prompt"
	}
	if n.CurrentMenu != "" {
		return "Menu: " + sanitizeTerminal(n.CurrentMenu)
	}
	return string(n.Status)
}

// drawEventsTable paints the scheduler's entries.
func (m Model) drawEventsTable(s *screen, g geometry, headerY, rows int) {
	cols := eventColumnsFor(g.innerW)
	x := g.innerX
	hdr := func(label string, w int) {
		s.textPad(x, headerY, w, label, cDarkGray, cBlack)
		x += w
	}
	hdr("Event", cols.name)
	hdr("Schedule", cols.schedule)
	hdr("Next Run", cols.next)
	hdr("Last Run", cols.last)
	hdr("Status", cols.status)

	evs := m.scheduled()
	if len(evs) == 0 {
		msg := "(no scheduled events)"
		switch {
		case m.snapshot == nil:
			msg = "(waiting for first snapshot)"
		case !m.hasSchema(2):
			msg = "(this BBS daemon predates the Events tab - update the BBS to see them)"
		}
		s.textCenter(g.innerX, headerY+1, g.innerW, msg, cDarkGray, cBlack)
		return
	}
	first := 0
	if m.selected >= rows {
		first = m.selected - rows + 1
	}
	now := m.now()
	for row := 0; row < rows && first+row < len(evs); row++ {
		y := headerY + 1 + row
		i := first + row
		ev := evs[i]
		fg, bg := cWhite, cBlack
		if !ev.Enabled {
			fg = cDarkGray
		}
		if i == m.selected {
			bg = cCyan
			fg = cWhite
		}
		status, sfg := eventStatusText(ev)
		if bg == cCyan {
			sfg = cWhite
		}
		x = g.innerX
		s.textPad(x, y, cols.name, sanitizeTerminal(eventName(ev)), fg, bg)
		x += cols.name
		s.textPad(x, y, cols.schedule, sanitizeTerminal(scheduleText(ev)), fg, bg)
		x += cols.schedule
		s.textPad(x, y, cols.next, formatWhen(ev.NextRun, now), fg, bg)
		x += cols.next
		s.textPad(x, y, cols.last, formatWhen(ev.LastRun, now), fg, bg)
		x += cols.last
		s.textPad(x, y, cols.status, status, sfg, bg)
	}
}

func eventName(ev admin.ScheduledEvent) string {
	if strings.TrimSpace(ev.Name) != "" {
		return ev.Name
	}
	return ev.ID
}

func scheduleText(ev admin.ScheduledEvent) string {
	switch {
	case ev.Schedule != "" && ev.RunAtStartup:
		return ev.Schedule + " +boot"
	case ev.Schedule != "":
		return ev.Schedule
	case ev.RunAtStartup:
		return "at startup"
	}
	return "-"
}

// eventStatusText summarises an event's state and picks its colour.
func eventStatusText(ev admin.ScheduledEvent) (string, uint8) {
	switch {
	case ev.Running:
		return "running", cYellow
	case !ev.Enabled:
		return "disabled", cDarkGray
	case ev.LastStatus == "":
		return "never run", cLightGray
	}
	dur := ""
	if ev.LastDurationMs > 0 {
		dur = " " + (time.Duration(ev.LastDurationMs) * time.Millisecond).Round(100*time.Millisecond).String()
	}
	switch ev.LastStatus {
	case "success":
		return "ok" + dur, cLightGreen
	case "failure":
		return "failed" + dur, cLightRed
	case "timeout":
		return "timed out" + dur, cLightRed
	}
	return sanitizeTerminal(ev.LastStatus) + dur, cLightGray
}

// formatWhen renders a timestamp relative to today: "15:04" today, "Tue
// 15:04" within the week, "Jan 02 15:04" beyond, "-" when zero.
func formatWhen(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	t = t.In(now.Location())
	ny, nm, nd := now.Date()
	ty, tm, td := t.Date()
	switch {
	case ny == ty && nm == tm && nd == td:
		return t.Format("15:04")
	case t.Sub(now).Abs() < 6*24*time.Hour:
		return t.Format("Mon 15:04")
	}
	return t.Format("Jan 02 15:04")
}

// drawEventLog paints the callers' activity log, newest entry last.
func (m Model) drawEventLog(s *screen, g geometry, headerY, rows int) {
	cols := logColumnsFor(g.innerW)
	s.textPad(g.innerX, headerY, cols.time, "Time", cDarkGray, cBlack)
	s.textPad(g.innerX+cols.time, headerY, cols.user, "User", cDarkGray, cBlack)
	s.textPad(g.innerX+cols.time+cols.user, headerY, cols.text, "Activity", cDarkGray, cBlack)

	events, newer := m.logWindow(m.callerEvents(), rows)
	m.drawScrollMark(s, g, headerY, newer)
	for i, ev := range events {
		y := headerY + 1 + i
		fg := cLightGray
		switch ev.Type {
		case eventConsole:
			fg = cLightCyan
		case admin.EventNodeKicked:
			fg = cLightRed
		}
		who := sanitizeTerminal(ev.Handle)
		if who == "" {
			who = fmt.Sprintf("node %d", ev.NodeID)
		}
		s.textPad(g.innerX, y, cols.time, ev.Time.Local().Format("15:04:05"), fg, cBlack)
		s.textPad(g.innerX+cols.time, y, cols.user, who, fg, cBlack)
		s.textPad(g.innerX+cols.time+cols.user, y, cols.text, eventText(ev), fg, cBlack)
	}
}

// drawBotLog paints the anonymous-connection log: scanners, probes, and
// callers who have not logged in yet, named by address.
func (m Model) drawBotLog(s *screen, g geometry, headerY, rows int) {
	cols := botColumnsFor(g.innerW)
	x := g.innerX
	hdr := func(label string, w int) {
		s.textPad(x, headerY, w, label, cDarkGray, cBlack)
		x += w
	}
	hdr("Time", cols.time)
	hdr("Address", cols.addr)
	hdr("Event", cols.text)
	hdr("N#", cols.node)

	if len(m.botEvents()) == 0 {
		s.textCenter(g.innerX, headerY+1, g.innerW, "(no bot activity yet)", cDarkGray, cBlack)
		return
	}
	events, newer := m.logWindow(m.botEvents(), rows)
	m.drawScrollMark(s, g, headerY, newer)
	for i, ev := range events {
		y := headerY + 1 + i
		fg := cLightGray
		switch ev.Type {
		case admin.EventCallerDisconnected:
			fg = cDarkGray
		case admin.EventNodeKicked:
			fg = cLightRed
		}
		addr := sanitizeTerminal(hostOnly(ev.Addr))
		if addr == "" {
			addr = fmt.Sprintf("node %d", ev.NodeID) // older daemons send no address
		}
		x = g.innerX
		s.textPad(x, y, cols.time, ev.Time.Local().Format("15:04:05"), fg, cBlack)
		x += cols.time
		s.textPad(x, y, cols.addr, addr, fg, cBlack)
		x += cols.addr
		s.textPad(x, y, cols.text, eventText(ev), fg, cBlack)
		x += cols.text
		s.textPad(x, y, cols.node, strconv.Itoa(ev.NodeID), fg, cBlack)
	}
}

// logWindow returns the rows-long slice of events the lower box shows,
// honouring scrollBack, and how many newer entries lie below the window.
func (m Model) logWindow(events []admin.Event, rows int) ([]admin.Event, int) {
	back := m.scrollBack
	if maxBack := len(events) - rows; back > maxBack {
		back = max(0, maxBack)
	}
	end := len(events) - back
	start := end - rows
	if start < 0 {
		start = 0
	}
	return events[start:end], back
}

// drawScrollMark flags a scrolled-back log at the right of its header row.
func (m Model) drawScrollMark(s *screen, g geometry, headerY, newer int) {
	if newer <= 0 {
		return
	}
	s.textRight(g.innerX, headerY, g.innerW, fmt.Sprintf("%d newer - PgDn", newer), cYellow, cBlack)
}

// eventText turns a diff event into a sentence for the log.
func eventText(ev admin.Event) string {
	msg := sanitizeTerminal(ev.Message)
	switch ev.Type {
	case admin.EventCallerConnected:
		return "Connected"
	case admin.EventCallerLoggedIn:
		return "Logged on"
	case admin.EventCallerDisconnected:
		return "Disconnected"
	case admin.EventMenuChanged:
		if msg == "" {
			return "Left menu"
		}
		return "Menu: " + msg
	case admin.EventActivityChanged:
		if msg == "" {
			return "Idle"
		}
		return msg
	case admin.EventNodeKicked:
		return "Kicked by sysop"
	}
	return msg
}

// segment is one run of footer text in a single colour.
type segment struct {
	text string
	fg   uint8
}

// keySeg returns the segments for "[K] label ".
func keySeg(k, label string) []segment {
	return []segment{{"[", cCyan}, {k, cYellow}, {"] " + label + " ", cCyan}}
}

// drawSegments writes segs centred on row y over bg.
func drawSegments(s *screen, y int, segs []segment, bg uint8) {
	total := 0
	for _, sg := range segs {
		total += runeCount(sg.text)
	}
	x := 0
	if total < s.w {
		x = (s.w - total) / 2
	}
	for _, sg := range segs {
		x = s.text(x, y, sg.text, sg.fg, bg, s.w)
	}
}

// drawCmdBar paints the bottom key bar, a transient status line, or the
// kick confirmation prompt, each centred.
func (m Model) drawCmdBar(s *screen, g geometry) {
	y := g.cmdY
	if m.mode == modeConfirmKick {
		s.fill(0, y, g.w, 1, ' ', cWhite, cRed)
		n := m.kickTarget
		who := sanitizeTerminal(n.Handle)
		if who == "" {
			who = sanitizeTerminal(n.RemoteAddr)
		}
		segs := []segment{{fmt.Sprintf("Disconnect %s on node %d? ", who, n.NodeID), cWhite}}
		segs = append(segs, keySeg("Y", "yes")...)
		segs = append(segs, keySeg("N", "no")...)
		drawSegments(s, y, segs, cRed)
		return
	}
	s.fill(0, y, g.w, 1, ' ', cCyan, cBlue)
	if m.status != "" {
		fg := cWhite
		if m.statusErr {
			fg = cLightRed
		}
		drawSegments(s, y, []segment{{m.status, fg}}, cBlue)
		return
	}
	canKick := !m.opts.ReadOnly && !m.focusBottom()
	var segs []segment
	switch {
	case m.mode == modeDetails:
		segs = append(segs, keySeg("ESC", "back")...)
		if canKick {
			segs = append(segs, keySeg("K", "kick")...)
		}
		segs = append(segs, keySeg("Q", "quit")...)
	case m.conn != connConnected:
		if m.opts.Dial != nil {
			segs = append(segs, keySeg("R", "retry now")...)
		}
		segs = append(segs, keySeg("TAB", "view")...)
		segs = append(segs, keySeg("PgUp/PgDn", "scroll")...)
		segs = append(segs, keySeg("Q", "quit")...)
	default:
		// R (refresh now) still works but is left off the bar: the screen
		// refreshes every second on its own and the row is full at 80 cols.
		segs = append(segs,
			segment{"[", cCyan}, segment{string(gUp), cYellow}, segment{"/", cWhite},
			segment{string(gDown), cYellow}, segment{"] select ", cCyan})
		segs = append(segs, keySeg("TAB", "view")...)
		segs = append(segs, keySeg("PgUp/PgDn", "scroll")...)
		segs = append(segs, keySeg("ENTER", "details")...)
		segs = append(segs, keySeg("Q", "quit")...)
		if canKick {
			segs = append(segs, keySeg("K", "kick")...)
		}
	}
	// Drop the trailing space so the block centres on its visible text.
	if n := len(segs); n > 0 {
		segs[n-1].text = strings.TrimRight(segs[n-1].text, " ")
	}
	drawSegments(s, y, segs, cBlue)
}

// formatUptime renders seconds as HH:MM:SS, prefixed with days past 24h.
func formatUptime(secs int64) string {
	if secs < 0 {
		secs = 0
	}
	d := secs / 86400
	secs %= 86400
	h := secs / 3600
	secs %= 3600
	mn := secs / 60
	sec := secs % 60
	if d > 0 {
		return fmt.Sprintf("%dd %02d:%02d:%02d", d, h, mn, sec)
	}
	return fmt.Sprintf("%02d:%02d:%02d", h, mn, sec)
}

// formatOnline renders time online compactly: <1m, 4m, 1h10m, 2d3h.
func formatOnline(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	mins := int(d / time.Minute)
	switch {
	case mins < 60:
		return fmt.Sprintf("%dm", mins)
	case mins < 24*60:
		return fmt.Sprintf("%dh%02dm", mins/60, mins%60)
	default:
		return fmt.Sprintf("%dd%dh", mins/(24*60), (mins%(24*60))/60)
	}
}
