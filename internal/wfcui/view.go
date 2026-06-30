package wfcui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// View renders the full-screen TUI based on the current mode.
func (m Model) View() string {
	switch m.mode {
	case modeTooSmall:
		return m.tooSmallView()
	case modeDisconnected:
		return m.disconnectedView()
	case modeDetails:
		return m.detailsView()
	default:
		return m.listView()
	}
}

// tooSmallView renders a minimal "terminal too small" message.
func (m Model) tooSmallView() string {
	msg := fmt.Sprintf("Terminal too small (need %dx%d, have %dx%d)",
		minWidth, minHeight, m.width, m.height)
	return msg + "\n"
}

// disconnectedView renders a full-screen disconnected banner.
func (m Model) disconnectedView() string {
	st := newStyles(m.opts)
	errMsg := ""
	if m.lastErr != nil {
		errMsg = m.lastErr.Error()
	}
	lines := []string{
		"",
		st.errorStyle.Render("Disconnected"),
		"",
	}
	if errMsg != "" {
		lines = append(lines, st.dimmed.Render(errMsg), "")
	}
	lines = append(lines, st.cmdBar.Render("[r] reconnect   [q] quit"))
	return strings.Join(lines, "\n")
}

// listView renders the main WFC node list, event feed, and command bar.
func (m Model) listView() string {
	st := newStyles(m.opts)
	b := borderSet(m.opts.ASCII)

	// Total available height: subtract fixed rows so rendered output fits m.height exactly.
	//
	// Layout rows:
	//   header  = 1
	//   cmdBar  = 1
	//   section separators from strings.Join("\n"): 2 when logs hidden (header + 1 box),
	//     3 when logs shown (header + 2 boxes).  Each "\n" between sections costs 1 row.
	//
	// innerH is what remains for the box content (including each box's own border rows).
	headerHeight := 1
	cmdBarHeight := 1

	separatorRows := 2 // logs hidden: join("\n") between header, nodeBox, cmdBar = 2 separators
	if m.showLogs {
		separatorRows = 3 // logs shown: +1 separator between nodeBox and eventBox
	}

	innerH := m.height - headerHeight - cmdBarHeight - separatorRows
	if innerH < 4 {
		innerH = 4
	}

	w := m.width
	if w < minWidth {
		w = minWidth
	}

	// --- Header ---
	headerLine := m.renderHeader(st)

	var sections []string
	sections = append(sections, headerLine)

	if m.showLogs {
		nodeTableH := innerH / 2
		eventFeedH := innerH - nodeTableH
		if eventFeedH < 2 {
			eventFeedH = 2
		}

		// --- Node table ---
		nodeLines := m.renderNodeTable(st, w-2, nodeTableH-2)
		nodeBox := lipgloss.NewStyle().
			Border(b).
			BorderForeground(lipgloss.Color("4")).
			Width(w - 2).
			Render(strings.Join(nodeLines, "\n"))
		sections = append(sections, nodeBox)

		// --- Event feed ---
		eventLines := m.renderEventFeed(st, w-2, eventFeedH-2)
		eventBox := lipgloss.NewStyle().
			Border(b).
			BorderForeground(lipgloss.Color("4")).
			Width(w - 2).
			Render(strings.Join(eventLines, "\n"))
		sections = append(sections, eventBox)
	} else {
		// Logs hidden: give the full inner height to the node table.
		nodeLines := m.renderNodeTable(st, w-2, innerH-2)
		nodeBox := lipgloss.NewStyle().
			Border(b).
			BorderForeground(lipgloss.Color("4")).
			Width(w - 2).
			Render(strings.Join(nodeLines, "\n"))
		sections = append(sections, nodeBox)
	}

	// --- Command bar ---
	cmdBar := m.renderCmdBar(st, w)
	sections = append(sections, cmdBar)

	return strings.Join(sections, "\n")
}

// renderHeader returns a single header line with system info.
func (m Model) renderHeader(st colorSet) string {
	sysName := "ViSiON/3 WFC"
	activeNodes := 0
	callsTodayStr := "—" // em dash: shown before any snapshot arrives
	uptime := ""

	if m.snapshot != nil {
		if m.snapshot.SystemName != "" {
			sysName = m.snapshot.SystemName
		}
		activeNodes = m.snapshot.Counters.ActiveNodes
		// CallsToday uses -1 as a sentinel meaning "unavailable".
		if m.snapshot.Counters.CallsToday < 0 {
			callsTodayStr = "—"
		} else {
			callsTodayStr = strconv.Itoa(m.snapshot.Counters.CallsToday)
		}
		uptime = formatUptime(m.snapshot.UptimeSecs)
	}

	now := time.Now().Format("15:04:05")
	header := fmt.Sprintf(" %s  |  Nodes: %d  |  Calls Today: %s  |  Uptime: %s  |  %s",
		sysName, activeNodes, callsTodayStr, uptime, now)
	return st.header.Render(header)
}

// renderNodeTable returns lines for the node table (inner, no border).
func (m Model) renderNodeTable(st colorSet, width, maxLines int) []string {
	if maxLines < 1 {
		maxLines = 1
	}

	colHandle := 16
	colStatus := 10
	colActivity := 20
	colMenu := 16
	colAddr := 18
	// titlebar
	titleFmt := fmt.Sprintf("%%-%ds %%-%ds %%-%ds %%-%ds %%-%ds",
		colHandle, colStatus, colActivity, colMenu, colAddr)
	title := fmt.Sprintf(titleFmt, "Handle", "Status", "Activity", "Menu", "Address")
	lines := []string{st.dimmed.Render(title)}

	if m.snapshot == nil || len(m.snapshot.Nodes) == 0 {
		lines = append(lines, st.dimmed.Render(" (no active nodes)"))
		return trimToMax(lines, maxLines)
	}

	for i, n := range m.snapshot.Nodes {
		if len(lines) >= maxLines {
			break
		}
		handle := truncate(n.Handle, colHandle)
		if handle == "" {
			handle = "(login)"
		}
		status := truncate(string(n.Status), colStatus)
		activity := truncate(n.Activity, colActivity)
		menu := truncate(n.CurrentMenu, colMenu)
		addr := truncate(n.RemoteAddr, colAddr)

		row := fmt.Sprintf(titleFmt, handle, status, activity, menu, addr)
		if i == m.selected {
			lines = append(lines, st.selected.Render(row))
		} else {
			lines = append(lines, row)
		}
	}
	_ = width // reserved for future column scaling
	return trimToMax(lines, maxLines)
}

// renderEventFeed returns lines for the event feed (inner, no border).
func (m Model) renderEventFeed(st colorSet, width, maxLines int) []string {
	if maxLines < 1 {
		maxLines = 1
	}
	lines := []string{st.dimmed.Render("Events:")}

	// Show most recent events, newest last, trimmed to maxLines-1 (leave room for header).
	available := maxLines - 1
	if available < 1 {
		available = 1
	}
	start := 0
	if len(m.events) > available {
		start = len(m.events) - available
	}
	for _, ev := range m.events[start:] {
		ts := ev.Time.Format("15:04:05")
		// Truncate the handle+message tail to prevent long lines from wrapping and
		// consuming extra rows beyond maxLines.  tsLen accounts for the timestamp
		// prefix plus the two two-space separators ("  " each).
		const tsLen = 8 + 2 // "HH:MM:SS" + two-space separator
		tail := ev.Handle + "  " + ev.Message
		tailRunes := []rune(tail)
		maxTail := width - tsLen
		if maxTail < 0 {
			maxTail = 0
		}
		if len(tailRunes) > maxTail {
			tailRunes = tailRunes[:maxTail]
		}
		line := st.eventTime.Render(ts) + "  " + string(tailRunes)
		lines = append(lines, line)
	}
	return trimToMax(lines, maxLines)
}

// renderCmdBar returns the command bar line.
func (m Model) renderCmdBar(st colorSet, width int) string {
	var keys string
	switch m.mode {
	case modeDetails:
		keys = "[ESC] back   [q] quit"
	default:
		keys = "[↑/↓] select   [ENTER] details   [L] logs   [r] refresh   [q] quit"
	}
	bar := st.cmdBar.Render(keys)
	_ = width // reserved for padding
	return bar
}

// formatUptime converts seconds to "Xd Xh Xm" string.
func formatUptime(secs int64) string {
	if secs <= 0 {
		return "0m"
	}
	d := secs / 86400
	secs %= 86400
	h := secs / 3600
	secs %= 3600
	mn := secs / 60
	switch {
	case d > 0:
		return fmt.Sprintf("%dd %dh %dm", d, h, mn)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, mn)
	default:
		return fmt.Sprintf("%dm", mn)
	}
}

// truncate shortens s to at most n runes, padding with spaces to exactly n.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) > n {
		return string(runes[:n])
	}
	return s + strings.Repeat(" ", n-len(runes))
}

// trimToMax returns at most max lines from lines.
func trimToMax(lines []string, max int) []string {
	if len(lines) > max {
		return lines[:max]
	}
	return lines
}
