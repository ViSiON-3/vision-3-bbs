package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// displaySponsorHeader clears the screen (if configured) and displays the SPONSORM.ANS header.
func (e *MenuExecutor) displaySponsorHeader(terminal *term.Terminal, menuRec *MenuRecord, outputMode ansi.OutputMode, nodeNumber, termWidth, termHeight int) {
	if menuRec != nil && menuRec.GetClrScrBefore() {
		_ = terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	}
	if err := e.displayFile(terminal, "SPONSORM.ANS", outputMode, termWidth, termHeight); err != nil {
		slog.Warn("failed to display SPONSORM.ANS", "node", nodeNumber, "error", err)
	}
}

// runSponsorMenu is the handler for RUN:SPONSORMENU.
//
// Triggered by "%" in the Messages Menu. Sysop, co-sysop, or the named area
// sponsor may enter; all others are silently refused.
//
// Flow:
//  1. Resolve the user's current message area.
//  2. Gate via CanAccessSponsorMenu.
//  3. Display SPONSORM.ANS header.
//  4. Command loop (Enter required): E=Edit Area, [/]=Navigate Areas, P=Position, Q=Quit.
func runSponsorMenu(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil {
		return nil, "", nil
	}

	if e.MessageMgr == nil || currentUser.CurrentMessageAreaID == 0 {
		msg := "\r\n|03No message area selected.|07\r\n"
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		uiPause(1 * time.Second)
		return currentUser, "", nil
	}

	area, found := e.MessageMgr.GetAreaByID(currentUser.CurrentMessageAreaID)
	if !found {
		msg := "\r\n|03No message area selected.|07\r\n"
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		uiPause(1 * time.Second)
		return currentUser, "", nil
	}

	cfg := e.GetServerConfig()
	if !CanAccessSponsorMenu(currentUser, area, cfg) {
		slog.Info("user denied sponsor menu for area",
			"node", nodeNumber, "handle", currentUser.Handle, "tag", area.Tag)
		return currentUser, "", nil
	}

	slog.Info("user entering sponsor menu for area",
		"node", nodeNumber, "handle", currentUser.Handle, "tag", area.Tag)

	menuRec, loadErr := LoadMenu("SPONSORM", e.Menus())
	if loadErr != nil {
		slog.Warn("failed to load SPONSORM.MNU, using fallback prompt", "node", nodeNumber, "error", loadErr)
		menuRec = nil
	}

	// Display the sponsor menu header (clear screen + SPONSORM.ANS).
	e.displaySponsorHeader(terminal, menuRec, outputMode, nodeNumber, termWidth, termHeight)

	for {
		if menuRec != nil && menuRec.GetUsePrompt() {
			if err := e.displayPrompt(s, terminal, menuRec, currentUser, userManager, nodeNumber, "SPONSORM", sessionStartTime, outputMode, ""); err != nil {
				slog.Warn("displayPrompt failed for SPONSORM", "node", nodeNumber, "error", err)
			}
		} else {
			prompt := fmt.Sprintf("\r\n|15[|14%s|15] Sponsor: |11E|07=Edit  |11P|07=Position  |11[|07/|11]|07=Prev/Next  |11Q|07=Quit: ", area.Tag)
			_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)
		}

		input, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return currentUser, "", err
		}
		cmd := strings.ToUpper(strings.TrimSpace(input))

		switch cmd {
		case "E":
			updated, next, runErr := runSponsorEditArea(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, args)
			if runErr != nil {
				if errors.Is(runErr, io.EOF) {
					return nil, "LOGOFF", io.EOF
				}
				return updated, "", runErr
			}
			if next != "" {
				return updated, next, nil
			}
			currentUser = updated
			// Re-fetch area in case the edit updated its name/tag display.
			if a, ok := e.MessageMgr.GetAreaByID(currentUser.CurrentMessageAreaID); ok {
				area = a
			}
			// Redisplay the sponsor menu header on a clean screen after returning
			// from the edit area so the prompt doesn't appear on a dirty screen.
			e.displaySponsorHeader(terminal, menuRec, outputMode, nodeNumber, termWidth, termHeight)

		case "[", "]":
			forward := cmd == "]"
			sponsorAreas := getSponsorableAreasInConference(e, currentUser)
			if len(sponsorAreas) > 1 {
				currentIdx := -1
				for i, a := range sponsorAreas {
					if a.ID == currentUser.CurrentMessageAreaID {
						currentIdx = i
						break
					}
				}
				var newIdx int
				if currentIdx == -1 {
					newIdx = 0
				} else if forward {
					newIdx = (currentIdx + 1) % len(sponsorAreas)
				} else {
					newIdx = (currentIdx - 1 + len(sponsorAreas)) % len(sponsorAreas)
				}
				newArea := sponsorAreas[newIdx]
				currentUser.CurrentMessageAreaID = newArea.ID
				currentUser.CurrentMessageAreaTag = newArea.Tag
				if userManager != nil {
					if err := userManager.UpdateUser(currentUser); err != nil {
						slog.Error("failed to save user after sponsor area nav", "node", nodeNumber, "error", err)
					}
				} else {
					slog.Warn("userManager is nil; sponsor area nav not persisted", "node", nodeNumber)
				}
				area = newArea
				slog.Info("user sponsor-navigated to area", "node", nodeNumber, "handle", currentUser.Handle, "id", newArea.ID, "tag", newArea.Tag)
				e.displaySponsorHeader(terminal, menuRec, outputMode, nodeNumber, termWidth, termHeight)
			}

		case "P":
			// Use getSponsorableAreasInConference to enforce per-area authorization.
			confAreas := getSponsorableAreasInConference(e, currentUser)
			if len(confAreas) < 2 {
				msg := "\r\n|03Need at least 2 areas to reposition.|07\r\n"
				_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
				uiPause(1 * time.Second)
				break
			}

			confName := "Ungrouped"
			if e.ConferenceMgr != nil {
				if conf, ok := e.ConferenceMgr.GetByID(currentUser.CurrentMsgConferenceID); ok {
					confName = conf.Name
				}
			}

			// Position sub-menu loop — exits only on Q or empty input.
			showPositionList := true
			for {
				if showPositionList {
					// Refresh area list each iteration (positions may have changed)
					confAreas = getSponsorableAreasInConference(e, currentUser)

					_ = terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
					header := fmt.Sprintf("\r\n|14-- Area Positions: %s --|07\r\n\r\n", confName)
					_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(header)), outputMode)
					linesUsed := 3 // header uses 3 lines (blank + title + blank)
					pageSize := termHeight - 2

					for i, a := range confAreas {
						line := fmt.Sprintf("  |11%2d|07) |03%-16s |15%-24s|07\r\n",
							i+1, a.Tag, a.Name)
						_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line)), outputMode)
						linesUsed++
						if linesUsed >= pageSize && i < len(confAreas)-1 {
							e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
							linesUsed = 0
						}
					}
				}
				showPositionList = true // default: redraw on next iteration

				// Prompt: select area to move
				selPrompt := fmt.Sprintf("\r\n|15Select area to move (|111-%d|15, |11Q|15=Quit): |07",
					len(confAreas))
				_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(selPrompt)), outputMode)
				selInput, selErr := readLineFromSessionIH(s, terminal)
				if selErr != nil {
					if errors.Is(selErr, io.EOF) {
						return nil, "LOGOFF", io.EOF
					}
					break
				}
				selCmd := strings.ToUpper(strings.TrimSpace(selInput))
				if selCmd == "Q" || selCmd == "" {
					break // exit position sub-menu
				}

				var selIdx int
				if _, scanErr := fmt.Sscanf(selCmd, "%d", &selIdx); scanErr != nil || selIdx < 1 || selIdx > len(confAreas) {
					msg := "\r\n|01Invalid selection.|07\r\n"
					_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
					uiPause(1 * time.Second)
					continue
				}
				selectedArea := confAreas[selIdx-1]

				// Prompt: where to place it
				destPrompt := fmt.Sprintf("|15Place |14%s|15 before (|111-%d|15) or |11E|15=End: |07",
					selectedArea.Tag, len(confAreas))
				_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(destPrompt)), outputMode)
				destInput, destErr := readLineFromSessionIH(s, terminal)
				if destErr != nil {
					if errors.Is(destErr, io.EOF) {
						return nil, "LOGOFF", io.EOF
					}
					break
				}
				destCmd := strings.ToUpper(strings.TrimSpace(destInput))
				if destCmd == "" {
					// Cancelled — re-show list
					continue
				}

				var newPos int
				if destCmd == "E" {
					newPos = len(confAreas)
				} else {
					if _, scanErr := fmt.Sscanf(destCmd, "%d", &newPos); scanErr != nil || newPos < 1 || newPos > len(confAreas) {
						msg := "\r\n|01Invalid position.|07\r\n"
						_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
						uiPause(1 * time.Second)
						continue
					}
				}

				// No-op if already at that position. "Before the next one" is the
				// same slot too, and must not hop over areas the list leaves out.
				if newPos == selIdx || (destCmd != "E" && newPos == selIdx+1) {
					continue
				}

				// The list shown is only the areas this user may sponsor, but the
				// move indexes every area in the conference, so resolve the
				// destination against the full list.
				beforeIdx := newPos - 1
				if destCmd == "E" {
					beforeIdx = -1
				}
				targetPos := sponsorMoveTarget(
					getAllAreasInConference(e, selectedArea.ConferenceID), confAreas, selectedArea, beforeIdx)

				// Perform the move within this conference
				if moveErr := e.MessageMgr.MoveAreaPositionInConference(selectedArea.ID, targetPos); moveErr != nil {
					slog.Error("failed to move area position", "node", nodeNumber, "error", moveErr)
					msg := "\r\n|01Error moving area position.|07\r\n"
					_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
					uiPause(1 * time.Second)
					continue
				}
				if saveErr := e.MessageMgr.SaveAreas(); saveErr != nil {
					slog.Error("failed to save areas after reposition", "node", nodeNumber, "error", saveErr)
					msg := "\r\n|01Error saving areas.|07\r\n"
					_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
					uiPause(1 * time.Second)
					continue
				}
				slog.Info("user repositioned area",
					"node", nodeNumber, "handle", currentUser.Handle, "tag", selectedArea.Tag, "position", targetPos, "conference", confName)
				// Loop: list will refresh at top of next iteration
			}

			// Return to sponsor menu
			e.displaySponsorHeader(terminal, menuRec, outputMode, nodeNumber, termWidth, termHeight)

		case "?":
			// Reload menu
			e.displaySponsorHeader(terminal, menuRec, outputMode, nodeNumber, termWidth, termHeight)

		case "Q", "":
			_ = terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
			return currentUser, "", nil

		default:
			msg := "\r\n|01Invalid key.|07\r\n"
			_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			uiPause(1 * time.Second)
			e.displaySponsorHeader(terminal, menuRec, outputMode, nodeNumber, termWidth, termHeight)
		}
	}
}

// allowAnonEqual reports whether two *bool AllowAnon values are equal (both nil, or both non-nil with same value).
func allowAnonEqual(a, b *bool) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// runSponsorEditArea is the handler for RUN:SPONSOREDITAREA.
//
// Sequential field editor for the current message area. All MessageArea fields
// are editable except ID (which is immutable).
//
// Key map: T=Tag N=Name D=Description R=ACS Read W=ACS Write S=Sponsor
//
//	M=Max Msgs G=Max Age A=Allow Anon L=Real Name Only J=Auto Join
//	C=Conf ID B=Base Path Y=Area Type E=Echo Tag O=Origin K=Network
//	[/]=Prev/Next area (co-sysop+) Q=Save ESC=Cancel
//
// The Sponsor field is validated against the user database. Enter "-" to clear
// it or any other optional text field; Tag, Name and Base Path cannot be
// cleared. Conference ID must name an existing conference (or 0) and Area Type
// must be a recognised type.
func runSponsorEditArea(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	if currentUser == nil {
		return nil, "", nil
	}

	if e.MessageMgr == nil || currentUser.CurrentMessageAreaID == 0 {
		msg := "\r\n|03No message area selected.|07\r\n"
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		uiPause(1 * time.Second)
		return currentUser, "", nil
	}

	area, found := e.MessageMgr.GetAreaByID(currentUser.CurrentMessageAreaID)
	if !found {
		msg := "\r\n|03Area not found.|07\r\n"
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		uiPause(1 * time.Second)
		return currentUser, "", nil
	}

	cfg := e.GetServerConfig()
	if !CanAccessSponsorMenu(currentUser, area, cfg) {
		return currentUser, "", nil
	}

	// Work on a copy; apply to the live pointer only on save.
	edited := *area

	showAllowAnon := func() string {
		if edited.AllowAnon == nil {
			return "default"
		}
		if *edited.AllowAnon {
			return "yes"
		}
		return "no"
	}
	showFields := func() {
		var b strings.Builder
		b.WriteString("\r\n")
		fmt.Fprintf(&b, "|15Edit Area: |14%s|07 (ID %d)\r\n", edited.Tag, edited.ID)
		b.WriteString("|08────────────────────────────────────────────────────\r\n")
		fmt.Fprintf(&b, "|11T|07) Tag           : |15%s\r\n", edited.Tag)
		fmt.Fprintf(&b, "|11N|07) Name          : |15%s\r\n", edited.Name)
		fmt.Fprintf(&b, "|11D|07) Description   : |15%s\r\n", edited.Description)
		fmt.Fprintf(&b, "|11R|07) ACS Read      : |15%s\r\n", edited.ACSRead)
		fmt.Fprintf(&b, "|11W|07) ACS Write     : |15%s\r\n", edited.ACSWrite)
		fmt.Fprintf(&b, "|11S|07) Sponsor       : |15%s\r\n", edited.Sponsor)
		fmt.Fprintf(&b, "|11M|07) Max Messages  : |15%d\r\n", edited.MaxMessages)
		fmt.Fprintf(&b, "|11G|07) Max Age (days): |15%d\r\n", edited.MaxAge)
		fmt.Fprintf(&b, "|11A|07) Allow Anon    : |15%s\r\n", showAllowAnon())
		fmt.Fprintf(&b, "|11L|07) Real Name Only: |15%t\r\n", edited.RealNameOnly)
		fmt.Fprintf(&b, "|11J|07) Auto Join     : |15%t\r\n", edited.AutoJoin)
		fmt.Fprintf(&b, "|11C|07) Conference ID : |15%d\r\n", edited.ConferenceID)
		fmt.Fprintf(&b, "|11B|07) Base Path     : |15%s\r\n", edited.BasePath)
		fmt.Fprintf(&b, "|11Y|07) Area Type     : |15%s\r\n", edited.AreaType)
		fmt.Fprintf(&b, "|11E|07) Echo Tag      : |15%s\r\n", edited.EchoTag)
		fmt.Fprintf(&b, "|11O|07) Origin Addr   : |15%s\r\n", edited.OriginAddr)
		fmt.Fprintf(&b, "|11K|07) Network       : |15%s\r\n", edited.Network)
		b.WriteString("|08────────────────────────────────────────────────────\r\n")
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(b.String())), outputMode)
	}

	_ = terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	showFields()

	ih := getSessionIH(s)
	dirty := false

	// editPromptRow is the terminal row where the edit prompt appears.
	// Layout: row 1=blank, 2=header, 3=separator, 4..20=fields, 21=separator, 22=prompt.
	const editPromptRow = 22

	// refreshFieldRow redraws a single field row in-place using ANSI cursor
	// positioning, then clears the prompt area so the next prompt renders clean.
	refreshFieldRow := func(row int, pipeLine string) {
		pos := fmt.Sprintf("\033[%d;1H\033[2K", row)
		_ = terminalio.WriteProcessedBytes(terminal, []byte(pos), outputMode)
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(pipeLine)), outputMode)
		clear := fmt.Sprintf("\033[%d;1H\033[J", editPromptRow)
		_ = terminalio.WriteProcessedBytes(terminal, []byte(clear), outputMode)
	}

	for {
		// Position prompt at a fixed row so keypresses never scroll the display.
		promptPos := fmt.Sprintf("\033[%d;1H\033[J", editPromptRow)
		_ = terminalio.WriteProcessedBytes(terminal, []byte(promptPos), outputMode)

		prompt := "|07Edit (|11T|07|11N|07|11D|07|11R|07|11W|07|11S|07|11M|07|11G|07|11A|07|11L|07|11J|07|11C|07|11B|07|11Y|07|11E|07|11O|07|11K|07)"
		if currentUser.AccessLevel >= cfg.CoSysOpLevel {
			prompt += "  |11[|07/|11]|07=Prev/Next"
		}
		prompt += "  |11Q|07=Save/Quit  |03ESC|07=Cancel: "
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)

		key, err := ih.ReadKey()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return currentUser, "", err
		}

		// Clear prompt area for sub-prompts / messages; cursor stays at row 22.
		_ = terminalio.WriteProcessedBytes(terminal, []byte(promptPos), outputMode)

		switch key {
		case int('t'), int('T'):
			if currentUser.AccessLevel < cfg.CoSysOpLevel {
				msg := "|01Tag - sysop/co-sysop only.|07"
				_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
				uiPause(1 * time.Second)
				break
			}
			newVal := promptAreaField(s, terminal, outputMode, "Tag", edited.Tag, 32)
			if newVal != edited.Tag {
				dirty = true
				edited.Tag = newVal
			}
			// Tag also appears in the header row
			refreshFieldRow(2, fmt.Sprintf("|15Edit Area: |14%s|07 (ID %d)", edited.Tag, edited.ID))
			refreshFieldRow(4, fmt.Sprintf("|11T|07) Tag           : |15%s", edited.Tag))

		case int('n'), int('N'):
			newVal := promptAreaField(s, terminal, outputMode, "Name", edited.Name, 60)
			if newVal != edited.Name {
				dirty = true
				edited.Name = newVal
			}
			refreshFieldRow(5, fmt.Sprintf("|11N|07) Name          : |15%s", edited.Name))

		case int('d'), int('D'):
			newVal := promptClearableAreaField(s, terminal, outputMode,
				"Description", edited.Description, 80)
			if newVal != edited.Description {
				dirty = true
				edited.Description = newVal
			}
			refreshFieldRow(6, fmt.Sprintf("|11D|07) Description   : |15%s", edited.Description))

		case int('r'), int('R'):
			newVal := promptClearableAreaField(s, terminal, outputMode,
				"ACS Read", edited.ACSRead, 40)
			if newVal != edited.ACSRead {
				dirty = true
				edited.ACSRead = newVal
			}
			refreshFieldRow(7, fmt.Sprintf("|11R|07) ACS Read      : |15%s", edited.ACSRead))

		case int('w'), int('W'):
			newVal := promptClearableAreaField(s, terminal, outputMode,
				"ACS Write", edited.ACSWrite, 40)
			if newVal != edited.ACSWrite {
				dirty = true
				edited.ACSWrite = newVal
			}
			refreshFieldRow(8, fmt.Sprintf("|11W|07) ACS Write     : |15%s", edited.ACSWrite))

		case int('s'), int('S'):
			if currentUser.AccessLevel < cfg.CoSysOpLevel {
				msg := "|01Sponsor - sysop/co-sysop only.|07"
				_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
				uiPause(1 * time.Second)
				break
			}
			prevSponsor := edited.Sponsor
			newHandle := promptAreaField(s, terminal, outputMode,
				"Sponsor handle (- to clear)", edited.Sponsor, 30)
			switch {
			case newHandle == "-":
				edited.Sponsor = ""
			case newHandle != "":
				if userManager != nil {
					if _, exists := userManager.GetUser(newHandle); !exists {
						msg := fmt.Sprintf("|01User '%s' not found - sponsor unchanged.|07", newHandle)
						_ = terminalio.WriteProcessedBytes(terminal,
							ansi.ReplacePipeCodes([]byte(msg)), outputMode)
						uiPause(1 * time.Second)
					} else {
						edited.Sponsor = newHandle
					}
				} else {
					edited.Sponsor = newHandle
				}
			}
			if edited.Sponsor != prevSponsor {
				dirty = true
			}
			refreshFieldRow(9, fmt.Sprintf("|11S|07) Sponsor       : |15%s", edited.Sponsor))

		case int('m'), int('M'):
			prevMax := edited.MaxMessages
			raw := promptAreaField(s, terminal, outputMode,
				"Max Messages (0=unlimited)", fmt.Sprintf("%d", edited.MaxMessages), 10)
			if raw != "" {
				var n int
				if _, scanErr := fmt.Sscanf(raw, "%d", &n); scanErr == nil && n >= 0 {
					edited.MaxMessages = n
				} else {
					msg := "|01Invalid number - unchanged.|07"
					_ = terminalio.WriteProcessedBytes(terminal,
						ansi.ReplacePipeCodes([]byte(msg)), outputMode)
					uiPause(1 * time.Second)
				}
			}
			if edited.MaxMessages != prevMax {
				dirty = true
			}
			refreshFieldRow(10, fmt.Sprintf("|11M|07) Max Messages  : |15%d", edited.MaxMessages))

		case int('g'), int('G'):
			prevMaxAge := edited.MaxAge
			raw := promptAreaField(s, terminal, outputMode,
				"Max Age days (0=unlimited)", fmt.Sprintf("%d", edited.MaxAge), 6)
			if raw != "" {
				var n int
				if _, scanErr := fmt.Sscanf(raw, "%d", &n); scanErr == nil && n >= 0 {
					edited.MaxAge = n
				} else {
					msg := "|01Invalid number - unchanged.|07"
					_ = terminalio.WriteProcessedBytes(terminal,
						ansi.ReplacePipeCodes([]byte(msg)), outputMode)
					uiPause(1 * time.Second)
				}
			}
			if edited.MaxAge != prevMaxAge {
				dirty = true
			}
			refreshFieldRow(11, fmt.Sprintf("|11G|07) Max Age (days): |15%d", edited.MaxAge))

		case int('a'), int('A'):
			prevAllowAnon := edited.AllowAnon
			cur := "default"
			if edited.AllowAnon != nil {
				if *edited.AllowAnon {
					cur = "yes"
				} else {
					cur = "no"
				}
			}
			raw := promptAreaField(s, terminal, outputMode,
				"Allow Anonymous (yes/no/default)", cur, 10)
			raw = strings.ToLower(strings.TrimSpace(raw))
			if raw != "" {
				switch raw {
				case "y", "yes", "1", "true":
					t := true
					edited.AllowAnon = &t
				case "n", "no", "0", "false":
					f := false
					edited.AllowAnon = &f
				case "d", "default", "":
					edited.AllowAnon = nil
				default:
					msg := "|01Enter yes, no, or default.|07"
					_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
					uiPause(1 * time.Second)
				}
			}
			if !allowAnonEqual(prevAllowAnon, edited.AllowAnon) {
				dirty = true
			}
			refreshFieldRow(12, fmt.Sprintf("|11A|07) Allow Anon    : |15%s", showAllowAnon()))

		case int('l'), int('L'):
			prevRealName := edited.RealNameOnly
			cur := "no"
			if edited.RealNameOnly {
				cur = "yes"
			}
			raw := promptAreaField(s, terminal, outputMode,
				"Real Name Only (yes/no)", cur, 5)
			raw = strings.ToLower(strings.TrimSpace(raw))
			if raw != "" {
				edited.RealNameOnly = strings.HasPrefix(raw, "y") || raw == "1" || raw == "true"
			}
			if edited.RealNameOnly != prevRealName {
				dirty = true
			}
			refreshFieldRow(13, fmt.Sprintf("|11L|07) Real Name Only: |15%t", edited.RealNameOnly))

		case int('j'), int('J'):
			prevAutoJoin := edited.AutoJoin
			cur := "no"
			if edited.AutoJoin {
				cur = "yes"
			}
			raw := promptAreaField(s, terminal, outputMode,
				"Auto Join (yes/no)", cur, 5)
			raw = strings.ToLower(strings.TrimSpace(raw))
			if raw != "" {
				edited.AutoJoin = strings.HasPrefix(raw, "y") || raw == "1" || raw == "true"
			}
			if edited.AutoJoin != prevAutoJoin {
				dirty = true
			}
			refreshFieldRow(14, fmt.Sprintf("|11J|07) Auto Join     : |15%t", edited.AutoJoin))

		case int('c'), int('C'):
			if currentUser.AccessLevel < cfg.CoSysOpLevel {
				msg := "|01Conference ID - sysop/co-sysop only.|07"
				_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
				uiPause(1 * time.Second)
				break
			}
			prevConfID := edited.ConferenceID
			raw := promptAreaField(s, terminal, outputMode,
				"Conference ID (0=ungrouped)", fmt.Sprintf("%d", edited.ConferenceID), 6)
			if raw != "" {
				var n int
				if _, scanErr := fmt.Sscanf(raw, "%d", &n); scanErr != nil || n < 0 {
					msg := "|01Invalid number - unchanged.|07"
					_ = terminalio.WriteProcessedBytes(terminal,
						ansi.ReplacePipeCodes([]byte(msg)), outputMode)
					uiPause(1 * time.Second)
				} else if !e.conferenceExists(n) {
					msg := fmt.Sprintf("|01Conference %d does not exist - unchanged.|07", n)
					_ = terminalio.WriteProcessedBytes(terminal,
						ansi.ReplacePipeCodes([]byte(msg)), outputMode)
					uiPause(1 * time.Second)
				} else {
					edited.ConferenceID = n
				}
			}
			if edited.ConferenceID != prevConfID {
				dirty = true
			}
			refreshFieldRow(15, fmt.Sprintf("|11C|07) Conference ID : |15%d", edited.ConferenceID))

		case int('b'), int('B'):
			if currentUser.AccessLevel < cfg.CoSysOpLevel {
				msg := "|01Base Path - sysop/co-sysop only.|07"
				_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
				uiPause(1 * time.Second)
				break
			}
			newVal := promptAreaField(s, terminal, outputMode,
				"Base Path", edited.BasePath, 80)
			if newVal != edited.BasePath {
				dirty = true
				edited.BasePath = newVal
			}
			refreshFieldRow(16, fmt.Sprintf("|11B|07) Base Path     : |15%s", edited.BasePath))

		case int('y'), int('Y'):
			if currentUser.AccessLevel < cfg.CoSysOpLevel {
				msg := "|01Area Type - sysop/co-sysop only.|07"
				_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
				uiPause(1 * time.Second)
				break
			}
			newVal := promptAreaField(s, terminal, outputMode,
				"Area Type ("+strings.Join(sponsorAreaTypes, "/")+")", edited.AreaType, 16)
			if newVal != edited.AreaType {
				if canon, ok := canonicalAreaType(newVal); ok {
					if canon != edited.AreaType {
						dirty = true
						edited.AreaType = canon
					}
				} else {
					msg := fmt.Sprintf("|01Unknown area type '%s' - unchanged.|07", newVal)
					_ = terminalio.WriteProcessedBytes(terminal,
						ansi.ReplacePipeCodes([]byte(msg)), outputMode)
					uiPause(1 * time.Second)
				}
			}
			refreshFieldRow(17, fmt.Sprintf("|11Y|07) Area Type     : |15%s", edited.AreaType))

		case int('e'), int('E'):
			if currentUser.AccessLevel < cfg.CoSysOpLevel {
				msg := "|01Echo Tag - sysop/co-sysop only.|07"
				_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
				uiPause(1 * time.Second)
				break
			}
			newVal := promptClearableAreaField(s, terminal, outputMode,
				"Echo Tag", edited.EchoTag, 32)
			if newVal != edited.EchoTag {
				dirty = true
				edited.EchoTag = newVal
			}
			refreshFieldRow(18, fmt.Sprintf("|11E|07) Echo Tag      : |15%s", edited.EchoTag))

		case int('o'), int('O'):
			if currentUser.AccessLevel < cfg.CoSysOpLevel {
				msg := "|01Origin Address - sysop/co-sysop only.|07"
				_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
				uiPause(1 * time.Second)
				break
			}
			newVal := promptClearableAreaField(s, terminal, outputMode,
				"Origin Address", edited.OriginAddr, 32)
			if newVal != edited.OriginAddr {
				dirty = true
				edited.OriginAddr = newVal
			}
			refreshFieldRow(19, fmt.Sprintf("|11O|07) Origin Addr   : |15%s", edited.OriginAddr))

		case int('k'), int('K'):
			if currentUser.AccessLevel < cfg.CoSysOpLevel {
				msg := "|01Network - sysop/co-sysop only.|07"
				_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
				uiPause(1 * time.Second)
				break
			}
			newVal := promptClearableAreaField(s, terminal, outputMode,
				"Network", edited.Network, 32)
			if newVal != edited.Network {
				dirty = true
				edited.Network = newVal
			}
			refreshFieldRow(20, fmt.Sprintf("|11K|07) Network       : |15%s", edited.Network))

		case int('['), int(']'): // Prev/Next area navigation (co-sysop+)
			if currentUser.AccessLevel < cfg.CoSysOpLevel {
				break
			}
			// If dirty, prompt to save before switching
			if dirty {
				savePrompt := "|15Save changes before switching? (|11Y|15/|11N|15/|03ESC|15=Cancel): |07"
				_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(savePrompt)), outputMode)
				saveKey, saveErr := ih.ReadKey()
				if saveErr != nil {
					if errors.Is(saveErr, io.EOF) {
						return nil, "LOGOFF", io.EOF
					}
					break
				}
				_ = terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
				saveOK := false
				switch saveKey {
				case int('y'), int('Y'):
					if updateErr := e.MessageMgr.UpdateAreaByID(edited.ID, edited); updateErr != nil {
						slog.Error("failed to update area", "node", nodeNumber, "error", updateErr)
						msg := "|01Error updating area.|07\r\n"
						_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
						uiPause(1 * time.Second)
					} else if saveErr := e.MessageMgr.SaveAreas(); saveErr != nil {
						slog.Error("failed to save areas", "node", nodeNumber, "error", saveErr)
						msg := "|01Error saving area.|07\r\n"
						_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
						uiPause(1 * time.Second)
					} else {
						slog.Info("user saved area", "node", nodeNumber, "handle", currentUser.Handle, "tag", edited.Tag)
						saveMsg := fmt.Sprintf("|02Area |14%s|02 saved.|07\r\n", edited.Tag)
						_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(saveMsg)), outputMode)
						uiPause(500 * time.Millisecond)
						syncCurrentAreaTag(userManager, currentUser, &edited, nodeNumber)
						saveOK = true
					}
				case int('n'), int('N'):
					saveOK = true // Discard is intentional, allow navigation
				default:
					// Cancel navigation
				}
				if !saveOK {
					break
				}
			}

			// Find navigable areas in this conference
			forward := key == int(']')
			navAreas := getAllAreasInConference(e, currentUser.CurrentMsgConferenceID)
			if len(navAreas) < 2 {
				break
			}
			currentNavIdx := -1
			for i, a := range navAreas {
				if a.ID == area.ID {
					currentNavIdx = i
					break
				}
			}
			if currentNavIdx == -1 {
				break
			}
			var newNavIdx int
			if forward {
				newNavIdx = (currentNavIdx + 1) % len(navAreas)
			} else {
				newNavIdx = (currentNavIdx - 1 + len(navAreas)) % len(navAreas)
			}
			newArea := navAreas[newNavIdx]

			// Switch to the new area
			area = newArea
			edited = *newArea
			dirty = false
			currentUser.CurrentMessageAreaID = newArea.ID
			currentUser.CurrentMessageAreaTag = newArea.Tag
			if userManager != nil {
				if persistErr := userManager.UpdateUser(currentUser); persistErr != nil {
					slog.Error("failed to persist user after area nav", "node", nodeNumber, "error", persistErr)
				}
			}
			slog.Info("user editor-navigated to area",
				"node", nodeNumber, "handle", currentUser.Handle, "id", newArea.ID, "tag", newArea.Tag)
			_ = terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
			showFields()

		case int('q'), int('Q'): // Q = save and quit (no-op if nothing changed)
			if !dirty {
				return currentUser, "", nil
			}
			// Capture a value snapshot for rollback before applying the update.
			var prevAreaSnapshot message.MessageArea
			hasPrevArea := false
			if prevArea, ok := e.MessageMgr.GetAreaByID(edited.ID); ok && prevArea != nil {
				prevAreaSnapshot = *prevArea
				hasPrevArea = true
			}
			if updateErr := e.MessageMgr.UpdateAreaByID(edited.ID, edited); updateErr != nil {
				slog.Error("failed to update area", "node", nodeNumber, "error", updateErr)
				msg := "|01Error updating area - changes may be lost.|07\r\n"
				_ = terminalio.WriteProcessedBytes(terminal,
					ansi.ReplacePipeCodes([]byte(msg)), outputMode)
				uiPause(2 * time.Second)
				return currentUser, "", nil
			}
			if saveErr := e.MessageMgr.SaveAreas(); saveErr != nil {
				slog.Error("failed to save areas", "node", nodeNumber, "error", saveErr)
				msg := "|01Error saving area - changes may be lost.|07\r\n"
				_ = terminalio.WriteProcessedBytes(terminal,
					ansi.ReplacePipeCodes([]byte(msg)), outputMode)
				uiPause(2 * time.Second)
				// Roll back in-memory state so edited state is not left applied without disk persist.
				if hasPrevArea {
					_ = e.MessageMgr.UpdateAreaByID(edited.ID, prevAreaSnapshot)
				}
			} else {
				slog.Info("user saved area", "node", nodeNumber, "handle", currentUser.Handle, "tag", edited.Tag)
				msg := fmt.Sprintf("|02Area |14%s|02 saved.|07\r\n", edited.Tag)
				_ = terminalio.WriteProcessedBytes(terminal,
					ansi.ReplacePipeCodes([]byte(msg)), outputMode)
				uiPause(500 * time.Millisecond)
				syncCurrentAreaTag(userManager, currentUser, &edited, nodeNumber)
			}
			return currentUser, "", nil

		case 27: // ESC = discard and quit
			msg := "|03Changes discarded.|07\r\n"
			_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			uiPause(500 * time.Millisecond)
			return currentUser, "", nil
		}
	}
}

// syncCurrentAreaTag updates the user's cached current-area tag after area has
// been saved, and saves the user when it changed so users.json never points at
// a tag that no longer exists.
func syncCurrentAreaTag(userManager *user.UserMgr, currentUser *user.User, area *message.MessageArea, nodeNumber int) {
	if currentUser.CurrentMessageAreaID != area.ID || currentUser.CurrentMessageAreaTag == area.Tag {
		return
	}
	currentUser.CurrentMessageAreaTag = area.Tag
	if userManager == nil {
		slog.Warn("userManager is nil; renamed area tag not persisted", "node", nodeNumber)
		return
	}
	if err := userManager.UpdateUser(currentUser); err != nil {
		slog.Error("failed to save user after area tag change", "node", nodeNumber, "error", err)
	}
}

// getSponsorableAreasInConference returns all areas in the user's current
// conference where the user has sponsor menu access (sysop, co-sysop, or
// named sponsor), sorted by Position.
func getSponsorableAreasInConference(e *MenuExecutor, currentUser *user.User) []*message.MessageArea {
	cfg := e.GetServerConfig()
	var result []*message.MessageArea
	for _, area := range e.MessageMgr.ListAreas() {
		if area.ConferenceID != currentUser.CurrentMsgConferenceID {
			continue
		}
		if !CanAccessSponsorMenu(currentUser, area, cfg) {
			continue
		}
		result = append(result, area)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Position < result[j].Position
	})
	return result
}

// sponsorMoveTarget converts a destination picked in the sponsor's filtered
// area list into the 1-based index MoveAreaPositionInConference expects, which
// counts every area in the conference once moving is taken out.
//
// all is every area in the conference and listed is the subset shown to the
// user, both in position order. beforeIdx is the 0-based index in listed of the
// area to place moving before, or -1 to place it after the last listed area.
// Areas the user cannot see keep their order relative to each other.
func sponsorMoveTarget(all, listed []*message.MessageArea, moving *message.MessageArea, beforeIdx int) int {
	rest := make([]*message.MessageArea, 0, len(all))
	for _, a := range all {
		if a.ID != moving.ID {
			rest = append(rest, a)
		}
	}
	indexOf := func(id int) int {
		for i, a := range rest {
			if a.ID == id {
				return i
			}
		}
		return -1
	}

	if beforeIdx >= 0 && beforeIdx < len(listed) {
		if i := indexOf(listed[beforeIdx].ID); i >= 0 {
			return i + 1
		}
		return len(rest) + 1
	}
	// End: directly after the last listed area other than the one moving.
	for j := len(listed) - 1; j >= 0; j-- {
		if listed[j].ID == moving.ID {
			continue
		}
		if i := indexOf(listed[j].ID); i >= 0 {
			return i + 2
		}
	}
	return len(rest) + 1
}

// getAllAreasInConference returns all areas in the given conference, sorted by Position.
// No ACS or sponsor filtering — used for repositioning where the caller already
// passed the sponsor menu access gate.
func getAllAreasInConference(e *MenuExecutor, conferenceID int) []*message.MessageArea {
	var result []*message.MessageArea
	for _, area := range e.MessageMgr.ListAreas() {
		if area.ConferenceID != conferenceID {
			continue
		}
		result = append(result, area)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Position < result[j].Position
	})
	return result
}

// promptAreaField prints a prompt, reads a line, and returns the new value.
// If the user presses Enter with no input, the original value is returned
// unchanged.
func promptAreaField(s ssh.Session, terminal *term.Terminal,
	outputMode ansi.OutputMode, label, current string, maxLen int) string {

	prompt := fmt.Sprintf("|15%s|07 [|11%s|07]: ", label, current)
	_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)

	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return current
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return current
	}
	if runes := []rune(input); len(runes) > maxLen {
		input = string(runes[:maxLen])
	}
	return input
}

// promptClearableAreaField is promptAreaField for an optional text field: the
// label says so, and a reply of "-" clears the value.
func promptClearableAreaField(s ssh.Session, terminal *term.Terminal,
	outputMode ansi.OutputMode, label, current string, maxLen int) string {

	newVal := promptAreaField(s, terminal, outputMode, label+" (- to clear)", current, maxLen)
	if newVal == "-" {
		return ""
	}
	return newVal
}

// sponsorAreaTypes are the message area types the area editor accepts, the
// same set the config editor offers.
var sponsorAreaTypes = []string{"local", "echomail", "netmail", "v3net", message.AreaTypeQWKNet}

// canonicalAreaType returns the recognised area type matching v, ignoring
// case and surrounding space, and whether there was one.
func canonicalAreaType(v string) (string, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	for _, t := range sponsorAreaTypes {
		if v == t {
			return t, true
		}
	}
	return "", false
}

// conferenceExists reports whether id names a configured conference. 0 is
// always valid: it means the area is ungrouped.
func (e *MenuExecutor) conferenceExists(id int) bool {
	if id == 0 {
		return true
	}
	if e.ConferenceMgr == nil {
		return false
	}
	_, ok := e.ConferenceMgr.GetByID(id)
	return ok
}
