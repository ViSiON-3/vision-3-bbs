package menu

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"golang.org/x/term"
)

var doorMenuSGR = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// doorMenuTemplate falls back per part, so a category can override only TOP.
func (e *MenuExecutor) doorMenuTemplate(category, part string, categories bool) ([]byte, error) {
	base := "DOORMENU"
	if categories {
		base = "DOORCAT"
	}
	if category != "" {
		code, err := config.NormalizeDoorCode(category)
		if err != nil {
			return nil, err
		}
		name := base + "_" + code + "." + part
		names := []string{name, name + ".ANS", name + ".ans"}
		// Search the overlay under every spelling before the shipped tree,
		// so a sysop's copy wins whatever its suffix. ResolveFirst checks
		// both layers per spelling, which lets a shipped .ANS beat an
		// overlay .ans.
		set := e.Menus()
		layers := []menuset.Set{menuset.Bare(set.Base)}
		if set.HasOverlay() {
			layers = []menuset.Set{menuset.Bare(set.Overlay), layers[0]}
		}
		for _, layer := range layers {
			for _, n := range names {
				path, _, ok, err := layer.Locate("templates", n)
				if err != nil {
					return nil, err
				}
				if !ok {
					continue
				}
				b, err := readTemplateFile(path)
				if err == nil {
					return b, nil
				}
				if !os.IsNotExist(err) {
					return nil, err
				}
			}
		}
	}
	return readTemplateFile(e.templateFile(base + "." + part))
}

// doorMenuDropPaging leaves out every line of a header or footer that shows
// the page number or count, so paging help appears only when there is more
// than one page.
func doorMenuDropPaging(s string) string {
	lines := strings.SplitAfter(s, "\n")
	kept := lines[:0]
	for _, l := range lines {
		if !strings.Contains(l, "^PG") && !strings.Contains(l, "^PT") {
			kept = append(kept, l)
		}
	}
	return strings.Join(kept, "")
}

// doorMenuLines measures flowing template text, including wrapped lines.
// MID templates may span multiple lines. Leave the last terminal column free
// in shipped art to avoid terminal-dependent autowrap at exactly the margin.
func doorMenuLines(s string, width int) int {
	s = strings.TrimRight(s, "\r\n")
	if s == "" {
		return 0
	}
	// The terminal layer sends a bare LF as CRLF; measure what is sent, or
	// each line after a long one is counted from where that one ended.
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
	rows, _ := ansi.ArtGeometry(ansi.ReplacePipeCodes([]byte(s)), width)
	return rows
}

func runDoorMenu(c *cmdCtx, args string) (*user.User, string, error) {
	if c.currentUser == nil {
		return nil, "", nil
	}
	e := c.e
	// rec is re-read before every full redraw, so MNU edits (a tighter ACS,
	// a new fallback or prompt) take effect when a caller comes back from a
	// door or changes page, as they do for ordinary menus.
	rec, err := LoadMenu("DOORMENU", e.Menus())
	if err != nil {
		return c.currentUser, "", err
	}
	write := func(s string) error {
		return terminalio.WriteProcessedBytes(c.terminal, ansi.ReplacePipeCodes([]byte(s)), c.outputMode)
	}
	denied := func() (*user.User, string, error) {
		err := write(e.Strings().DoorMenuDenied)
		next := ""
		if rec.Fallback != "" {
			next = "GOTO:" + rec.Fallback
		}
		return c.currentUser, next, err
	}
	allows := func(acs string) bool { return checkACS(acs, c.currentUser, c.s, c.terminal, c.sessionStartTime) }
	if !allows(rec.ACS) {
		return denied()
	}
	category := strings.ToUpper(strings.TrimSpace(args))
	if category != "" {
		if _, err := config.NormalizeDoorCode(category); err != nil {
			return denied()
		}
	}
	direct := category != ""
	selected, rootSelected := 0, 0
	typed := ""
	for first := true; ; first = false {
		cfg := e.GetServerConfig()
		if !first {
			if rec, err = LoadMenu("DOORMENU", e.Menus()); err != nil {
				return c.currentUser, "", err
			}
		}
		if !allows(rec.ACS) {
			return denied()
		}
		name := rec.Title
		if category != "" {
			found := category == "OTHER"
			if found {
				name = e.Strings().DoorMenuOther
			}
			for _, cat := range cfg.DoorCategories {
				if cat.Code == category {
					found = true
					name = cat.Name
					if cat.MinAccessLevel > c.currentUser.AccessLevel || !allows(cat.ACS) {
						return denied()
					}
				}
			}
			if !found {
				return denied()
			}
		}
		categories := category == "" && len(cfg.DoorCategories) > 0
		entries := buildDoorMenuEntries(e.DoorRegistry(), cfg, category, c.currentUser.AccessLevel, allows, e.Strings().DoorMenuOther)
		selected = max(0, min(selected, len(entries)-1))
		w, h := c.termWidth, c.termHeight
		if c.s != nil {
			if p, _, ok := c.s.Pty(); ok {
				if p.Window.Width > 0 {
					w = p.Window.Width
				}
				if p.Window.Height > 0 {
					h = p.Window.Height
				}
			}
		}
		// SSH Pty() can retain the initial request. The registered node
		// exposes the live dimensions maintained by the resize handler.
		if node := e.nodeSession(c.nodeNumber); node != nil {
			nw, nh := node.TermSize()
			if nw > 0 {
				w = nw
			}
			if nh > 0 {
				h = nh
			}
		}
		w, h = resolveTermDims(c.currentUser, w, h)
		c.termWidth, c.termHeight = w, h
		var art [3]string
		for i, part := range []string{"TOP", "MID", "BOT"} {
			b, err := e.doorMenuTemplate(category, part, categories)
			if err != nil {
				return c.currentUser, "", err
			}
			art[i] = string(b)
		}
		// Columns need one-line rows. A COL template, when present, is the
		// row for a column layout; it is usually narrower than MID.
		cols := doorMenuFitColumns(cfg.DoorMenuColumnsFor(category), min(w, ansi.ArtWidth))
		if cols > 1 {
			b, err := e.doorMenuTemplate(category, "COL", categories)
			switch {
			case err == nil:
				art[1] = string(b)
			case !os.IsNotExist(err):
				return c.currentUser, "", err
			}
			if strings.Contains(strings.TrimRight(art[1], "\r\n"), "\n") {
				cols = 1
			}
		}
		// The column heading sits between the header and the rows, so it
		// can follow the layout: HDR above a one-column list, the first line
		// of CHD repeated over each column. Both are optional.
		headPart := "HDR"
		if cols > 1 {
			headPart = "CHD"
		}
		heading := ""
		switch b, err := e.doorMenuTemplate(category, headPart, categories); {
		case err == nil:
			heading = strings.TrimRight(string(b), "\r\n")
		case !os.IsNotExist(err):
			return c.currentUser, "", err
		}
		if cols > 1 {
			heading, _, _ = strings.Cut(strings.ReplaceAll(heading, "\r", ""), "\n")
		}
		expand := func(s string, pg, pt int) string {
			s = strings.NewReplacer("^CN", name, "^TI", rec.Title, "^PG", strconv.Itoa(pg), "^PT", strconv.Itoa(pt)).Replace(s)
			return string(e.applyCommonTemplateTokens([]byte(s), c.currentUser, c.nodeNumber))
		}
		// frame expands the header or footer, without its paging lines when
		// everything fits on one page.
		frame := func(s string, pg, pt int) string {
			if pt <= 1 {
				s = doorMenuDropPaging(s)
			}
			return expand(s, pg, pt)
		}
		line := func(entry doorMenuEntry, idx, pg, pt int) string {
			d := entry.door
			if entry.category {
				d = config.DoorConfig{Name: entry.name}
			}
			// Description is replaced in the same pass as existing row tokens.
			return formatDoorMenuLine(expand(art[1], pg, pt), idx, entry.code, d, entry.description, name)
		}
		// Render the real MNU prompt through the shared MCI pipeline, then measure
		// its result. This also accounts for includes and two-line prompts.
		var prompt bytes.Buffer
		if rec.GetUsePrompt() {
			if err := e.displayPrompt(c.s, term.NewTerminal(&prompt, ""), rec, c.currentUser, c.userManager, c.nodeNumber, "DOORMENU", c.sessionStartTime, c.outputMode, ""); err != nil {
				return c.currentUser, "", err
			}
		}
		maxPage := max(1, len(entries))
		rowHeight := 1
		if cols == 1 {
			for i, entry := range entries {
				rowHeight = max(rowHeight, doorMenuLines(line(entry, i+1, maxPage, maxPage), w))
			}
		}
		headRows := 0
		if heading != "" {
			headRows = 1
			if cols == 1 {
				headRows = doorMenuLines(expand(heading, maxPage, maxPage), w)
			}
		}
		promptRows := doorMenuLines(prompt.String()+strings.Repeat("X", 16), w)
		// Size the page without the paging lines first; only a list that
		// overflows that needs them, and then the page is sized with them.
		pageRows := func(pt int) int {
			return doorMenuPageSize(h, doorMenuLines(frame(art[0], pt, pt), w)+headRows, doorMenuLines(frame(art[2], pt, pt), w), promptRows, rowHeight)
		}
		rows := pageRows(1)
		if len(entries) > rows*cols {
			rows = pageRows(maxPage)
		}
		size := rows * cols
		pages := max(1, (len(entries)+size-1)/size)
		page := selected / size
		start := page * size
		// Entries run down each column. A page that is not full is split
		// evenly across the columns rather than filling the first one.
		onPage := min(size, len(entries)-start)
		rowsOnPage := max(1, (onPage+cols-1)/cols)
		cellWidth := (min(w, ansi.ArtWidth) - 1) / cols
		lightbar := cfg.DoorMenuMode != "list"
		barColors := false
		hi, normal := colorCodeToAnsi(e.Theme().YesNoHighlightColor), "\x1b[0m"
		if bars, err := loadBarFile("DOORMENUHI", e); err == nil && len(bars) > 0 {
			barColors = true
			hi = colorCodeToAnsi(bars[0].HighlightColor)
			normal = colorCodeToAnsi(bars[0].RegularColor)
		}
		if rec.GetClrScrBefore() {
			if err := write(ansi.ClearScreen()); err != nil {
				return c.currentUser, "", err
			}
		}
		block := func(s string) error {
			s = strings.TrimRight(s, "\r\n")
			if s == "" {
				return nil
			}
			return write(s + "\r\n")
		}
		if err := block(frame(art[0], page+1, pages)); err != nil {
			return c.currentUser, "", err
		}
		if len(entries) == 0 {
			if err := block(e.Strings().DoorMenuEmpty); err != nil {
				return c.currentUser, "", err
			}
		}
		// fit cuts and pads one column's text to the cell width, leaving a
		// space before the next column.
		fit := func(s string) string {
			s = ansi.PadVisible(ansi.TruncateVisible(s, cellWidth-1), cellWidth-1, ' ')
			return s + "\x1b[0m "
		}
		// cell renders entry i as drawn: the whole row in one column, or a
		// cell cut and padded to cellWidth in a column layout. It reads
		// selected when called, so it also redraws a cell after a move.
		cell := func(i int) string {
			s := strings.TrimRight(line(entries[i], i+1, page+1, pages), "\r\n")
			color, plain := normal, barColors
			if lightbar && i == selected {
				color, plain = hi, true
			}
			if plain || cols > 1 {
				s = string(ansi.ReplacePipeCodes([]byte(s)))
			}
			if plain {
				s = doorMenuSGR.ReplaceAllString(s, "")
			}
			if cols == 1 {
				return color + s + "\x1b[0m"
			}
			return color + fit(s)
		}
		if heading != "" && onPage > 0 {
			head := expand(heading, page+1, pages)
			if cols > 1 {
				one := string(ansi.ReplacePipeCodes([]byte(head)))
				head = strings.Repeat(fit(one), (onPage+rowsOnPage-1)/rowsOnPage)
			}
			if err := block(head); err != nil {
				return c.currentUser, "", err
			}
		}
		for r := 0; r < rowsOnPage; r++ {
			var row strings.Builder
			for k := 0; k < cols; k++ {
				if i := start + k*rowsOnPage + r; i < start+onPage {
					row.WriteString(cell(i))
				}
			}
			if err := block(row.String()); err != nil {
				return c.currentUser, "", err
			}
		}
		// A move within the page repaints just the two cells involved. That
		// needs known screen positions: the screen was cleared and every row
		// is one line, so entry i sits below the header at its row and column.
		inPlace := lightbar && rec.GetClrScrBefore() && rowHeight == 1 && onPage > 0
		topRows := doorMenuLines(frame(art[0], page+1, pages), w) + headRows
		at := func(i int) string {
			off := i - start
			return ansi.MoveCursor(1+topRows+off%rowsOnPage, 1+(off/rowsOnPage)*cellWidth)
		}
		// move selects entry to. It reports true when the screen is up to date
		// (a repaint in place, or nothing to do) and false when the page must
		// be redrawn.
		move := func(to int) (bool, error) {
			to = max(0, min(to, len(entries)-1))
			if to == selected {
				return true, nil
			}
			from := selected
			selected = to
			if !inPlace || to < start || to >= start+onPage {
				return false, nil
			}
			out := strings.Repeat("\b \b", len(typed)) + ansi.SaveCursor() + at(from) + cell(from) + at(to) + cell(to) + ansi.RestoreCursor()
			typed = ""
			return true, write(out)
		}
		if err := block(frame(art[2], page+1, pages)); err != nil {
			return c.currentUser, "", err
		}
		if _, err := c.terminal.Write(prompt.Bytes()); err != nil {
			return c.currentUser, "", err
		}
		if err := write(typed); err != nil {
			return c.currentUser, "", err
		}
		choose, back := false, false
		for {
			key, err := getSessionIH(c.s).ReadKey()
			if err != nil {
				return c.currentUser, "", err
			}
			switch key {
			case editor.KeyEsc:
				back = true
			case editor.KeyArrowUp, editor.KeyArrowDown, editor.KeyArrowLeft, editor.KeyArrowRight, editor.KeyHome, editor.KeyEnd:
				to, ok := selected, true
				switch key {
				case editor.KeyArrowUp:
					to, ok = selected-1, lightbar
				case editor.KeyArrowDown:
					to, ok = selected+1, lightbar
				case editor.KeyArrowLeft:
					// Same row, previous column.
					to, ok = selected-rowsOnPage, lightbar && selected-rowsOnPage >= start
				case editor.KeyArrowRight:
					// Same row, next column; the last entry when that column
					// is shorter.
					last := start + onPage - 1
					to = min(selected+rowsOnPage, last)
					ok = lightbar && (selected-start)/rowsOnPage < (last-start)/rowsOnPage
				case editor.KeyHome:
					to = 0
				case editor.KeyEnd:
					to = len(entries) - 1
				}
				if !ok {
					continue
				}
				done, err := move(to)
				if err != nil {
					return c.currentUser, "", err
				}
				if done {
					continue
				}
				typed = ""
			case editor.KeyPageUp, '[':
				selected = max(0, start-size)
				typed = ""
			case editor.KeyPageDown, ']':
				if start+size < len(entries) {
					selected = start + size
				}
				typed = ""
			case editor.KeyBackspace, 127:
				if len(typed) > 0 {
					typed = typed[:len(typed)-1]
					if err := write("\b \b"); err != nil {
						return c.currentUser, "", err
					}
				}
				continue
			case editor.KeyEnter:
				if typed != "" {
					idx, err := strconv.Atoi(typed)
					if err == nil && idx > 0 && idx <= len(entries) {
						selected = idx - 1
						choose = true
					} else {
						for i, entry := range entries {
							if strings.EqualFold(entry.code, typed) {
								selected = i
								choose = true
								break
							}
						}
					}
				} else {
					choose = lightbar && len(entries) > 0
				}
				typed = ""
			default:
				if (key == 'q' || key == 'Q') && typed == "" {
					back = true
				} else if key >= 32 && key < 127 && len(typed) < 16 {
					typed += string(rune(key))
					if err := write(string(rune(key))); err != nil {
						return c.currentUser, "", err
					}
				}
				if !back {
					continue
				}
			}
			break
		}
		if back {
			if category != "" && !direct {
				category = ""
				selected = rootSelected
				typed = ""
				continue
			}
			return c.currentUser, "", nil
		}
		if !choose {
			continue
		}
		entry := entries[selected]
		if entry.category {
			rootSelected = selected
			category = entry.code
			selected = 0
			continue
		}
		// Recheck the snapshot after input, in case configuration or access changed
		// while the caller was deciding. The normal door handler checks access too.
		fresh := buildDoorMenuEntries(e.DoorRegistry(), e.GetServerConfig(), category, c.currentUser.AccessLevel, allows, e.Strings().DoorMenuOther)
		allowed := false
		for _, d := range fresh {
			if !d.category && d.code == entry.code {
				allowed = true
				break
			}
		}
		if !allowed {
			continue
		}
		fn := e.RunRegistry["DOOR:"]
		if fn == nil {
			return c.currentUser, "", fmt.Errorf("DOOR handler not registered")
		}
		u, next, err := fn(c, entry.code)
		if u != nil {
			c.currentUser = u
		}
		if next != "" || isSessionFatal(err) {
			return c.currentUser, next, err
		}
		if err != nil {
			if werr := write(fmt.Sprintf(e.Strings().ExecRunDoorError, entry.code, err)); werr != nil {
				return c.currentUser, "", werr
			}
			uiPause(time.Second)
		}
		// Keep the selected door across registry changes and recompute page geometry
		// on the next redraw (including a resize performed while inside the door).
		fresh = buildDoorMenuEntries(e.DoorRegistry(), e.GetServerConfig(), category, c.currentUser.AccessLevel, allows, e.Strings().DoorMenuOther)
		for i, d := range fresh {
			if d.code == entry.code {
				selected = i
				break
			}
		}
	}
}
