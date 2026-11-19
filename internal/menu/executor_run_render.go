package menu

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strconv"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
)

// renderMenuAnsi loads the ANSI file for the current menu, resolves
// conditional regions and pipe/token substitution, and processes it for
// display bytes and field coordinates, converting encoding based on
// outputMode. A screen file that cannot be read is a *screenReadError, which
// Run passes to screenUnreadable to tell the user; a processing
// error is returned wrapped.
func (st *runLoopState) renderMenuAnsi() (ansi.ProcessAnsiResult, error) {
	e := st.e
	terminal := st.terminal
	outputMode := st.outputMode
	userManager := st.userManager

	// Determine ANSI filename using standard convention
	ansFilename := st.currentMenuName + ".ANS"
	// Use MenuSetPath for ANSI file
	fullAnsPath := e.menuFile("ansi", ansFilename)

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
		keywords := resolveRegionKeywords(st.currentUser, areas, e.GetServerConfig())
		rawAnsiContent = applyConditionalRegions(rawAnsiContent, st.currentUser, st.s, st.sessionStartTime, keywords)
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
		// The menu art is raw CP437 until it is converted for output below, so
		// every value substituted into it is converted to CP437 first. A UTF-8
		// value left as it is would be converted a second time with the art.
		currentAreaTag, currentAreaDisplayName := e.resolveCurrentAreaTokens(st.currentUser, st.currentAreaName)
		currentFileAreaTag, currentFileAreaDisplayName := e.resolveCurrentFileAreaTokens(st.currentUser)
		// Replace longer tokens first to avoid partial replacement conflicts (e.g. |FCONFPATH, |CFAN vs |CFA vs |CAN vs |CA).
		rawAnsiContent = bytes.ReplaceAll(rawAnsiContent, []byte("|FCONFPATH"), []byte(toCP437Safe(e.resolveFileConferencePath(st.currentUser))))
		rawAnsiContent = bytes.ReplaceAll(rawAnsiContent, []byte("|CFAN"), []byte(toCP437Safe(currentFileAreaDisplayName)))
		rawAnsiContent = bytes.ReplaceAll(rawAnsiContent, []byte("|CFA"), []byte(toCP437Safe(currentFileAreaTag)))
		rawAnsiContent = bytes.ReplaceAll(rawAnsiContent, []byte("|CAN"), []byte(toCP437Safe(currentAreaDisplayName)))
		rawAnsiContent = bytes.ReplaceAll(rawAnsiContent, []byte("|CA"), []byte(toCP437Safe(currentAreaTag)))
		rawAnsiContent = replaceMenuATCode(rawAnsiContent, "UC", strconv.Itoa(userManager.GetUserCount()))
		rawAnsiContent = replaceMenuATCode(rawAnsiContent, "U", strconv.Itoa(e.activeNodeCount()))
		if hasNetOnlineCode(rawAnsiContent) {
			rawAnsiContent = replaceMenuATCode(rawAnsiContent, "UN", strconv.Itoa(e.v3netOnlineCount()))
		}
		// @RR@ — Random Rumor text (supports @RR@, @RR:50@, @RR######@)
		rumorLevel := 1 // default MinLevel when no user context
		if st.currentUser != nil {
			rumorLevel = st.currentUser.AccessLevel
		}
		rawAnsiContent = expandRandomRumorATCode(rawAnsiContent, e.dataDir(), rumorLevel, true)
	}
	var ansiProcessResult ansi.ProcessAnsiResult
	var processErr error
	if readErr != nil {
		// Reading the screen file is critical; Run reports it with
		// screenUnreadable unless the menu's script draws the menu.
		return ansi.ProcessAnsiResult{}, &screenReadError{file: ansFilename, err: readErr}
	}

	// Process for coords and display bytes
	// Use CP437 mode to keep raw bytes for coordinate tracking, then convert based on outputMode
	ansiProcessResult, processErr = ansi.ProcessAnsiAndExtractCoords(rawAnsiContent, ansi.OutputModeCP437)
	if processErr != nil {
		slog.Error("failed to process ANSI file, display may be incorrect", "file", ansFilename, "error", processErr)
		// Processing error is also critical, return error
		return ansi.ProcessAnsiResult{}, fmt.Errorf("failed to process screen file %s: %w", ansFilename, processErr)
	}

	// Art wraps at the terminal's edge, or at ArtWidth once fitArt has made that
	// wrap explicit for a wider terminal.
	wrapToastAnchor(&ansiProcessResult, rawAnsiContent, min(artWidth(terminal, st.termWidth), ansi.ArtWidth))

	// DisplayBytes are still raw CP437 here, one byte per cell, so measure
	// them as such before any UTF-8 conversion.
	ansiProcessResult.DisplayBytes = fitArt(terminal, ansiProcessResult.DisplayBytes, st.termWidth, false)
	// Whether the art scrolls the screen is measured here too, while each
	// byte is one cell: ArtGeometry would count a UTF-8 box character as three.
	st.artScrolls = menuArtScrolls(ansiProcessResult.DisplayBytes, artWidth(terminal, st.termWidth), st.termHeight)

	// Convert encoding based on output mode (similar to SHOWSTATS fix)
	if outputMode == ansi.OutputModeUTF8 {
		// UTF-8 mode: Convert CP437 bytes to UTF-8 for proper display, glyph
		// control bytes (▲ ▼ …) included
		ansiProcessResult.DisplayBytes = ansi.DrawCP437ControlGlyphs(ansi.CP437BytesToUTF8(ansiProcessResult.DisplayBytes))
	}
	// CP437 mode: DisplayBytes already contain raw CP437, pass through as-is

	return ansiProcessResult, nil
}

// screenReadError is a menu screen file that could not be read.
type screenReadError struct {
	file string
	err  error
}

func (e *screenReadError) Error() string {
	return fmt.Sprintf("failed to read screen file %s: %v", e.file, e.err)
}

func (e *screenReadError) Unwrap() error { return e.err }

// screenUnreadable writes the user-facing message for a screen file that
// could not be read and returns err, the run loop's error. Other errors are
// returned as they are.
func (st *runLoopState) screenUnreadable(err error) error {
	var readErr *screenReadError
	if errors.As(err, &readErr) {
		slog.Error("failed to read ANSI file", "file", readErr.file, "error", readErr.err)
		errMsg := fmt.Sprintf("\r\n|01Error reading screen file: %s|07\r\n", readErr.file)
		if wErr := terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte(errMsg)), st.outputMode); wErr != nil {
			slog.Error("failed writing screen read error", "error", wErr)
		}
	}
	return err
}

// backFromMissingScreen sends a logged-in caller back to the menu they came
// from when err, from screenUnreadable, is a menu screen that does not exist:
// the error line is already on their screen, so they are asked to
// press Enter, and the run carries on there. It reports false, and the run
// ends with err as before, for any other error, before login, on the LOGIN
// menu, when there is no menu to go back to, or when the caller disconnects
// at the pause. The way back is used once: if the menu they return to has
// lost its screen too, the run ends.
func (st *runLoopState) backFromMissingScreen(err error) bool {
	var readErr *screenReadError
	if !errors.As(err, &readErr) || !errors.Is(err, fs.ErrNotExist) {
		return false
	}
	back := st.previousMenuName
	if st.currentUser == nil || st.currentMenuName == "LOGIN" || back == "" || back == st.currentMenuName {
		return false
	}
	if perr := st.e.pauseHere(st.s, st.terminal, st.outputMode); perr != nil {
		return false
	}
	slog.Warn("menu screen missing; caller sent back", "menu", st.currentMenuName, "back", back)
	st.pending = nil
	st.currentMenuName, st.previousMenuName = back, ""
	return true
}

// scriptMayDraw reports whether the current menu has a script that can run
// for the caller (menuScriptReady) and would draw it in place of its screen,
// so a missing screen need not end the run before the script is tried. No
// script runs for the next command of a chain, and LOGIN uses its screen
// before any script.
func (st *runLoopState) scriptMayDraw() bool {
	if st.currentMenuName == "LOGIN" || len(st.pending) > 0 {
		return false
	}
	rec, err := LoadMenu(st.currentMenuName, st.e.Menus())
	if err != nil {
		return false
	}
	_, _, _, ok := st.menuScriptReady(rec, true)
	return ok
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
