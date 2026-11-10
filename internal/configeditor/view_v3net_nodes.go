package configeditor

import (
	"fmt"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// nodesListVisible is the number of rows shown in the node management list.
const nodesListVisible = 10

// nodesDetailRows is the number of rows below the list describing the node
// under the cursor, after a separator.
const nodesDetailRows = 3

// viewV3NetNodes renders the node management screen for a hosted network.
func (m Model) viewV3NetNodes() string {
	boxW := 70
	listVisible := nodesListVisible
	total := len(m.nodesList)

	lb := m.newListBox(boxW, listVisible+9+1+nodesDetailRows)
	lb.topBorder()
	lb.title(fmt.Sprintf("Node Management — %s", m.nodesNetwork))

	// The status screens fill the rows the list and its detail rows take.
	statusRows := listVisible + 1 + nodesDetailRows
	if m.nodesLoading {
		return lb.statusScreen(menuItemStyle.Render(centerText("Fetching nodes...", boxW)),
			statusRows, 3, "ESC - Cancel")
	}
	if m.nodesError != "" && total == 0 {
		return lb.statusScreen(lb.errorRow(m.nodesError),
			statusRows, 3, "R - Retry  |  ESC - Back")
	}

	lb.colHeader(fmt.Sprintf("   %-16s %-16s %-8s %-10s %s", "Node ID", "BBS Name", "Status", "Version", "Joined"))
	lb.separator()
	lb.list(listVisible, m.nodesScroll, m.nodesCursor, total,
		func(i int) string {
			n := m.nodesList[i]
			// Nodes choose what they send, and the terminal gets it as written.
			// The list keeps it exact because actions send the node ID back.
			joined := ansi.TruncateRunes(ansi.OneLine(n.CreatedAt), 10, "") // date only
			return fmt.Sprintf("   %-16s %s %s %s %s",
				ansi.OneLine(n.NodeID), padRight(ansi.OneLine(n.BBSName), 16), padRight(ansi.OneLine(n.Status), 8),
				padRight(nodeVersion(n), 10), joined)
		})
	lb.separator()
	var detail [nodesDetailRows]string
	if n := m.currentNode(); n != nil {
		detail = nodeDetail(*n, time.Now())
	}
	for _, line := range detail {
		lb.row(menuItemStyle.Render(padRight(" "+line, boxW)))
	}
	lb.bottomBorder()
	lb.bgRows(lb.bottomPad)
	if m.nodesConfirmDelete {
		lb.messageRow("Remove this node registration? Y/N")
	} else {
		lb.messageRow(m.message)
	}
	lb.bgRows(1)
	return lb.finish("A - Approve  |  B - Ban/Unban  |  D - Delete  |  R - Refresh  |  ESC - Back")
}

// nodeVersion is the list's Version column: the reported version, "?" for
// a node that has not reported, and "-" for a report without one.
func nodeVersion(n protocol.NodeInfo) string {
	switch {
	case n.Software == nil:
		return "?"
	case n.Software.Version == "":
		return "-"
	}
	return ansi.OneLine(n.Software.Version)
}

// nodeDetail returns the detail rows for n: the software it reported, how
// fresh that report is, and what that means for the features this hub
// serves. now is the time the screen is drawn, for the report's age. The
// hub validated the report before storing it, but it came from the node,
// so every field is still made safe for the terminal here.
func nodeDetail(n protocol.NodeInfo, now time.Time) [nodesDetailRows]string {
	var out [nodesDetailRows]string
	reported, timeErr := time.Parse(time.RFC3339, n.SoftwareReportedAt)
	if n.Software == nil {
		// Unknown, which is not the same as out of date.
		out[0] = "Software: unknown (not reported)"
		if timeErr == nil {
			// The hub refused its last report as malformed.
			out[1] = "Its last report was not valid (" + ago(reported, now) + ")" + nodeOnline(n)
		} else {
			out[1] = "Never reported to this hub" + nodeOnline(n)
		}
	} else {
		out[0] = "Software: " + ansi.OneLine(n.Software.String()) + " (self-reported)"
		if timeErr == nil {
			out[1] = "Reported " + reportAge(reported, now) + nodeOnline(n)
		} else {
			out[1] = "Report time unknown" + nodeOnline(n)
		}
	}
	out[2] = nodeFeatures(n)
	for i := range out {
		out[i] = ansi.TruncateRunes(ansi.OneLine(out[i]), 68, "…")
	}
	return out
}

// reportAge formats when a report arrived, as a UTC date and time and how
// long ago: "2026-11-10 09:00 UTC (3h ago)".
func reportAge(at, now time.Time) string {
	return at.UTC().Format("2006-01-02 15:04") + " UTC (" + ago(at, now) + ")"
}

// ago formats how long before now at was: "just now", "30m ago", "3h ago"
// or, from two days, "9d ago".
func ago(at, now time.Time) string {
	age := now.Sub(at)
	switch {
	case age < time.Minute:
		return "just now"
	case age < time.Hour:
		return fmt.Sprintf("%dm ago", int(age.Minutes()))
	case age < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(age.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(age.Hours()/24))
}

// nodeOnline says whether the report can be taken as current: a node online
// now reported when it last started, while one offline may have changed its
// software since.
func nodeOnline(n protocol.NodeInfo) string {
	if n.Online {
		return "; online now"
	}
	return "; offline, may be stale"
}

// nodeFeatures sums up protocol.AssessFeatures for n. Only a feature the
// node reported software without is put down to the node; one the hub does
// not serve is the hub's, and a node that has not reported is unknown, not
// out of date. Whether a board has a supported feature turned on, or its
// callers take part, is theirs to choose and not shown.
func nodeFeatures(n protocol.NodeInfo) string {
	if len(n.Features) == 0 {
		return "Features: not assessed by this hub"
	}
	var upgrade, missing, hubLacks []string
	unknown := false
	for _, f := range n.Features {
		switch f.State {
		case protocol.FeatureNeedsUpgrade:
			// A release is named only when one is known to have it and
			// the node reports an earlier ViSiON/3; otherwise the software
			// lacks it and no release is promised.
			if f.UpgradeTo != "" {
				upgrade = append(upgrade, fmt.Sprintf("%s (v%s+)", f.Title, ansi.OneLine(f.UpgradeTo)))
			} else {
				missing = append(missing, f.Title)
			}
		case protocol.FeatureHubLacks:
			hubLacks = append(hubLacks, f.Title)
		case protocol.FeatureUnknown:
			unknown = true
		}
	}
	var parts []string
	if len(upgrade) > 0 {
		parts = append(parts, "Upgrade for: "+strings.Join(upgrade, ", "))
	}
	if len(missing) > 0 {
		parts = append(parts, "Not in its software: "+strings.Join(missing, ", "))
	}
	line := strings.Join(parts, "; ")
	switch {
	case line != "":
	case unknown:
		line = "Features: unknown until it reports"
	default:
		line = "Features: supports all this hub serves"
	}
	if len(hubLacks) > 0 {
		line += "; hub does not serve " + strings.Join(hubLacks, ", ")
	}
	return line
}
