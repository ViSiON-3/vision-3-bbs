package wfcui

import (
	"fmt"
	"strings"
	"time"
)

// detailsView renders the full details for the currently selected node.
func (m Model) detailsView() string {
	st := newStyles(m.opts)

	// Guard: no snapshot or selected index out of range.
	if m.snapshot == nil || m.selected < 0 || m.selected >= len(m.snapshot.Nodes) {
		lines := []string{
			"",
			st.dimmed.Render("No node selected."),
			"",
			st.cmdBar.Render("[Esc] back   [q] quit"),
		}
		return strings.Join(lines, "\n")
	}

	n := m.snapshot.Nodes[m.selected]

	// Build the detail rows as label/value pairs.
	rows := []struct{ label, value string }{
		{"Node", fmt.Sprintf("%d", n.NodeID)},
		{"Status", string(n.Status)},
		{"Handle", labelOrFallback(n.Handle, "(not logged in)")},
		{"User ID", fmt.Sprintf("%d", n.UserID)},
		{"Access Level", fmt.Sprintf("%d", n.AccessLevel)},
		{"Remote Addr", labelOrFallback(n.RemoteAddr, "(unknown)")},
		{"Current Menu", labelOrFallback(n.CurrentMenu, "(none)")},
		{"Activity", labelOrFallback(n.Activity, "(none)")},
		{"Connected At", formatTimestamp(n.ConnectedAt)},
		{"Last Activity", formatTimestamp(n.LastActivity)},
		{"Time Left", formatTimeLeft(n.TimeLeftMins)},
	}

	const labelWidth = 14
	var sb strings.Builder

	sb.WriteString("\n")
	title := fmt.Sprintf(" Node %d Details ", n.NodeID)
	sb.WriteString(st.header.Render(title))
	sb.WriteString("\n\n")

	for _, row := range rows {
		label := fmt.Sprintf("%-*s", labelWidth, row.label+":")
		sb.WriteString(" ")
		sb.WriteString(st.dimmed.Render(label))
		sb.WriteString(" ")
		sb.WriteString(row.value)
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(st.cmdBar.Render("[Esc] back   [q] quit"))

	return sb.String()
}

// labelOrFallback returns val if non-empty, else fallback.
func labelOrFallback(val, fallback string) string {
	if strings.TrimSpace(val) == "" {
		return fallback
	}
	return val
}

// formatTimestamp renders a time.Time as HH:MM:SS on YYYY-MM-DD, or "(none)"
// if the time is zero.
func formatTimestamp(t time.Time) string {
	if t.IsZero() {
		return "(none)"
	}
	return t.Format("2006-01-02 15:04:05")
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
