package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runRumorsList displays all visible rumors.
// Maps to V2's ListRumors procedure (simplified — no Stats/Both modes).
func runRumorsList(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil {
		return currentUser, "", nil
	}

	slog.Debug("running RUMORSLIST", "node", nodeNumber, "handle", currentUser.Handle)

	// Clear screen before listing
	wv(terminal, "\x1b[2J\x1b[H", outputMode)

	isSysop := currentUser.AccessLevel >= 255
	userLevel := currentUser.AccessLevel
	anonName := rumorAnonName(e)

	rumorsMu.Lock()
	rd, err := loadRumorsData(e.RootConfigPath)
	rumorsMu.Unlock()
	if err != nil {
		wv(terminal, "\r\n|04Error loading rumors.\r\n", outputMode)
		return currentUser, "", nil
	}

	visible := visibleRumors(rd, userLevel)
	if len(visible) == 0 {
		wv(terminal, "\r\n|07There are no rumors!\r\n", outputMode)
		return currentUser, "", nil
	}

	wv(terminal, fmt.Sprintf("\r\n|11%-4s%-42s%-16s%s\r\n", "#", "Rumor", "Author", "Date"), outputMode)
	wv(terminal, "|08"+strings.Repeat("\xc4", 70)+"\r\n", outputMode)

	for _, idx := range visible {
		r := &rd.Rumors[idx]
		author := rumorDisplayAuthor(r, isSysop, anonName)
		wv(terminal, fmt.Sprintf("|03%-4d|07%-42s|11%-16s|07%s\r\n",
			r.ID, truncateRunes(r.Text, 41), truncateRunes(author, 15), r.PostedAt.Format("01/02/06")), outputMode)
	}

	e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
	return currentUser, "", nil
}

// runRumorsSearch searches rumors by text or author.
// Maps to V2's SearchForText procedure.
func runRumorsSearch(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	if currentUser == nil {
		return currentUser, "", nil
	}

	slog.Debug("running RUMORSSEARCH", "node", nodeNumber, "handle", currentUser.Handle)

	isSysop := currentUser.AccessLevel >= 255
	userLevel := currentUser.AccessLevel
	anonName := rumorAnonName(e)

	rumorsMu.Lock()
	rd, err := loadRumorsData(e.RootConfigPath)
	rumorsMu.Unlock()
	if err != nil {
		wv(terminal, "\r\n|04Error loading rumors.\r\n", outputMode)
		return currentUser, "", nil
	}

	if len(rd.Rumors) == 0 {
		wv(terminal, "\r\n|07No rumors exist!\r\n", outputMode)
		return currentUser, "", nil
	}

	wv(terminal, "\r\n|15Search for text in rumors\r\n|07Enter text to search for:\r\n|07> ", outputMode)
	searchInput, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return currentUser, "", nil
	}
	if strings.TrimSpace(searchInput) == "" {
		return currentUser, "", nil
	}
	searchTerm := strings.ToUpper(strings.TrimSpace(searchInput))

	wv(terminal, "\r\n", outputMode)
	found := 0
	for _, r := range rd.Rumors {
		if userLevel < r.MinLevel {
			continue
		}
		match := strings.Contains(strings.ToUpper(r.Text), searchTerm) ||
			strings.Contains(strings.ToUpper(r.Author), searchTerm)
		if !match {
			continue
		}
		found++
		author := rumorDisplayAuthor(&r, isSysop, anonName)
		wv(terminal, fmt.Sprintf("|03%-4d|07%s |08by |11%s\r\n", r.ID, r.Text, author), outputMode)
	}

	if found == 0 {
		wv(terminal, "|07No matching rumors found.\r\n", outputMode)
	}

	return currentUser, "", nil
}

// runRumorsNewscan shows rumors posted since the user's last login.
// Maps to V2's RumorsNewscan procedure.
func runRumorsNewscan(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil {
		return currentUser, "", nil
	}

	slog.Debug("running RUMORSNEWSCAN", "node", nodeNumber, "handle", currentUser.Handle)

	// Clear screen before newscan
	wv(terminal, "\x1b[2J\x1b[H", outputMode)

	isSysop := currentUser.AccessLevel >= 255
	userLevel := currentUser.AccessLevel
	anonName := rumorAnonName(e)

	rumorsMu.Lock()
	rd, err := loadRumorsData(e.RootConfigPath)
	rumorsMu.Unlock()
	if err != nil {
		wv(terminal, "\r\n|04Error loading rumors.\r\n", outputMode)
		return currentUser, "", nil
	}

	wv(terminal, "\r\n|15Rumors Newscan\r\n|08"+strings.Repeat("\xc4", 50)+"\r\n", outputMode)

	// PreviousLogin, not LastLogin: the latter is stamped at authentication.
	lastLogin := currentUser.PreviousLogin
	found := 0
	for _, r := range rd.Rumors {
		if userLevel < r.MinLevel {
			continue
		}
		if !r.PostedAt.After(lastLogin) && !lastLogin.IsZero() {
			continue
		}
		found++
		author := rumorDisplayAuthor(&r, isSysop, anonName)
		wv(terminal, fmt.Sprintf("|03%-4d|07%s |08by |11%s\r\n", r.ID, r.Text, author), outputMode)
	}

	if found == 0 {
		wv(terminal, "|07No new rumors since your last login.\r\n", outputMode)
	}

	e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
	return currentUser, "", nil
}

// runRandomRumor displays a random rumor at login (V2: randomrumor from MAINR2.PAS).
// Intended for use in the login sequence.
func runRandomRumor(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	terminal := c.terminal
	currentUser := c.currentUser
	outputMode := c.outputMode
	termWidth := c.termWidth

	if currentUser == nil {
		return currentUser, "", nil
	}

	rumorsMu.Lock()
	rd, err := loadRumorsData(e.RootConfigPath)
	rumorsMu.Unlock()
	if err != nil {
		return currentUser, "", nil
	}

	visible := visibleRumors(rd, currentUser.AccessLevel)
	if len(visible) == 0 {
		return currentUser, "", nil
	}

	idx := visible[rand.Intn(len(visible))]
	r := &rd.Rumors[idx]

	// Center the rumor text (V2 centered it on 80-col screen)
	rumorText := r.Text
	displayWidth := ansi.VisibleLength(rumorText) + 4 // add brackets + spaces
	padding := 0
	tw := termWidth
	if tw <= 0 {
		tw = 80
	}
	if displayWidth < tw {
		padding = (tw - displayWidth) / 2
	}

	wv(terminal, "\r\n"+strings.Repeat(" ", padding)+"|07[ |15"+rumorText+" |07]\r\n", outputMode)

	return currentUser, "", nil
}
