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
		path, err := e.Menus().ResolveFirst("templates", name, name+".ANS", name+".ans")
		if err != nil {
			return nil, err
		}
		b, err := readTemplateFile(path)
		if err == nil {
			return b, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return readTemplateFile(e.templateFile(base + "." + part))
}

// doorMenuLines measures flowing template text, including wrapped lines.
// MID templates may span multiple lines. Leave the last terminal column free
// in shipped art to avoid terminal-dependent autowrap at exactly the margin.
func doorMenuLines(s string, width int) int {
	s = strings.TrimRight(s, "\r\n")
	if s == "" {
		return 0
	}
	rows, _ := ansi.ArtGeometry(ansi.ReplacePipeCodes([]byte(s)), width)
	return rows
}

func runDoorMenu(c *cmdCtx, args string) (*user.User, string, error) {
	if c.currentUser == nil {
		return nil, "", nil
	}
	e := c.e
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
	for {
		cfg := e.GetServerConfig()
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
		expand := func(s string, pg, pt int) string {
			s = strings.NewReplacer("^CN", name, "^TI", rec.Title, "^PG", strconv.Itoa(pg), "^PT", strconv.Itoa(pt)).Replace(s)
			return string(e.applyCommonTemplateTokens([]byte(s), c.currentUser, c.nodeNumber))
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
		for i, entry := range entries {
			rowHeight = max(rowHeight, doorMenuLines(line(entry, i+1, maxPage, maxPage), w))
		}
		size := doorMenuPageSize(h, doorMenuLines(expand(art[0], maxPage, maxPage), w), doorMenuLines(expand(art[2], maxPage, maxPage), w), doorMenuLines(prompt.String()+strings.Repeat("X", 16), w), rowHeight)
		pages := max(1, (len(entries)+size-1)/size)
		page := selected / size
		start := page * size
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
		if err := block(expand(art[0], page+1, pages)); err != nil {
			return c.currentUser, "", err
		}
		if len(entries) == 0 {
			if err := block(e.Strings().DoorMenuEmpty); err != nil {
				return c.currentUser, "", err
			}
		}
		for i := start; i < min(start+size, len(entries)); i++ {
			s := strings.TrimRight(line(entries[i], i+1, page+1, pages), "\r\n")
			color := normal
			if barColors {
				s = doorMenuSGR.ReplaceAllString(string(ansi.ReplacePipeCodes([]byte(s))), "")
			}
			if lightbar && i == selected {
				color = hi
				s = doorMenuSGR.ReplaceAllString(string(ansi.ReplacePipeCodes([]byte(s))), "")
			}
			if err := block(color + s + "\x1b[0m"); err != nil {
				return c.currentUser, "", err
			}
		}
		if err := block(expand(art[2], page+1, pages)); err != nil {
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
			case editor.KeyArrowUp:
				if lightbar {
					selected--
				}
				typed = ""
			case editor.KeyArrowDown:
				if lightbar {
					selected++
				}
				typed = ""
			case editor.KeyHome:
				selected = 0
				typed = ""
			case editor.KeyEnd:
				selected = len(entries) - 1
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
