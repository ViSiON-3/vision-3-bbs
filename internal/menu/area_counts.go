package menu

import (
	"bytes"
	"log/slog"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Column widths for the MSGAREA row template, shared by the lightbar picker
// and the text listing so both line up under the MSGAREA.TOP header.
const (
	areaGutterWidth = 3  // ^ID: list number, or the NEW flag in the lightbar
	areaNameWidth   = 34 // ^NA
	areaConfWidth   = 12 // ^CF
	areaTotalWidth  = 7  // ^TM
	areaNewWidth    = 6  // ^NM
	areaYoursWidth  = 6  // ^YM
)

// areaNewFlag is the marker shown left of the area name when the area holds
// messages the user has not read, and newFlagColor is the pipe code it is
// drawn in.
const (
	areaNewFlag  = "NEW"
	newFlagColor = "|12"
)

// collectAreaCounts tallies total/new/personal message counts for each area,
// keyed by area ID. Counting opens each JAM base once, so callers should build
// the map once per listing rather than per rendered row. Areas whose counts
// cannot be read are logged and left at zero so the list still renders.
func collectAreaCounts(e *MenuExecutor, areas []*message.MessageArea, currentUser *user.User, nodeNumber int) map[int]message.AreaCounts {
	counts := make(map[int]message.AreaCounts, len(areas))
	if e.MessageMgr == nil {
		return counts
	}
	handle := ""
	if currentUser != nil {
		handle = currentUser.Handle
	}
	for _, area := range areas {
		c, err := e.MessageMgr.GetAreaCounts(area.ID, handle)
		if err != nil {
			slog.Warn("failed to read message counts for area",
				"node", nodeNumber, "areaID", area.ID, "tag", area.Tag, "error", err)
			continue
		}
		counts[area.ID] = c
	}
	return counts
}

// confNameForArea returns the name of the conference an area belongs to, or an
// empty string when the conference is unknown.
func confNameForArea(e *MenuExecutor, area *message.MessageArea) string {
	if e.ConferenceMgr == nil {
		return ""
	}
	if conf, found := e.ConferenceMgr.GetByID(area.ConferenceID); found {
		return conf.Name
	}
	return ""
}

// applyAreaColumnTokens substitutes the area name, conference and message-count
// tokens of a MSGAREA row template.
func applyAreaColumnTokens(line string, e *MenuExecutor, area *message.MessageArea, counts message.AreaCounts) string {
	line = strings.ReplaceAll(line, "^NA", padRight(truncateStr(area.Name, areaNameWidth), areaNameWidth))
	line = strings.ReplaceAll(line, "^CF", padRight(truncateStr(confNameForArea(e, area), areaConfWidth), areaConfWidth))
	line = strings.ReplaceAll(line, "^TM", ansi.PadLeft(strconv.Itoa(counts.Total), areaTotalWidth))
	line = strings.ReplaceAll(line, "^NM", ansi.PadLeft(strconv.Itoa(counts.New), areaNewWidth))
	line = strings.ReplaceAll(line, "^YM", ansi.PadLeft(strconv.Itoa(counts.Personal), areaYoursWidth))
	return line
}

// areaColumnHeaderToken is the MSGAREA.TOP placeholder replaced with the
// column-title row.
const areaColumnHeaderToken = "^COLS"

// areaColumnHeaderColor is the pipe code the column titles are drawn in.
const areaColumnHeaderColor = "|05"

// areaColumnHeader renders the column-title row from the same widths the row
// template uses, so the titles cannot drift out of alignment with the data.
func areaColumnHeader() string {
	return areaColumnHeaderColor + " " +
		strings.Repeat(" ", areaGutterWidth) + " " +
		padRight("Area", areaNameWidth) + " " +
		padRight("Conf", areaConfWidth) + " " +
		ansi.PadLeft("Total", areaTotalWidth) + " " +
		ansi.PadLeft("New", areaNewWidth) + " " +
		ansi.PadLeft("Yours", areaYoursWidth)
}

// injectAreaColumnHeader substitutes the ^COLS token in a MSGAREA.TOP template
// with the rendered column titles. Templates without the token are unchanged.
func injectAreaColumnHeader(top []byte) []byte {
	if !bytes.Contains(top, []byte(areaColumnHeaderToken)) {
		return top
	}
	return bytes.ReplaceAll(top, []byte(areaColumnHeaderToken), []byte(areaColumnHeader()))
}
