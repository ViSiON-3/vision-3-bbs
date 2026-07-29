package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
)

// runLightbarInput drives the keyboard-navigation loop for a lightbar menu
// after the initial menu has been drawn successfully by the caller (Run
// keeps the drawLightbarMenu call and its failure-driven
// st.isLightbarMenu = false toggle exactly where they were; only the
// keyboard-navigation loop that runs after a successful draw moves here).
// cursorHidden is the value returned by the e.hideCursorIfNeeded call Run
// already made before drawing, needed here so the loop's own error/exit
// paths can call e.showCursorIfHidden with the same value.
//
// Returns the selected option's hotkey as input with act set to
// loopFallthrough so Run proceeds to match it against the menu's commands
// (mirroring the original code falling through to the "6. Process Input"
// section). A read error or idle timeout instead sets act to loopReturn,
// with input/retErr carrying the exact values Run's original inline
// `return` statements in this loop used to produce.
func (st *runLoopState) runLightbarInput(options []LightbarOption, cursorHidden bool) (input string, act loopAction, retErr error) {
	e := st.e
	s := st.s
	terminal := st.terminal
	outputMode := st.outputMode
	nodeNumber := st.nodeNumber
	termHeight := st.termHeight

	// Process keyboard navigation for lightbar.
	// Use the session-scoped InputHandler so the same goroutine that
	// reads from the SSH session is shared with any editor invocations
	// triggered from this menu (e.g. COMPOSEMSG). This prevents the
	// orphaned goroutine from consuming the first keystroke after the
	// editor exits, which caused the "double key press" bug.
	lightbarResult := "" // Use a local variable for the result
	inputLoop := true
	selectedIndex := 0
	sessionIH := getSessionIH(s)
	for inputLoop {
		key, err := sessionIH.ReadKey()
		if err != nil {
			e.showCursorIfHidden(terminal, outputMode, cursorHidden)
			if errors.Is(err, io.EOF) {
				slog.Info("user disconnected during lightbar input", "menu", st.currentMenuName)
				return "LOGOFF", loopReturn, nil
			}
			if errors.Is(err, editor.ErrIdleTimeout) {
				e.handleIdleTimeout(terminal, outputMode, nodeNumber, termHeight)
				return "LOGOFF", loopReturn, nil
			}
			slog.Error("failed to read lightbar input", "menu", st.currentMenuName, "error", err)
			return "", loopReturn, fmt.Errorf("failed reading lightbar input: %w", err)
		}
		slog.Debug("lightbar input key", "key", key)

		switch key {
		case editor.KeyArrowUp:
			prevIndex := selectedIndex
			selectedIndex--
			if selectedIndex < 0 {
				selectedIndex = len(options) - 1
			}
			if prevIndex != selectedIndex {
				_ = drawLightbarOption(terminal, options[prevIndex], false, outputMode)
				_ = drawLightbarOption(terminal, options[selectedIndex], true, outputMode)
			}
		case editor.KeyArrowDown:
			prevIndex := selectedIndex
			selectedIndex++
			if selectedIndex >= len(options) {
				selectedIndex = 0
			}
			if prevIndex != selectedIndex {
				_ = drawLightbarOption(terminal, options[prevIndex], false, outputMode)
				_ = drawLightbarOption(terminal, options[selectedIndex], true, outputMode)
			}
		case editor.KeyHome:
			if selectedIndex != 0 {
				prevIndex := selectedIndex
				selectedIndex = 0
				_ = drawLightbarOption(terminal, options[prevIndex], false, outputMode)
				_ = drawLightbarOption(terminal, options[selectedIndex], true, outputMode)
			}
		case editor.KeyEnd:
			lastIdx := len(options) - 1
			if selectedIndex != lastIdx {
				prevIndex := selectedIndex
				selectedIndex = lastIdx
				_ = drawLightbarOption(terminal, options[prevIndex], false, outputMode)
				_ = drawLightbarOption(terminal, options[selectedIndex], true, outputMode)
			}
		case int('\r'), int('\n'): // Enter (CR or LF) - select current item
			if selectedIndex >= 0 && selectedIndex < len(options) {
				lightbarResult = options[selectedIndex].HotKey
				inputLoop = false
			}
		case editor.KeyEsc:
			// Bare ESC (InputHandler already consumed any ANSI sequence) — ignore
		default:
			if key >= int('1') && key <= int('9') {
				// Direct selection by number
				numIndex := key - int('1') // Convert 1-9 to 0-8
				if numIndex >= 0 && numIndex < len(options) {
					prevIndex := selectedIndex
					selectedIndex = numIndex
					if prevIndex != selectedIndex {
						_ = drawLightbarOption(terminal, options[prevIndex], false, outputMode)
						_ = drawLightbarOption(terminal, options[selectedIndex], true, outputMode)
					}
					lightbarResult = options[numIndex].HotKey
					inputLoop = false
				}
			} else if key >= 32 && key < 127 {
				// Check if printable key matches any hotkey directly
				keyStr := strings.ToUpper(string(rune(key)))
				for _, opt := range options {
					if keyStr == opt.HotKey {
						lightbarResult = opt.HotKey
						inputLoop = false
						break
					}
				}
			}
			// Control chars and other special codes are ignored
		}
	}
	slog.Debug("processed lightbar input", "result", lightbarResult)
	e.showCursorIfHidden(terminal, outputMode, cursorHidden)
	return lightbarResult, loopFallthrough, nil
}

// readStandardInput implements the non-lightbar prompt+readline path: it
// delivers any pending pages, displays the current menu prompt (unless
// UsePrompt is false), reads a line of input via the session's
// InputHandler, and handles the ^P back-navigation shortcut.
//
// act is loopReturn on disconnect/idle-timeout/error, with input/retErr
// carrying the exact values Run's original inline `return` statements in
// this block used to produce. act is loopContinue when ^P navigation
// occurred (st.currentMenuName/st.previousMenuName are already swapped
// before returning, exactly as the original inline `continue` did after
// the same mutations) — Run must `continue` immediately, before command
// matching. Otherwise act is loopFallthrough and input holds the
// (already uppercased+trimmed) user input for command matching.
func (st *runLoopState) readStandardInput(menuRec *MenuRecord) (input string, act loopAction, retErr error) {
	e := st.e
	s := st.s
	terminal := st.terminal
	outputMode := st.outputMode
	userManager := st.userManager
	nodeNumber := st.nodeNumber
	sessionStartTime := st.sessionStartTime

	// --- Standard Menu Input Handling ---
	e.deliverPendingPages(terminal, nodeNumber, outputMode)
	// Display Prompt (Skip if USEPROMPT is false)
	slog.Debug("checking prompt display for menu", "menu", st.currentMenuName, "usePrompt", menuRec.GetUsePrompt())
	if menuRec.GetUsePrompt() { // Condition changed: Only check UsePrompt
		slog.Debug("calling displayPrompt for menu", "menu", st.currentMenuName)
		err := e.displayPrompt(terminal, menuRec, st.currentUser, userManager, nodeNumber, st.currentMenuName, sessionStartTime, outputMode, st.currentAreaName) // Pass st.currentAreaName
		slog.Debug("returned from displayPrompt for menu", "menu", st.currentMenuName, "error", err)
		if err != nil {
			return "", loopReturn, err // Propagate the error
		}
	} else {
		// Log message remains the same, but the condition causing it is now just UsePrompt==false
		slog.Debug("skipping prompt display", "menu", st.currentMenuName, "usePrompt", menuRec.GetUsePrompt(), "prompt1Empty", menuRec.Prompt1 == "")
	}

	// Read User Input Line via shared InputHandler to avoid reader races.
	rawInput, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		if errors.Is(err, io.EOF) {
			slog.Info("user disconnected during menu input", "menu", st.currentMenuName)
			return "LOGOFF", loopReturn, nil
		}
		if errors.Is(err, editor.ErrIdleTimeout) {
			e.handleIdleTimeout(terminal, outputMode, nodeNumber, st.termHeight)
			return "LOGOFF", loopReturn, nil
		}
		slog.Error("failed to read input for menu", "menu", st.currentMenuName, "error", err)
		return "", loopReturn, fmt.Errorf("failed reading input: %w", err)
	}
	st.userInput = strings.ToUpper(strings.TrimSpace(rawInput))
	slog.Debug("user input", "input", st.userInput)

	// --- Special Input Handling (^P, ##) ---
	if st.userInput == "\x10" || st.userInput == "^P" { // Ctrl+P is ASCII 16 (\x10)
		if st.previousMenuName != "" {
			slog.Debug("user entered ^P, going back to previous menu", "previous", st.previousMenuName)
			temp := st.currentMenuName
			st.currentMenuName = st.previousMenuName
			st.previousMenuName = temp   // Update previous in case they go back again
			return "", loopContinue, nil // Go directly to the previous menu loop iteration
		}
		slog.Debug("user entered ^P, but no previous menu recorded")
		return "", loopContinue, nil // Re-display current menu prompt
	}
	// --- End Special Input Handling ---

	return st.userInput, loopFallthrough, nil
}

// matchCommand finds the command among commands whose Keys match userInput,
// subject to hasAccess (an ACS check closure over the requesting session's
// user/terminal/start-time, matching the closure form checkACS is invoked
// with inline). It is deterministic — given the same
// commands/userInput/hasAccess it always produces the same (nextAction,
// nodeActivity, matched) — so it can be exercised directly by unit tests. Its
// only side effect is debug logging.
//
// Special cases: "/G" is the global hangup shortcut (matches unconditionally,
// no ACS check); "^M" matches Enter with no input (classic BBS default
// command); "##" matches any all-numeric input (classic BBS numeric
// wildcard), appending the entered number to the command as args.
//
// hasAccess takes both the command's ACS string and its Keys string (rather
// than just the ACS string) so that the closure Run constructs — which has
// access to st.currentUser and therefore knows whether to log the
// authenticated-user or unauthenticated-user denial message — can still
// include the original "keys" field in that debug log line. The denial log
// itself is emitted exactly once, inside that closure; matchCommand never
// logs the ACS-denial case itself, so there is no risk of a duplicate line.
func matchCommand(commands []CommandRecord, userInput string, hasAccess func(acs string, keys string) bool) (nextAction string, nodeActivity string, matched bool) {
	matchedNodeActivity := ""

	// Global hangup shortcut: /G
	if userInput == "/G" {
		nextAction = "RUN:IMMEDIATELOGOFF"
		matched = true
	}

	if !matched { // Check keyword matches (relevant for both)
		for _, cmdRec := range commands {
			// Hidden commands are still matched (e.g. % for sponsor menu); HIDDEN only affects display/prompts.

			cmdACS := cmdRec.ACS
			if !hasAccess(cmdACS, cmdRec.Keys) { // ACS check closure (logs denial itself; see doc comment above)
				continue // Skip this command if ACS check fails
			}

			keys := strings.Split(cmdRec.Keys, " ") // Use string directly
			for _, key := range keys {
				// ^M matches when user presses Enter with no input (classic BBS default command)
				if key == "^M" && userInput == "" {
					nextAction = cmdRec.Command
					matchedNodeActivity = cmdRec.NodeActivity
					slog.Debug("matched ^M (enter/default) to command action", "command", nextAction)
					matched = true
					break
				}
				// Standard exact key match
				if key != "" && userInput != "" && userInput == key {
					nextAction = cmdRec.Command
					matchedNodeActivity = cmdRec.NodeActivity
					slog.Debug("matched key to command action", "key", key, "command", nextAction)
					matched = true
					break
				}
				// ## matches any numeric input (classic BBS numeric wildcard)
				if key == "##" && userInput != "" {
					isNumeric := true
					for _, ch := range userInput {
						if ch < '0' || ch > '9' {
							isNumeric = false
							break
						}
					}
					if isNumeric {
						// Append the entered number as args so executeCommandAction
						// forwards it to the RUN: handler via runArgs.
						nextAction = cmdRec.Command + " " + userInput
						matchedNodeActivity = cmdRec.NodeActivity
						slog.Debug("matched ## numeric wildcard to command action", "input", userInput, "command", nextAction)
						matched = true
						break
					}
				}
			}
			if matched {
				break // Break outer command loop
			}
		}
	}

	return nextAction, matchedNodeActivity, matched
}
