package menu

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// twitPolicy reads list limits and exemption levels from the current board configuration.
func twitPolicy(e *MenuExecutor) user.TwitPolicy {
	cfg := e.GetServerConfig()
	return user.TwitPolicy{MaxEntries: cfg.TwitListMax, ExemptLevel: cfg.TwitExemptLevel, SysOpLevel: cfg.SysOpLevel}
}

// twitMatcher resolves the session list against current local accounts.
func twitMatcher(e *MenuExecutor, um *user.UserMgr, u *user.User) *user.TwitMatcher {
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

// addTwit validates and saves an entry, displaying the result to the caller.
func addTwit(c *cmdCtx, name string) bool {
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
		c.e.writeString(c.terminal, c.outputMode, text)
		return false
	}
	entries := append(append([]user.TwitEntry(nil), c.currentUser.TwitList...), entry)
	if err := saveTwitList(c.userManager, c.currentUser, entries); err != nil {
		c.e.writeString(c.terminal, c.outputMode, st.TwitSaveFailed)
		return false
	}
	c.e.writeString(c.terminal, c.outputMode, st.TwitAdded)
	return true
}

// runCfgTwitList runs the add, delete and clear prompt loop for the caller.
func runCfgTwitList(c *cmdCtx, args string) (*user.User, string, error) {
	u := c.currentUser
	if u == nil {
		return nil, "", nil
	}
	st := c.e.Strings()
	read := func(prompt string) (string, error) {
		c.e.writeString(c.terminal, c.outputMode, prompt)
		return readLineFromSessionIH(c.s, c.terminal)
	}
	for {
		c.e.writeStringf(c.terminal, c.outputMode, st.TwitHeader, len(u.TwitList), twitPolicy(c.e).Limit())
		for i, entry := range u.TwitList {
			name := entry.Name
			if entry.Addr != "" {
				name += "@" + entry.Addr
			}
			// Names are data, not pipe codes or string scripts.
			terminalio.WriteProcessedBytes(c.terminal, []byte(fmt.Sprintf("  %d. %s\r\n", i+1, name)), c.outputMode)
		}
		choice, err := read(st.TwitChoice)
		if err != nil {
			return u, "LOGOFF", err
		}
		switch strings.ToUpper(strings.TrimSpace(choice)) {
		case "", "Q":
			return u, "", nil
		case "A":
			name, err := read(st.TwitNamePrompt)
			if err != nil {
				return u, "LOGOFF", err
			}
			addTwit(c, name)
		case "D", "C":
			var entries []user.TwitEntry
			if strings.EqualFold(strings.TrimSpace(choice), "D") {
				number, err := read(st.TwitDeletePrompt)
				if err != nil {
					return u, "LOGOFF", err
				}
				n, err := strconv.Atoi(strings.TrimSpace(number))
				if err != nil || n < 1 || n > len(u.TwitList) {
					c.e.writeString(c.terminal, c.outputMode, st.TwitInvalid)
					continue
				}
				entries = append(entries, u.TwitList[:n-1]...)
				entries = append(entries, u.TwitList[n:]...)
			} else {
				answer, err := read(st.TwitClearConfirm)
				if err != nil {
					return u, "LOGOFF", err
				}
				if !strings.EqualFold(strings.TrimSpace(answer), "Y") {
					continue
				}
			}
			if err := saveTwitList(c.userManager, u, entries); err != nil {
				c.e.writeString(c.terminal, c.outputMode, st.TwitSaveFailed)
			} else if strings.EqualFold(strings.TrimSpace(choice), "D") {
				c.e.writeString(c.terminal, c.outputMode, st.TwitRemoved)
			} else {
				c.e.writeString(c.terminal, c.outputMode, st.TwitCleared)
			}
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
	st.redraw = true
	_, _, err := runCfgTwitList(st.c, "")
	return err
}
