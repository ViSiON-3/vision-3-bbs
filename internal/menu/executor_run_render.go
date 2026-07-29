package menu

import (
	"bytes"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
)

// renderMenuAnsi loads the ANSI file for the current menu, resolves
// conditional regions and pipe/token substitution, and processes it for
// display bytes and field coordinates, converting encoding based on
// outputMode. On a critical read/process error it writes a user-facing
// message to the terminal and returns the wrapped error; the caller (Run)
// is responsible for the top-level return.
func (st *runLoopState) renderMenuAnsi() (ansi.ProcessAnsiResult, error) {
	e := st.e
	terminal := st.terminal
	outputMode := st.outputMode
	userManager := st.userManager

	// Determine ANSI filename using standard convention
	ansFilename := st.currentMenuName + ".ANS"
	// Use MenuSetPath for ANSI file
	fullAnsPath := filepath.Join(e.MenuSetPath, "ansi", ansFilename)

	// Process the associated ANSI file to get display bytes and coordinates
	rawAnsiContent, readErr := ansi.GetAnsiFileContent(fullAnsPath)
	if readErr == nil {
		// Resolve {{acs}}...{{/}} conditional regions first, before any
		// |TOKEN substitution, so tokens inside hidden regions never expand.
		// Keyword conditions (e.g. {{SPONSOR}}) are resolved first against the
		// keywords map, then fall through to ACS evaluation. e.MessageMgr is
		// passed through a nil check so a nil *MessageManager never becomes a
		// non-nil areaLookup interface.
		var areas areaLookup
		if e.MessageMgr != nil {
			areas = e.MessageMgr
		}
		keywords := map[string]bool{
			"SPONSOR": sponsorKeyword(st.currentUser, areas, e.GetServerConfig()),
		}
		rawAnsiContent = applyConditionalRegions(rawAnsiContent, st.currentUser, keywords)
		if st.currentMenuName == "ADMIN" {
			pendingCount := pendingValidationCount(userManager)
			rawAnsiContent = bytes.ReplaceAll(rawAnsiContent, []byte("{{PENDING_VALIDATIONS}}"), []byte(strconv.Itoa(pendingCount)))
		}
		// Substitute global server-state placeholders before ANSI processing,
		// so multi-letter codes like |NEWUSERS aren't mis-parsed as coord markers.
		newUsersVal := "NO"
		if e.GetServerConfig().AllowNewUsers {
			newUsersVal = "YES"
		}
		rawAnsiContent = bytes.ReplaceAll(rawAnsiContent, []byte("|NEWUSERS"), []byte(newUsersVal))
		currentAreaTag, currentAreaDisplayName := e.resolveCurrentAreaTokens(st.currentUser, st.currentAreaName)
		currentFileAreaTag, currentFileAreaDisplayName := e.resolveCurrentFileAreaTokens(st.currentUser)
		// Replace longer tokens first to avoid partial replacement conflicts (e.g. |FCONFPATH, |CFAN vs |CFA vs |CAN vs |CA).
		rawAnsiContent = bytes.ReplaceAll(rawAnsiContent, []byte("|FCONFPATH"), []byte(e.resolveFileConferencePath(st.currentUser)))
		rawAnsiContent = bytes.ReplaceAll(rawAnsiContent, []byte("|CFAN"), []byte(currentFileAreaDisplayName))
		rawAnsiContent = bytes.ReplaceAll(rawAnsiContent, []byte("|CFA"), []byte(currentFileAreaTag))
		rawAnsiContent = bytes.ReplaceAll(rawAnsiContent, []byte("|CAN"), []byte(currentAreaDisplayName))
		rawAnsiContent = bytes.ReplaceAll(rawAnsiContent, []byte("|CA"), []byte(currentAreaTag))
		rawAnsiContent = replaceMenuATCode(rawAnsiContent, "UC", strconv.Itoa(userManager.GetUserCount()))
		rawAnsiContent = replaceMenuATCode(rawAnsiContent, "U", strconv.Itoa(e.SessionRegistry.ActiveCount()))
		// @RR@ — Random Rumor text (supports @RR@, @RR:50@, @RR######@)
		rumorLevel := 1 // default MinLevel when no user context
		if st.currentUser != nil {
			rumorLevel = st.currentUser.AccessLevel
		}
		rawAnsiContent = expandRandomRumorATCode(rawAnsiContent, e.RootConfigPath, rumorLevel)
	}
	var ansiProcessResult ansi.ProcessAnsiResult
	var processErr error
	if readErr != nil {
		slog.Error("failed to read ANSI file", "file", ansFilename, "error", readErr)
		// Display error message to user (using new helper)
		errMsg := fmt.Sprintf("\r\n|01Error reading screen file: %s|07\r\n", ansFilename)
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
		if wErr != nil {
			slog.Error("failed writing screen read error", "error", wErr)
		}
		// Reading the screen file is critical, return error
		return ansi.ProcessAnsiResult{}, fmt.Errorf("failed to read screen file %s: %w", ansFilename, readErr)
	}

	// Process for coords and display bytes
	// Use CP437 mode to keep raw bytes for coordinate tracking, then convert based on outputMode
	ansiProcessResult, processErr = ansi.ProcessAnsiAndExtractCoords(rawAnsiContent, ansi.OutputModeCP437)
	if processErr != nil {
		slog.Error("failed to process ANSI file, display may be incorrect", "file", ansFilename, "error", processErr)
		// Processing error is also critical, return error
		return ansi.ProcessAnsiResult{}, fmt.Errorf("failed to process screen file %s: %w", ansFilename, processErr)
	}

	// Convert encoding based on output mode (similar to SHOWSTATS fix)
	if outputMode == ansi.OutputModeUTF8 {
		// UTF-8 mode: Convert CP437 bytes to UTF-8 for proper display
		ansiProcessResult.DisplayBytes = ansi.CP437BytesToUTF8(ansiProcessResult.DisplayBytes)
	}
	// CP437 mode: DisplayBytes already contain raw CP437, pass through as-is

	return ansiProcessResult, nil
}

// displayMenuScreen truncates the processed ANSI to the terminal height and
// writes it, honoring menuRec.GetClrScrBefore(). It is a no-op for the LOGIN
// menu, which handles its own display before the interactive login prompt.
func (st *runLoopState) displayMenuScreen(res ansi.ProcessAnsiResult, menuRec *MenuRecord) error {
	if st.currentMenuName != "LOGIN" {
		// Truncate ANSI output to terminal height to prevent scrolling
		displayBytes := res.DisplayBytes
		// Prepend clear sequence when CLR is set (single write for reliable clearing)
		if menuRec.GetClrScrBefore() {
			displayBytes = append([]byte(ansi.ClearScreen()), displayBytes...)
		}
		if st.termHeight > 0 {
			lines := bytes.Split(displayBytes, []byte("\n"))
			if len(lines) > st.termHeight {
				displayBytes = bytes.Join(lines[:st.termHeight], []byte("\n"))
				slog.Debug("truncated menu ANSI to fit terminal", "menu", st.currentMenuName, "from", len(lines), "to", st.termHeight, "rows", st.termHeight)
			}
		}
		// For CP437 mode, write raw bytes directly to avoid UTF-8 false positives
		var wErr error
		if st.outputMode == ansi.OutputModeCP437 {
			_, wErr = st.terminal.Write(displayBytes)
		} else {
			wErr = terminalio.WriteProcessedBytes(st.terminal, displayBytes, st.outputMode)
		}
		if wErr != nil {
			slog.Error("failed writing ANSI screen", "menu", st.currentMenuName, "error", wErr)
			return fmt.Errorf("failed displaying screen: %w", wErr)
		}
	}
	return nil
}
