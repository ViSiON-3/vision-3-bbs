package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runRumorsAdd lets users post a new rumor.
// Maps to V2's AddRumor procedure.
func runRumorsAdd(c *cmdCtx, args string) (*user.User, string, error) {
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

	slog.Debug("running RUMORSADD", "node", nodeNumber, "handle", currentUser.Handle)

	userLevel := currentUser.AccessLevel
	anonName := rumorAnonName(e)

	if userLevel < 2 {
		wv(terminal, "\r\n|04You need at least level 2 to add rumors.\r\n", outputMode)
		return currentUser, "", nil
	}

	// Check max rumors (V2: 999 limit)
	rumorsMu.Lock()
	rd, err := loadRumorsData(e.RootConfigPath)
	rumorsMu.Unlock()
	if err != nil {
		wv(terminal, "\r\n|04Error loading rumors.\r\n", outputMode)
		return currentUser, "", nil
	}
	if len(rd.Rumors) >= 999 {
		wv(terminal, "\r\n|04Sorry, there are too many rumors! Ask the SysOp to delete some.\r\n", outputMode)
		return currentUser, "", nil
	}

	// Anonymous option (V2: only if user level >= AnonymousLevel)
	author := currentUser.Handle
	realUser := currentUser.Handle
	allowAnon := userLevel >= e.ServerCfg.AnonymousLevel
	if allowAnon {
		anonPrompt := e.LoadedStrings.AddRumorAnonymous
		if anonPrompt == "" {
			anonPrompt = "|09Anonymous? @"
		}
		anonYes, anonErr := e.PromptYesNo(s, terminal, anonPrompt, outputMode, nodeNumber, termWidth, termHeight, false)
		if anonErr != nil {
			if errors.Is(anonErr, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
		} else if anonYes {
			author = anonName
		}
	}

	// Min level to see (V2: Level_To_See_Rumor)
	wv(terminal, "|08Minimum security level required to view this rumor |07(|151-255|07, |15Enter|07=1|07)\r\n", outputMode)
	levelPrompt := e.LoadedStrings.EnterRumorLevel
	if levelPrompt == "" {
		levelPrompt = "|09Level|08 : "
	}
	minLevel := 1
	for {
		wv(terminal, levelPrompt, outputMode)
		levelInput, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return currentUser, "", nil
		}
		if strings.TrimSpace(levelInput) == "" {
			break // default to 1
		}
		v, nerr := strconv.Atoi(strings.TrimSpace(levelInput))
		if nerr != nil || v < 1 || v > 255 {
			wv(terminal, "|04Invalid level. Enter a number from 1-255.\r\n", outputMode)
			continue
		}
		minLevel = v
		break
	}

	// Rumor text
	enterPrompt := e.LoadedStrings.EnterRumorPrompt
	if enterPrompt == "" {
		enterPrompt = "|09Enter Rumor |08(|15Enter|08/|15Abort|08)|07:\r\n"
	}
	wv(terminal, enterPrompt, outputMode)
	rumorText, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return currentUser, "", nil
	}
	if strings.TrimSpace(rumorText) == "" {
		return currentUser, "", nil
	}

	newRumor := RumorRecord{
		Author:   author,
		RealUser: realUser,
		UserID:   currentUser.ID,
		Text:     rumorSanitize(rumorText),
		PostedAt: time.Now().UTC(),
		MinLevel: minLevel,
	}

	rumorsMu.Lock()
	rd, err = loadRumorsData(e.RootConfigPath)
	if err != nil {
		rumorsMu.Unlock()
		wv(terminal, "\r\n|04Error saving rumor.\r\n", outputMode)
		return currentUser, "", nil
	}
	if len(rd.Rumors) >= 999 {
		rumorsMu.Unlock()
		wv(terminal, "\r\n|04Sorry, there are too many rumors! Ask the SysOp to delete some.\r\n", outputMode)
		return currentUser, "", nil
	}
	newRumor.ID = rd.NextID
	rd.NextID++
	rd.Rumors = append(rd.Rumors, newRumor)
	saveErr := saveRumorsData(e.RootConfigPath, rd)
	rumorsMu.Unlock()

	if saveErr != nil {
		slog.Error("failed to save rumor", "node", nodeNumber, "error", saveErr)
		wv(terminal, "\r\n|04Error saving rumor.\r\n", outputMode)
		return currentUser, "", nil
	}

	addedMsg := e.LoadedStrings.RumorAdded
	if addedMsg == "" {
		addedMsg = "|10Rumor has been added!"
	}
	wv(terminal, "\r\n"+addedMsg+"\r\n", outputMode)
	time.Sleep(1 * time.Second)
	slog.Info("rumor added", "node", nodeNumber, "handle", currentUser.Handle, "id", newRumor.ID)
	return currentUser, "", nil
}

// runRumorsDelete lets users delete their own rumors; sysops can delete any.
// Maps to V2's DeleteRumor procedure.
func runRumorsDelete(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil {
		return currentUser, "", nil
	}

	slog.Debug("running RUMORSDELETE", "node", nodeNumber, "handle", currentUser.Handle)

	isSysop := currentUser.AccessLevel >= 255
	userLevel := currentUser.AccessLevel
	anonName := rumorAnonName(e)

	rumorsMu.Lock()
	rd, err := loadRumorsData(e.RootConfigPath)
	if err == nil && backfillRumorUserIDs(rd, userManager) {
		_ = saveRumorsData(e.RootConfigPath, rd) // best-effort migration; non-fatal
	}
	rumorsMu.Unlock()
	if err != nil {
		wv(terminal, "\r\n|04Error loading rumors.\r\n", outputMode)
		return currentUser, "", nil
	}

	visible := visibleRumors(rd, userLevel)
	if len(visible) == 0 {
		wv(terminal, "\r\n|07No rumors to delete.\r\n", outputMode)
		return currentUser, "", nil
	}

	wv(terminal, "\r\n|07Rumor number to delete [?=List]: ", outputMode)
	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return currentUser, "", nil
	}
	if strings.TrimSpace(input) == "" {
		return currentUser, "", nil
	}

	if strings.TrimSpace(input) == "?" {
		for _, idx := range visible {
			r := &rd.Rumors[idx]
			author := rumorDisplayAuthor(r, isSysop, anonName)
			wv(terminal, fmt.Sprintf("|03%-4d|07%-50s |11%s\r\n", r.ID, truncateRunes(r.Text, 48), author), outputMode)
		}
		wv(terminal, "\r\n|07Rumor number to delete: ", outputMode)
		input, err = readLineFromSessionIH(s, terminal)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return currentUser, "", nil
		}
		if strings.TrimSpace(input) == "" {
			return currentUser, "", nil
		}
	}

	num, nerr := strconv.Atoi(strings.TrimSpace(input))
	if nerr != nil {
		wv(terminal, "\r\n|04Invalid number.\r\n", outputMode)
		return currentUser, "", nil
	}

	// Find rumor by ID
	rumorIdx := -1
	for i, r := range rd.Rumors {
		if r.ID == num {
			rumorIdx = i
			break
		}
	}
	if rumorIdx < 0 {
		wv(terminal, "\r\n|04Rumor not found.\r\n", outputMode)
		return currentUser, "", nil
	}

	r := &rd.Rumors[rumorIdx]
	if userLevel < r.MinLevel {
		wv(terminal, "\r\n|04Rumor not found.\r\n", outputMode)
		return currentUser, "", nil
	}

	// Ownership check (V2: only sysop or author can delete).
	// Prefer stable UserID for newer records; fall back to handle comparison for legacy entries.
	if !isSysop {
		var isOwner bool
		if r.UserID != 0 {
			isOwner = r.UserID == currentUser.ID
		} else {
			isOwner = strings.EqualFold(r.RealUser, currentUser.Handle)
		}
		if !isOwner {
			wv(terminal, "\r\n|04You didn't post that!\r\n", outputMode)
			return currentUser, "", nil
		}
	}

	// Confirm
	wv(terminal, fmt.Sprintf("\r\n|07%s\r\n", r.Text), outputMode)
	delYes, delErr := e.PromptYesNo(s, terminal, "|09Delete this rumor? @", outputMode, nodeNumber, termWidth, termHeight, false)
	if delErr != nil || !delYes {
		return currentUser, "", nil
	}

	rumorsMu.Lock()
	rd, err = loadRumorsData(e.RootConfigPath)
	if err != nil {
		rumorsMu.Unlock()
		return currentUser, "", nil
	}
	for i, rr := range rd.Rumors {
		if rr.ID == num {
			rd.Rumors = append(rd.Rumors[:i], rd.Rumors[i+1:]...)
			break
		}
	}
	saveErr := saveRumorsData(e.RootConfigPath, rd)
	rumorsMu.Unlock()

	if saveErr != nil {
		slog.Error("failed to delete rumor", "node", nodeNumber, "id", num, "error", saveErr)
		wv(terminal, "\r\n|04Error deleting rumor.\r\n", outputMode)
		return currentUser, "", nil
	}

	wv(terminal, "\r\n|10Rumor deleted.\r\n", outputMode)
	slog.Info("rumor deleted", "node", nodeNumber, "handle", currentUser.Handle, "id", num)
	return currentUser, "", nil
}
