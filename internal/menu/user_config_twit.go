package menu

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// twitPolicy reads list limits and exemption levels from the current board configuration.
func twitPolicy(e *MenuExecutor) user.TwitPolicy {
	cfg := e.GetServerConfig()
	return user.TwitPolicy{MaxEntries: cfg.TwitListMax, ExemptLevel: cfg.TwitExemptLevel, SysOpLevel: cfg.SysOpLevel}
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

// tryAddTwit validates and saves an entry, returning the message to show.
func tryAddTwit(c *cmdCtx, name string) (string, bool) {
	entry, err := user.NewTwitEntry(c.currentUser, c.userManager, name, twitPolicy(c.e))
	st := c.e.Strings()
	if err != nil {
		text := st.TwitInvalid
		switch err {
		case user.ErrTwitSelf:
			text = st.TwitSelf
		case user.ErrTwitExempt:
			text = st.TwitExempt
		case user.ErrTwitFull:
			text = st.TwitFull
		case user.ErrTwitDuplicate:
			text = st.TwitDuplicate
		}
		return text, false
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

// twitScreenReserve is the rows below the entries: a gap, the page line, a
// notice (blank line plus text) and the prompt.
const twitScreenReserve = 6

// twitPageSize is how many entries fit between the header and the prompt.
func twitPageSize(termHeight int, header string) int {
	if termHeight <= 0 {
		termHeight = 25
	}
	return max(termHeight-strings.Count(header, "\n")-twitScreenReserve, 5)
}

// drawTwitScreen clears the screen and paints one page of the list with any
// notice from the last action above the prompt.
func drawTwitScreen(c *cmdCtx, page int, notice string) int {
	u, st := c.currentUser, c.e.Strings()
	perPage := twitPageSize(c.termHeight, st.TwitHeader)
	pages := max((len(u.TwitList)+perPage-1)/perPage, 1)
	page = min(max(page, 0), pages-1)
	terminalio.WriteProcessedBytes(c.terminal, []byte(ansi.ClearScreen()), c.outputMode)
	c.e.writeStringf(c.terminal, c.outputMode, st.TwitHeader, len(u.TwitList), twitPolicy(c.e).Limit())
	if len(u.TwitList) == 0 {
		c.e.writeString(c.terminal, c.outputMode, st.TwitEmpty)
	}
	for i := page * perPage; i < min((page+1)*perPage, len(u.TwitList)); i++ {
		entry := u.TwitList[i]
		name := entry.Name
		if entry.Addr != "" {
			name += "@" + entry.Addr
		}
		line := fmt.Sprintf("  %d. %s", i+1, name)
		// Clip long names so every entry stays on one row of the page.
		if r := []rune(line); c.termWidth > 1 && len(r) >= c.termWidth {
			line = string(r[:c.termWidth-1])
		}
		// Names are data, not pipe codes or string scripts.
		terminalio.WriteProcessedBytes(c.terminal, []byte(line+"\r\n"), c.outputMode)
	}
	if pages > 1 {
		c.e.writeStringf(c.terminal, c.outputMode, st.TwitPage, page+1, pages)
	}
	c.e.writeString(c.terminal, c.outputMode, notice)
	c.e.writeString(c.terminal, c.outputMode, st.TwitChoice)
	return page
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
	for {
		page = drawTwitScreen(c, page, notice)
		notice = ""
		choice, err := readLineFromSessionIH(c.s, c.terminal)
		if err != nil {
			return u, "LOGOFF", err
		}
		switch strings.ToUpper(strings.TrimSpace(choice)) {
		case "", "Q":
			return u, "", nil
		case "N":
			page++
		case "P":
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
