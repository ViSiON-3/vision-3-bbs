package menu

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runFastLogin presents the FASTLOGN menu inline during the login sequence.
// Returns a GOTO action if the user chooses to skip/jump, or empty string to continue.
func runFastLogin(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode
	termHeight := c.termHeight

	slog.Debug("running FASTLOGIN inline", "node", nodeNumber, "handle", currentUser.Handle)

	// Load FASTLOGN menu definition (.MNU) for CLR/CLS + prompt behavior
	var fastlognMenu *MenuRecord
	loadedMenu, menuErr := LoadMenu("FASTLOGN", e.Menus())
	if menuErr != nil {
		slog.Warn("failed to load FASTLOGN.MNU", "node", nodeNumber, "error", menuErr)
	} else {
		fastlognMenu = loadedMenu
	}

	renderFastLoginScreen := func() {
		clearFirst := fastlognMenu != nil && fastlognMenu.GetClrScrBefore()
		if displayErr := e.displayFile(terminal, "FASTLOGN.ANS", outputMode, c.termWidth, termHeight, clearFirst); displayErr != nil {
			slog.Warn("failed to display FASTLOGN.ANS", "node", nodeNumber, "error", displayErr)
		}

		if fastlognMenu != nil && fastlognMenu.GetUsePrompt() {
			promptParts := make([]string, 0, 2)
			if strings.TrimSpace(fastlognMenu.Prompt1) != "" {
				promptParts = append(promptParts, fastlognMenu.Prompt1)
			}
			if strings.TrimSpace(fastlognMenu.Prompt2) != "" {
				promptParts = append(promptParts, fastlognMenu.Prompt2)
			}
			if len(promptParts) > 0 {
				prompt := "\r\n" + strings.Join(promptParts, "\r\n")
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)
			}
		}
	}

	// Load FASTLOGN commands
	commands, err := LoadCommands("FASTLOGN", e.Menus())
	if err != nil {
		slog.Warn("failed to load FASTLOGN.CFG", "node", nodeNumber, "error", err)
		return currentUser, "", nil
	}
	// A missing or empty CFG loads as no commands. With nothing to match,
	// every key would answer "Unknown command!" and trap the caller, so
	// continue the login as for a malformed CFG.
	if len(commands) == 0 {
		slog.Warn("FASTLOGN.CFG is missing or has no commands; continuing login", "node", nodeNumber)
		return currentUser, "", nil
	}

	// Dispatch command by key string against CFG commands.
	dispatchCommand := func(keyStr string) (*user.User, string, error, bool) {
		for _, cmd := range commands {
			keys := strings.Fields(strings.ToUpper(cmd.Keys))
			matched := false
			for _, key := range keys {
				if key == keyStr {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			if cmd.ACS != "" && cmd.ACS != "*" {
				if !checkACS(cmd.ACS, currentUser, s, terminal, sessionStartTime) {
					continue
				}
			}
			if cmd.Command == "RUN:FULL_LOGIN_SEQUENCE" {
				slog.Debug("FASTLOGIN - user chose to continue full sequence", "node", nodeNumber)
				terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
				return currentUser, "", nil, true
			}
			if strings.HasPrefix(cmd.Command, "GOTO:") {
				nextMenu := strings.ToUpper(strings.TrimPrefix(cmd.Command, "GOTO:"))
				slog.Debug("FASTLOGIN - user chose GOTO", "node", nodeNumber, "menu", nextMenu)
				terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
				return currentUser, "GOTO:" + nextMenu, nil, true
			}
			if cmd.Command == "RUN:MAINLOGOFF" || cmd.Command == "LOGOFF" {
				return currentUser, "LOGOFF", nil, true
			}
			if cmd.Command == "RUN:IMMEDIATELOGOFF" {
				return nil, "LOGOFF", io.EOF, true
			}
		}
		return nil, "", nil, false
	}

	// FASTLOGN.MNU's SCRIPT, when it names one, draws Fast Logon in place of
	// its screen and answers with the caller's key, which is dispatched as
	// a key pressed on the screen. The built-in screen is drawn when the
	// script cannot run or its answer is no command the caller may use. The
	// script is not run again, so one that answers without reading the
	// caller cannot loop.
	if fastlognMenu != nil {
		permitted := func(acs string) bool {
			return acs == "" || acs == "*" || checkACS(acs, currentUser, s, terminal, sessionStartTime)
		}
		offered, owners := fastLoginOffer(commands, permitted)
		view := c.runState("FASTLOGN").menuView(fastlognMenu, offered)
		choice, scripted, scriptErr := c.scriptedChoice("FASTLOGN", fastlognMenu, view)
		if scriptErr != nil {
			if errors.Is(scriptErr, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			if errors.Is(scriptErr, editor.ErrIdleTimeout) {
				// Idle, or out of time (editor.ErrTimeLimit wraps it): the
				// login sequence drops the error, so the notice is shown here.
				e.handleSessionTimeout(s, terminal, outputMode, nodeNumber, c.termWidth, termHeight)
				return currentUser, "LOGOFF", nil
			}
			return currentUser, "LOGOFF", scriptErr
		}
		if scripted {
			// The key runs the entry it was shown for only if that entry
			// still wins under the caller's access now; an ACS can depend on
			// the time. Otherwise the built-in screen asks again.
			_, now := fastLoginOffer(commands, permitted)
			if i, ok := owners[choice]; ok && now[choice] == i {
				if u, action, err, matched := dispatchCommand(choice); matched {
					return u, action, err
				}
			}
		}
	}

	renderFastLoginScreen()

	// Check for lightbar BAR file.
	barPath := e.menuFile("bar", "FASTLOGN.BAR")
	lightbarOptions, barLoadErr := loadLightbarOptions("FASTLOGN", e)
	isLightbar := barLoadErr == nil && len(lightbarOptions) > 0
	if barLoadErr != nil {
		if _, statErr := os.Stat(barPath); statErr == nil {
			slog.Warn("BAR file exists but failed to load", "node", nodeNumber, "error", barLoadErr)
		}
	}

	// Use session-scoped InputHandler so we share the single goroutine reading
	// from the SSH session (prevents "double key press" race with other menus).
	ih := getSessionIH(s)

	if isLightbar {
		slog.Debug("FASTLOGIN using lightbar mode", "node", nodeNumber, "count", len(lightbarOptions))
		selectedIndex := 0
		_ = drawLightbarMenu(terminal, nil, lightbarOptions, selectedIndex, outputMode, false)

		for {
			key, readErr := ih.ReadKey()
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					return nil, "LOGOFF", io.EOF
				}
				if errors.Is(readErr, editor.ErrIdleTimeout) {
					e.handleIdleTimeout(terminal, outputMode, nodeNumber, c.termWidth, termHeight)
					return currentUser, "LOGOFF", nil
				}
				return currentUser, "", readErr
			}

			switch key {
			case editor.KeyArrowUp:
				if selectedIndex > 0 {
					prev := selectedIndex
					selectedIndex--
					_ = drawLightbarOption(terminal, lightbarOptions[prev], false, outputMode)
					_ = drawLightbarOption(terminal, lightbarOptions[selectedIndex], true, outputMode)
				}
			case editor.KeyArrowDown:
				if selectedIndex < len(lightbarOptions)-1 {
					prev := selectedIndex
					selectedIndex++
					_ = drawLightbarOption(terminal, lightbarOptions[prev], false, outputMode)
					_ = drawLightbarOption(terminal, lightbarOptions[selectedIndex], true, outputMode)
				}
			case int('\r'), int('\n'): // Enter — select current
				if selectedIndex >= 0 && selectedIndex < len(lightbarOptions) {
					keyStr := lightbarOptions[selectedIndex].HotKey
					if u, action, err, matched := dispatchCommand(keyStr); matched {
						return u, action, err
					}
				}
				return currentUser, "", nil
			case editor.KeyEsc:
				// Bare ESC (InputHandler consumed any ANSI sequence) — ignore
			default:
				if key >= 32 && key < 127 {
					keyStr := strings.ToUpper(string(rune(key)))
					if key == int('/') {
						// Read second key for two-character CFG commands like /G,
						// matching the non-lightbar fallback path below.
						if nextKey, nextErr := ih.ReadKey(); nextErr == nil && nextKey >= 32 && nextKey < 127 {
							keyStr = "/" + strings.ToUpper(string(rune(nextKey)))
						}
					}
					for i, opt := range lightbarOptions {
						if keyStr == opt.HotKey {
							prev := selectedIndex
							selectedIndex = i
							if prev != selectedIndex {
								_ = drawLightbarOption(terminal, lightbarOptions[prev], false, outputMode)
								_ = drawLightbarOption(terminal, lightbarOptions[selectedIndex], true, outputMode)
							}
							if u, action, err, matched := dispatchCommand(keyStr); matched {
								return u, action, err
							}
						}
					}
					// Also check non-lightbar commands (like G, /G)
					if u, action, err, matched := dispatchCommand(keyStr); matched {
						return u, action, err
					}
				}
			}
		}
	}

	// Fallback: standard keystroke input (no lightbar).
	for {
		key, readErr := ih.ReadKey()
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			if errors.Is(readErr, editor.ErrIdleTimeout) {
				e.handleIdleTimeout(terminal, outputMode, nodeNumber, c.termWidth, termHeight)
				return currentUser, "LOGOFF", nil
			}
			return currentUser, "", readErr
		}

		if key == editor.KeyEsc || key < 32 || key == 127 {
			continue
		}

		keyStr := strings.ToUpper(string(rune(key)))
		if key == int('/') {
			// Read second key for two-character commands like /G
			nextKey, nextErr := ih.ReadKey()
			if nextErr == nil && nextKey >= 32 && nextKey < 127 {
				keyStr = "/" + strings.ToUpper(string(rune(nextKey)))
			}
		}

		if u, action, err, matched := dispatchCommand(keyStr); matched {
			return u, action, err
		}

		e.showUndefinedMenuInput(terminal, outputMode, nodeNumber)
		renderFastLoginScreen()
	}
}

// fastLoginOffer returns the commands of a Fast Logon script's view: those
// Fast Logon acts on that permitted allows by their ACS, each key given once,
// to the first such entry in the CFG, which is the one dispatch runs for it.
// owners maps each offered key, upper-cased, to its entry's index in
// commands.
func fastLoginOffer(commands []CommandRecord, permitted func(acs string) bool) (offered []CommandRecord, owners map[string]int) {
	owners = map[string]int{}
	for i, cmd := range commands {
		if !fastLoginActsOn(cmd.Command) || !permitted(cmd.ACS) {
			continue
		}
		var keys []string
		for _, key := range strings.Fields(cmd.Keys) {
			if _, taken := owners[strings.ToUpper(key)]; !taken {
				owners[strings.ToUpper(key)] = i
				keys = append(keys, key)
			}
		}
		if len(keys) > 0 {
			cmd.Keys = strings.Join(keys, " ")
			offered = append(offered, cmd)
		}
	}
	return offered, owners
}

// fastLoginActsOn reports whether command is one Fast Logon carries out:
// the full login, a GOTO:, or a logoff.
func fastLoginActsOn(command string) bool {
	switch command {
	case "RUN:FULL_LOGIN_SEQUENCE", "RUN:MAINLOGOFF", "LOGOFF", "RUN:IMMEDIATELOGOFF":
		return true
	}
	return strings.HasPrefix(command, "GOTO:")
}
