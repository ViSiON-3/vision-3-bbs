package menu

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"golang.org/x/term"
)

// twitPolicy reads list limits and exemption levels from the current board configuration.
func twitPolicy(e *MenuExecutor) user.TwitPolicy {
	cfg := e.GetServerConfig()
	return user.TwitPolicy{MaxEntries: cfg.TwitListMax, ExemptLevel: cfg.TwitExemptLevel, SysOpLevel: cfg.SysOpLevel, CoSysOpLevel: cfg.CoSysOpLevel}
}

// twitMatcher resolves the session list against current local accounts. It
// is nil, hiding nothing, while the sysop has Twit Lists turned off.
func twitMatcher(e *MenuExecutor, um *user.UserMgr, u *user.User) *user.TwitMatcher {
	if !twitListsOn(e) {
		return nil
	}
	var entries []user.TwitEntry
	if u != nil {
		entries = u.TwitList
	}
	// A typed nil directory must not be passed through an interface.
	var directory user.TwitDirectory
	if um != nil {
		directory = um
	}
	return user.BuildTwitMatcher(entries, directory, twitPolicy(e), e.Strings().AnonymousName)
}

// TwitMatcher is twitMatcher for callers outside the menu package, such as
// the QWK API, so every export applies the same board settings.
func (e *MenuExecutor) TwitMatcher(um *user.UserMgr, u *user.User) *user.TwitMatcher {
	return twitMatcher(e, um, u)
}

// saveTwitList owns its slice and restores session state when persistence fails.
func saveTwitList(um *user.UserMgr, u *user.User, entries []user.TwitEntry) error {
	previous := u.TwitList
	u.TwitList = append([]user.TwitEntry(nil), entries...)
	if err := um.UpdateUser(u); err != nil {
		u.TwitList = previous
		return err
	}
	return nil
}

// twitRefusal returns the message explaining why NewTwitEntry refused an add.
func twitRefusal(st *config.StringsConfig, err error) string {
	switch err {
	case user.ErrTwitSelf:
		return st.TwitSelf
	case user.ErrTwitExempt:
		return st.TwitExempt
	case user.ErrTwitFull:
		return st.TwitFull
	case user.ErrTwitDuplicate:
		return st.TwitDuplicate
	}
	return st.TwitInvalid
}

// tryAddTwit validates and saves an entry, returning the message to show.
func tryAddTwit(c *cmdCtx, name string) (string, bool) {
	entry, err := user.NewTwitEntry(c.currentUser, c.userManager, name, twitPolicy(c.e))
	st := c.e.Strings()
	if err != nil {
		return twitRefusal(st, err), false
	}
	entries := append(append([]user.TwitEntry(nil), c.currentUser.TwitList...), entry)
	if err := saveTwitList(c.userManager, c.currentUser, entries); err != nil {
		return st.TwitSaveFailed, false
	}
	return st.TwitAdded, true
}

// addTwit validates and saves an entry, displaying the result to the caller.
func addTwit(c *cmdCtx, name string) bool {
	text, ok := tryAddTwit(c, name)
	c.e.writeString(c.terminal, c.outputMode, text)
	return ok
}

// twitListsOn reports whether the sysop has Twit Lists turned on.
func twitListsOn(e *MenuExecutor) bool {
	return e.GetServerConfig().TwitListsOn()
}

// twitNoticeRows is the room kept above the prompt for the last action's
// result: a blank line and the message.
const twitNoticeRows = 2

// twitDefaultMenu and twitDefaultArt match the shipped TWITLIST.MNU and
// templates/TWITLIST.*, for a menu set that predates them or leaves them out.
var twitDefaultMenu = MenuRecord{Title: "Twit List", ClrScrBefore: true, UsePrompt: true, Prompt1: "|08[|07|TIME|08] |15Selection|08: |07"}

var twitDefaultArt = map[string]string{
	"TOP": "|15^TI |08/ |11Personal Ignore List\n" +
		"|07Hide messages from authors you'd rather not read. Your list is private:\n" +
		"|07nobody is told, and their messages are not deleted.  |08(|15^CT|08 of |15^MX|08 used)",
	"HDR": "\n|08  #   Author",
	"MID": "|07  ^ID ^NA",
	"BOT": "|08Page |15^PG|08 of |15^PT|08  [ / ] Page|07\n\n" +
		"|15A|08) |07Add an author        |15D|08) |07Delete an entry\n" +
		"|15C|08) |07Clear the list       |15Q|08) |07Quit",
}

// twitMenu loads TWITLIST.MNU, or the built-in default when the menu set
// has none.
func (e *MenuExecutor) twitMenu() (*MenuRecord, error) {
	if ok, err := e.Menus().Exists("mnu", "TWITLIST.MNU"); err != nil || !ok {
		rec := twitDefaultMenu
		return &rec, err
	}
	return LoadMenu("TWITLIST", e.Menus())
}

// twitTemplate reads one part of the Twit List art, or the built-in default
// when the menu set has no file for it.
func (e *MenuExecutor) twitTemplate(part string) (string, error) {
	b, err := readTemplateFile(e.templateFile("TWITLIST." + part))
	if os.IsNotExist(err) {
		return twitDefaultArt[part], nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

// drawTwitScreen paints one page of the Twit List the way the generated door
// menu is drawn: TWITLIST.MNU supplies the title, clear and prompt, and
// templates/TWITLIST.TOP, .HDR, .MID and .BOT the header, column heading,
// row and footer. It returns the page shown, clamped to the pages there are.
func drawTwitScreen(c *cmdCtx, page int, notice string) (int, error) {
	e, u, st := c.e, c.currentUser, c.e.Strings()
	rec, err := e.twitMenu()
	if err != nil {
		return page, err
	}
	var art [4]string
	for i, part := range []string{"TOP", "HDR", "MID", "BOT"} {
		if art[i], err = e.twitTemplate(part); err != nil {
			return page, err
		}
	}
	w := c.termWidth
	if w <= 0 {
		w = 80
	}
	limit := twitPolicy(e).Limit()
	expand := func(s string, pg, pt int) string {
		if pt <= 1 {
			s = doorMenuDropPaging(s)
		}
		s = strings.NewReplacer("^TI", rec.Title, "^CT", strconv.Itoa(len(u.TwitList)), "^MX", strconv.Itoa(limit),
			"^PG", strconv.Itoa(pg), "^PT", strconv.Itoa(pt)).Replace(s)
		return string(e.applyCommonTemplateTokens([]byte(s), u, c.nodeNumber))
	}
	var prompt bytes.Buffer
	if rec.GetUsePrompt() {
		if err := e.displayPrompt(c.s, term.NewTerminal(&prompt, ""), rec, u, c.userManager, c.nodeNumber, "TWITLIST", c.sessionStartTime, c.outputMode, ""); err != nil {
			return page, err
		}
	}
	heading := art[1]
	if len(u.TwitList) == 0 {
		heading = ""
	}
	// Size the page without paging lines first; only a list that overflows
	// it needs them, and then the page is sized with them.
	rows := func(pt int) int {
		top := doorMenuLines(expand(art[0], pt, pt), w) + doorMenuLines(expand(heading, pt, pt), w)
		bottom := doorMenuLines(expand(art[3], pt, pt), w) + twitNoticeRows
		return doorMenuPageSize(c.termHeight, top, bottom, doorMenuLines(prompt.String()+strings.Repeat("X", 16), w), 1)
	}
	perPage := rows(1)
	if len(u.TwitList) > perPage {
		perPage = rows(2)
	}
	pages := max((len(u.TwitList)+perPage-1)/perPage, 1)
	page = min(max(page, 0), pages-1)

	write := func(s string) {
		if s = strings.TrimRight(s, "\r\n"); s != "" {
			terminalio.WriteProcessedBytes(c.terminal, ansi.ReplacePipeCodes([]byte(s+"\r\n")), c.outputMode)
		}
	}
	if rec.GetClrScrBefore() {
		terminalio.WriteProcessedBytes(c.terminal, []byte(ansi.ClearScreen()), c.outputMode)
	}
	write(expand(art[0], page+1, pages))
	write(expand(heading, page+1, pages))
	if len(u.TwitList) == 0 {
		c.e.writeString(c.terminal, c.outputMode, st.TwitEmpty)
	}
	// The row template is expanded and coloured first and the name put in
	// after, so a name is shown as written, never read as pipe codes.
	row := string(ansi.ReplacePipeCodes([]byte(expand(art[2], page+1, pages))))
	for i := page * perPage; i < min((page+1)*perPage, len(u.TwitList)); i++ {
		entry := u.TwitList[i]
		name := entry.Name
		if entry.Addr != "" {
			name += "@" + entry.Addr
		}
		line := strings.Replace(strings.Replace(row, "^ID", fmt.Sprintf("%-3d", i+1), 1), "^NA", name, 1)
		// Clip long names so every entry stays on one row of the page.
		terminalio.WriteProcessedBytes(c.terminal, []byte(ansi.TruncateVisible(line, w-1)+"\x1b[0m\r\n"), c.outputMode)
	}
	write(expand(art[3], page+1, pages))
	c.e.writeString(c.terminal, c.outputMode, notice)
	if _, err := c.terminal.Write(prompt.Bytes()); err != nil {
		return page, err
	}
	return page, nil
}

// runCfgTwitList runs the full-screen Twit List section: the list, then add,
// delete, clear and paging commands until the caller quits.
func runCfgTwitList(c *cmdCtx, args string) (*user.User, string, error) {
	u := c.currentUser
	if u == nil {
		return nil, "", nil
	}
	st := c.e.Strings()
	if !twitListsOn(c.e) {
		c.e.writeString(c.terminal, c.outputMode, st.TwitDisabled)
		uiPause(readerNoticePause)
		return u, "", nil
	}
	read := func(prompt string) (string, error) {
		c.e.writeString(c.terminal, c.outputMode, prompt)
		return readLineFromSessionIH(c.s, c.terminal)
	}
	page, notice := 0, ""
	var err error
	for {
		if page, err = drawTwitScreen(c, page, notice); err != nil {
			return u, "", err
		}
		notice = ""
		choice, err := readLineFromSessionIH(c.s, c.terminal)
		if err != nil {
			return u, "LOGOFF", err
		}
		switch strings.ToUpper(strings.TrimSpace(choice)) {
		case "", "Q":
			return u, "", nil
		case "]", "N":
			page++
		case "[", "P":
			page--
		case "A":
			name, err := read(st.TwitNamePrompt)
			if err != nil {
				return u, "LOGOFF", err
			}
			if strings.TrimSpace(name) != "" {
				notice, _ = tryAddTwit(c, name)
			}
		case "D":
			number, err := read(st.TwitDeletePrompt)
			if err != nil {
				return u, "LOGOFF", err
			}
			n, err := strconv.Atoi(strings.TrimSpace(number))
			if err != nil || n < 1 || n > len(u.TwitList) {
				notice = st.TwitInvalid
				continue
			}
			entries := append(append([]user.TwitEntry(nil), u.TwitList[:n-1]...), u.TwitList[n:]...)
			notice = st.TwitRemoved
			if err := saveTwitList(c.userManager, u, entries); err != nil {
				notice = st.TwitSaveFailed
			}
		case "C":
			answer, err := read(st.TwitClearConfirm)
			if err != nil {
				return u, "LOGOFF", err
			}
			if !strings.EqualFold(strings.TrimSpace(answer), "Y") {
				continue
			}
			notice = st.TwitCleared
			if err := saveTwitList(c.userManager, u, nil); err != nil {
				notice = st.TwitSaveFailed
			}
		default:
			notice = st.TwitInvalid
		}
	}
}

// twitScanCounter counts each visited, otherwise-visible message once across all
// areas, even if navigation probes the same message repeatedly.
type twitScanCounter struct{ hidden map[[2]int]bool }

// record counts an otherwise-visible message once within a scan.
func (c *twitScanCounter) record(m *message.DisplayMessage) {
	if c == nil {
		return
	}
	if c.hidden == nil {
		c.hidden = make(map[[2]int]bool)
	}
	c.hidden[[2]int{m.AreaID, m.MsgNum}] = true
}

// withTwit narrows a visibility filter and optionally counts rejected authors.
func withTwit(matcher func() *user.TwitMatcher, base msgOwnershipFilter, count *twitScanCounter) msgOwnershipFilter {
	return func(m *message.DisplayMessage) bool {
		if base != nil && !base(m) {
			return false
		}
		if matcher().Hidden(m.From, m.OrigAddr) {
			count.record(m)
			return false
		}
		return true
	}
}

// editTwitList returns to a freshly painted configuration form on exit.
func editTwitList(st *konfigState) error {
	if !twitListsOn(st.c.e) {
		st.status = strings.TrimSpace(st.c.e.Strings().TwitDisabled)
		return nil
	}
	st.redraw = true
	_, _, err := runCfgTwitList(st.c, "")
	return err
}
