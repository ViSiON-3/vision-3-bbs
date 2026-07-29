package menu

import (
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// readMenuInput implements Run's step 4 "Determine Input Mode / Method"
// dispatch. Run sets st.isLightbarMenu via HasBarFile immediately before
// calling this (that check stays inline in Run, out of scope here). For a
// lightbar-capable menu, this loads and draws the lightbar options and reads
// a selection via runLightbarInput, falling back to an inline
// prompt-and-readline path when option loading/drawing fails or no
// selection was made; for a non-lightbar menu it delegates entirely to
// readStandardInput.
//
// The three `st.isLightbarMenu = false` toggles (failed option load, empty
// option list, failed draw) and the drawLightbarMenu call itself move here
// verbatim, together and in their original relative order, exactly as they
// were inline in Run — this method is their only home now.
//
// The inline fallback path (deliverPendingPages/displayPrompt/
// readLineFromSessionIH, reached when the lightbar path yields no input) is
// deliberately NOT unified with readStandardInput: it has its own bare `err
// == io.EOF` check and no idle-timeout or ^P handling, unlike
// readStandardInput's errors.Is(err, io.EOF)/errors.Is(err,
// editor.ErrIdleTimeout)/^P logic. That distinction is preserved exactly, as
// it was a deliberate difference in the original code, not an oversight.
//
// act is loopReturn on any of the disconnect/error paths (from the lightbar
// input loop, the inline fallback prompt+read, or readStandardInput), with
// input/retErr carrying the exact values Run's original inline `return`
// statements produced for the caller — always paired with a literal nil
// user at the call site, matching the original code, since no
// user-mutating call happens anywhere in this part of Run. act is
// loopContinue when readStandardInput signals ^P back-navigation (its own
// st.currentMenuName/st.previousMenuName mutations already applied inside
// readStandardInput). Otherwise act is loopFallthrough and st.userInput
// already holds the final value for command matching, exactly as before.
func (st *runLoopState) readMenuInput(ansiProcessResult ansi.ProcessAnsiResult, menuRec *MenuRecord) (input string, act loopAction, retErr error) {
	e := st.e
	s := st.s
	terminal := st.terminal
	outputMode := st.outputMode
	userManager := st.userManager
	nodeNumber := st.nodeNumber
	sessionStartTime := st.sessionStartTime

	// 4. Determine Input Mode / Method
	if st.isLightbarMenu {
		slog.Debug("entering lightbar input mode", "menu", st.currentMenuName)

		// Load lightbar options from the config directory
		lightbarOptions, loadErr := loadLightbarOptions(st.currentMenuName, e)
		if loadErr != nil {
			slog.Error("failed to load lightbar options", "menu", st.currentMenuName, "error", loadErr)
			st.isLightbarMenu = false
		} else if len(lightbarOptions) == 0 {
			slog.Warn("no valid lightbar options loaded", "menu", st.currentMenuName)
			st.isLightbarMenu = false
		}

		if st.isLightbarMenu {
			cursorHidden := e.hideCursorIfNeeded(terminal, outputMode, cursorHideContextDefault)
			ansBackgroundBytes := ansiProcessResult.DisplayBytes

			// Initially draw with first option selected
			selectedIndex := 0
			drawErr := drawLightbarMenu(terminal, ansBackgroundBytes, lightbarOptions, selectedIndex, outputMode, false)
			if drawErr != nil {
				slog.Error("failed to draw lightbar menu", "menu", st.currentMenuName, "error", drawErr)
				e.showCursorIfHidden(terminal, outputMode, cursorHidden)
				st.isLightbarMenu = false
			} else {
				lightbarInput, act, retErr := st.runLightbarInput(lightbarOptions, cursorHidden)
				if act == loopReturn {
					return lightbarInput, loopReturn, retErr
				}
				// Set st.userInput to lightbar result if a selection was made
				if lightbarInput != "" {
					st.userInput = lightbarInput
				}
			}
		}

		if !st.isLightbarMenu || st.userInput == "" {
			// Fallback to standard input if lightbar loading failed or no valid selection made
			e.deliverPendingPages(terminal, nodeNumber, outputMode)
			// Display Prompt (Skip if USEPROMPT is false)
			if menuRec.GetUsePrompt() { // Condition changed: Only check UsePrompt
				err := e.displayPrompt(terminal, menuRec, st.currentUser, userManager, nodeNumber, st.currentMenuName, sessionStartTime, outputMode, st.currentAreaName) // Pass st.currentAreaName
				if err != nil {
					return "", loopReturn, err // Propagate the error
				}
			} else {
				// Log message remains the same, but the condition causing it is now just UsePrompt==false
				slog.Debug("skipping prompt display", "menu", st.currentMenuName, "usePrompt", menuRec.GetUsePrompt(), "prompt1Empty", menuRec.Prompt1 == "")
			}

			// Read User Input Line via shared InputHandler to avoid reader races.
			input, err := readLineFromSessionIH(s, terminal)
			if err != nil {
				if err == io.EOF {
					slog.Info("user disconnected during menu input", "menu", st.currentMenuName)
					return "LOGOFF", loopReturn, nil // Signal logoff
				}
				slog.Error("failed to read input for menu", "menu", st.currentMenuName, "error", err)
				return "", loopReturn, fmt.Errorf("failed reading input: %w", err)
			}
			st.userInput = strings.ToUpper(strings.TrimSpace(input))
			slog.Debug("user input", "input", st.userInput)
		}
	} else {
		input, act, retErr := st.readStandardInput(menuRec)
		switch act {
		case loopReturn:
			return input, loopReturn, retErr
		case loopContinue:
			return input, loopContinue, retErr
		}
	} // End if st.isLightbarMenu / else

	return st.userInput, loopFallthrough, nil
}
