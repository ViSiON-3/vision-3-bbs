package menu

import (
	"log/slog"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// readMenuInput implements Run's step 4 "Determine Input Mode / Method"
// dispatch. Run sets st.isLightbarMenu via HasBarFile immediately before
// calling this (that check stays inline in Run, out of scope here). For a
// lightbar-capable menu, this loads and draws the lightbar options and reads
// a selection via runLightbarInput, falling back to readStandardInput when
// option loading/drawing fails or no selection was made; for a non-lightbar
// menu it delegates to readStandardInput directly.
//
// The three `st.isLightbarMenu = false` toggles (failed option load, empty
// option list, failed draw) and the drawLightbarMenu call itself move here
// verbatim, together and in their original relative order, exactly as they
// were inline in Run — this method is their only home now.
//
// The lightbar fallback path previously duplicated readStandardInput inline,
// with a bare `err == io.EOF` check and no idle-timeout or ^P handling. That
// duplication meant a user idling at the fallback prompt of a .BAR menu was
// never timed out. Both paths now share readStandardInput.
//
// act is loopReturn on any of the disconnect/error paths (from the lightbar
// input loop or readStandardInput), with input/retErr carrying the values the
// caller returns. act is loopContinue when readStandardInput signals ^P
// back-navigation (its own st.currentMenuName/st.previousMenuName mutations
// already applied inside readStandardInput). Otherwise act is loopFallthrough
// and st.userInput already holds the final value for command matching.
func (st *runLoopState) readMenuInput(ansiProcessResult ansi.ProcessAnsiResult, menuRec *MenuRecord) (input string, act loopAction, retErr error) {
	e := st.e
	terminal := st.terminal
	outputMode := st.outputMode

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
			// Fall back to standard input if lightbar loading failed or no valid
			// selection was made. Shares readStandardInput so this path also gets
			// idle-timeout enforcement, ^P back-navigation and wrapped-EOF
			// detection, which the previous inline copy lacked.
			input, act, retErr := st.readStandardInput(menuRec)
			switch act {
			case loopReturn:
				return input, loopReturn, retErr
			case loopContinue:
				return input, loopContinue, retErr
			}
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
