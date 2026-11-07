package menu

import (
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// Network Who's Online and Last Callers can be laid out by the menu set, as
// the local screens are with WHOONLN and LASTCALL: WHONET.TOP/MID/BOT and
// LASTNET.TOP/MID/BOT in the set's templates folder. TOP and BOT are drawn
// once and MID once per row, each with @CODE@ tokens filled in (every width
// and alignment form replaceMenuATCode takes: @UN:20@, @UN##########@,
// @UN|R:20@ and so on). A set without all three files of a screen gets the
// built-in layout, so an install whose menus predate them looks as before.
//
// Pipe codes in the templates are expanded before the tokens are filled,
// so text another board published is drawn as text: it can never act as a
// colour or screen code, whatever it holds. The key help, any fetch errors
// and the pause prompt are drawn by the screen below BOT.

// centralTemplate is one screen's three template files, read, converted to
// UTF-8 and with their pipe codes expanded.
type centralTemplate struct {
	top, mid, bot string
}

// loadCentralTemplate reads base.TOP, base.MID and base.BOT from the menu
// set. It reports false when any of them is missing or unreadable, and the
// screen then uses its built-in layout.
func (e *MenuExecutor) loadCentralTemplate(base string) (centralTemplate, bool) {
	var parts [3]string
	for i, ext := range []string{".TOP", ".MID", ".BOT"} {
		data, err := readTemplateFile(e.templateFile(base + ext))
		if err != nil {
			slog.Debug("menu template not found; using the built-in layout", "template", base+ext, "error", err)
			return centralTemplate{}, false
		}
		data = normalizePipeCodeDelimiters(stripSauceMetadata(data))
		// Art may be CP437 or UTF-8. It is held as UTF-8, with the values
		// filled in as UTF-8, and converted for the caller's terminal when
		// the screen is written.
		data = ansi.CP437ArtToUTF8(data)
		parts[i] = string(ansi.ReplacePipeCodes(data))
	}
	// The MID template draws one row; its own line breaks are replaced by
	// one per row.
	return centralTemplate{top: parts[0], mid: strings.TrimRight(parts[1], "\r\n"), bot: parts[2]}, true
}

// centralTemplateLines returns how many screen rows s takes once its last
// line is ended.
func centralTemplateLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// rowsThatFit returns how many MID rows fit on a termHeight-row screen with
// TOP, BOT and footer more rows below them, at least one.
func (t centralTemplate) rowsThatFit(termHeight, footer int) int {
	per := max(centralTemplateLines(t.mid), 1)
	free := termHeight - centralTemplateLines(t.top) - centralTemplateLines(t.bot) - footer - 1
	return max(free/per, 1)
}

// fillCentralTokens fills each @CODE@ token in s from vals. A value is cut
// to one line first: it may be another board's text.
func fillCentralTokens(s string, vals map[string]string) string {
	b := []byte(s)
	for code, v := range vals {
		b = replaceMenuATCode(b, code, ansi.OneLine(v))
	}
	return string(b)
}

// endLine returns s ending in a line break.
func endLine(s string) string {
	if s == "" || strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\r\n"
}

// renderWhoTemplate draws network Who's Online with t. TOP and BOT take
// @CT@ (callers shown), @TT@ (callers in all) and @NW@ (the networks).
// Each MID row takes @UN@ handle, @BB@ board, @NW@ network, @AV@
// availability (available, busy, or - when not shared) and @NS@ "(2 nodes)"
// when the caller is on more than one node there, else blank.
func renderWhoTemplate(t centralTemplate, rows []centralWhoRow, total int, networks []string) string {
	global := map[string]string{
		"CT": strconv.Itoa(len(rows)),
		"TT": strconv.Itoa(total),
		"NW": strings.Join(networks, ", "),
	}
	var b strings.Builder
	b.WriteString(endLine(fillCentralTokens(t.top, global)))
	for _, r := range rows {
		avail := r.availability
		if avail == "" {
			avail = "-"
		}
		ns := ""
		if r.sessions > 1 {
			ns = "(" + strconv.Itoa(r.sessions) + " nodes)"
		}
		b.WriteString(fillCentralTokens(t.mid, map[string]string{
			"UN": r.handle, "BB": r.board, "NW": r.network, "AV": avail, "NS": ns,
		}) + "\r\n")
	}
	b.WriteString(endLine(fillCentralTokens(t.bot, global)))
	return b.String()
}

// renderLastCallsTemplate draws network Last Callers with t. TOP and BOT
// take @CT@ (calls shown), @NW@ (the networks) and @LEGEND@ (a line saying
// what the From column means in this view). Each MID row takes @UN@
// handle, @BB@ board, @NW@ where the call was (the network's name, or
// "local" for this board's own calls in the combined view), @DY@ the day
// it began (Mon), @LO@ and @LT@ when it began and ended (03:04pm), and
// @TO@ its length in minutes (- while unknown).
func renderLastCallsTemplate(t centralTemplate, rows []centralCallRow, networks []string, includeLocal bool, timeLoc *time.Location) string {
	legend := "Calls shared by callers on V3Net boards, kept 24 hours"
	if includeLocal {
		legend = "local = this board; a network name = shared from a V3Net board"
	}
	global := map[string]string{
		"CT":     strconv.Itoa(len(rows)),
		"NW":     strings.Join(networks, ", "),
		"LEGEND": legend,
	}
	var b strings.Builder
	b.WriteString(endLine(fillCentralTokens(t.top, global)))
	for _, r := range rows {
		from := r.network
		if r.local {
			from = "local"
		}
		day, on, off, mins := "", "", "", "-"
		if !r.began.IsZero() {
			day = r.began.In(timeLoc).Format("Mon")
			on = formatLastCallerShortLocalTime(r.began, timeLoc)
		}
		if !r.ended.IsZero() && !r.ended.Before(r.began) {
			off = formatLastCallerShortLocalTime(r.ended, timeLoc)
			mins = strconv.Itoa(int(r.ended.Sub(r.began).Minutes()))
		}
		b.WriteString(fillCentralTokens(t.mid, map[string]string{
			"UN": r.handle, "BB": r.board, "NW": from, "DY": day, "LO": on, "LT": off, "TO": mins,
		}) + "\r\n")
	}
	b.WriteString(endLine(fillCentralTokens(t.bot, global)))
	return b.String()
}

// centralAvailabilityText is how the built-in screen colours an
// availability, kept here beside the template's plain form.
func centralAvailabilityText(avail string) string {
	switch avail {
	case protocol.CentralAvailable:
		return "|10available|07"
	case protocol.CentralBusy:
		return "|14busy|07"
	}
	return "|08-|07"
}
