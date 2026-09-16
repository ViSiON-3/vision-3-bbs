package wfcui

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type detailRow struct{ label, value string }

// drawDetails paints the details overlay for the selected row: a caller or
// bot on the node tabs, a scheduled event on the events tab.
func (m Model) drawDetails(s *screen, g geometry) {
	var title string
	var rows []detailRow
	now := m.now()
	if ev, ok := m.selectedEvent(); ok {
		title = "Event " + eventName(ev)
		status, _ := eventStatusText(ev)
		rows = []detailRow{
			{"ID", ev.ID},
			{"Name", labelOrFallback(ev.Name, "(none)")},
			{"Schedule", scheduleText(ev)},
			{"Enabled", yesNo(ev.Enabled)},
			{"Status", status},
			{"Next Run", formatTimestamp(ev.NextRun)},
			{"Last Run", formatTimestamp(ev.LastRun)},
			{"Last Result", labelOrFallback(ev.LastStatus, "(never run)")},
			{"Duration", formatMillis(ev.LastDurationMs)},
			{"Runs", strconv.Itoa(ev.RunCount)},
			{"Failures", strconv.Itoa(ev.FailureCount)},
		}
	} else if n, ok := m.selectedNode(); ok {
		title = fmt.Sprintf("Node %d", n.NodeID)
		rows = []detailRow{
			{"Handle", labelOrFallback(n.Handle, "(not logged in)")},
			{"Status", string(n.Status)},
			{"User ID", strconv.Itoa(n.UserID)},
			{"Access Level", strconv.Itoa(n.AccessLevel)},
			{"Address", labelOrFallback(n.RemoteAddr, "(unknown)")},
			{"Menu", labelOrFallback(n.CurrentMenu, "(none)")},
			{"Activity", labelOrFallback(n.Activity, "(none)")},
			{"Connected", formatTimestamp(n.ConnectedAt)},
			{"Online For", formatOnline(now.Sub(n.ConnectedAt))},
			{"Last Activity", formatTimestamp(n.LastActivity)},
			{"Time Left", formatTimeLeft(n.TimeLeftMins)},
		}
		if n.Invisible {
			rows = append(rows, detailRow{"Invisible", "yes"})
		}
	} else {
		return
	}

	const labelW = 14
	boxW := min(60, g.boxW)
	boxH := len(rows) + 4
	x := (g.w - boxW) / 2
	y := (g.h - boxH) / 2
	if y < 1 {
		y = 1
	}
	s.fill(x, y, boxW, boxH, ' ', cLightGray, cBlack)
	s.box(x, y, boxW, boxH, boxColors{dim: cLightMagenta, bright: cLightMagenta})
	s.tab(x+1, y+1, boxW-2, sanitizeTerminal(title), cMagenta, cWhite, cMagenta)
	for i, r := range rows {
		ry := y + 3 + i
		s.textRight(x+2, ry, labelW, r.label+":", cDarkGray, cBlack)
		s.text(x+2+labelW+1, ry, sanitizeTerminal(r.value), cWhite, cBlack, x+boxW-2)
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// formatMillis renders a duration in milliseconds, or "-" when unknown.
func formatMillis(ms int64) string {
	if ms <= 0 {
		return "-"
	}
	return (time.Duration(ms) * time.Millisecond).Round(time.Millisecond).String()
}

// labelOrFallback returns val if non-empty, else fallback.
func labelOrFallback(val, fallback string) string {
	if strings.TrimSpace(val) == "" {
		return fallback
	}
	return val
}

// formatTimestamp renders a time.Time as YYYY-MM-DD HH:MM:SS, or "(none)"
// if the time is zero.
func formatTimestamp(t time.Time) string {
	if t.IsZero() {
		return "(none)"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

// formatTimeLeft converts the TimeLeftMins value to a human-readable string.
// -1 means unknown.
func formatTimeLeft(mins int) string {
	switch {
	case mins < 0:
		return "(unknown)"
	case mins == 0:
		return "0m (expired)"
	default:
		return fmt.Sprintf("%dm", mins)
	}
}
