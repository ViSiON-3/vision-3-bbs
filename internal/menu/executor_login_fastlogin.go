package menu

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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
	menuMnuPath := filepath.Join(e.MenuSetPath, "mnu")
	loadedMenu, menuErr := LoadMenu("FASTLOGN", menuMnuPath)
	if menuErr != nil {
		slog.Warn("failed to load FASTLOGN.MNU", "node", nodeNumber, "error", menuErr)
	} else {
		fastlognMenu = loadedMenu
	}

	renderFastLoginScreen := func() {
		clearFirst := fastlognMenu != nil && fastlognMenu.GetClrScrBefore()
		if displayErr := e.displayFile(terminal, "FASTLOGN.ANS", outputMode, clearFirst); displayErr != nil {
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
	cfgPath := filepath.Join(e.MenuSetPath, "cfg")
	commands, err := LoadCommands("FASTLOGN", cfgPath)
	if err != nil {
		slog.Warn("failed to load FASTLOGN.CFG", "node", nodeNumber, "error", err)
		return currentUser, "", nil
	}

	renderFastLoginScreen()

	// Check for lightbar BAR file.
	barPath := filepath.Join(e.MenuSetPath, "bar", "FASTLOGN.BAR")
	lightbarOptions, barLoadErr := loadLightbarOptions("FASTLOGN", e)
	isLightbar := barLoadErr == nil && len(lightbarOptions) > 0
	if barLoadErr != nil {
		if _, statErr := os.Stat(barPath); statErr == nil {
			slog.Warn("BAR file exists but failed to load", "node", nodeNumber, "error", barLoadErr)
		}
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
					e.handleIdleTimeout(terminal, outputMode, nodeNumber, termHeight)
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
				e.handleIdleTimeout(terminal, outputMode, nodeNumber, termHeight)
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
