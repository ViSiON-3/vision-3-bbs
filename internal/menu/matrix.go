package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// RunMatrixScreen displays the pre-login matrix menu and returns the selected action.
// Actions: "LOGIN", "NEWUSER", "CHECKACCESS", "DISCONNECT"
// Called from main.go sessionHandler before the login loop for telnet users.
// Uses standard .BAR/.CFG menu files (PDMATRIX.BAR, PDMATRIX.CFG) for configuration.
func (e *MenuExecutor) RunMatrixScreen(
	s ssh.Session,
	terminal *term.Terminal,
	userManager *user.UserMgr,
	nodeNumber int,
	outputMode ansi.OutputMode,
	termWidth int,
	termHeight int,
) (string, error) {
	const menuName = "PDMATRIX"

	// Load lightbar options from PDMATRIX.BAR
	options, err := loadLightbarOptions(menuName, e)
	if err != nil {
		slog.Warn("failed to load BAR file, skipping matrix", "node", nodeNumber, "menu", menuName, "error", err)
		return "LOGIN", nil
	}
	if len(options) == 0 {
		slog.Warn("no options in BAR file, skipping matrix", "node", nodeNumber, "menu", menuName)
		return "LOGIN", nil
	}

	// Load commands from PDMATRIX.CFG to map hotkeys to actions
	cfgPath := filepath.Join(e.MenuSetPath, "cfg")
	commands, err := LoadCommands(menuName, cfgPath)
	if err != nil {
		slog.Warn("failed to load CFG file, skipping matrix", "node", nodeNumber, "menu", menuName, "error", err)
		return "LOGIN", nil
	}

	// Build hotkey → command map
	commandMap := make(map[string]string)
	for _, cmd := range commands {
		commandMap[strings.ToUpper(cmd.Keys)] = strings.ToUpper(cmd.Command)
	}

	// Load the ANSI background (convention: PDMATRIX.ANS)
	// Use GetAnsiFileContent to automatically strip SAUCE metadata
	ansPath := filepath.Join(e.MenuSetPath, "ansi", menuName+".ANS")
	ansBackground, err := ansi.GetAnsiFileContent(ansPath)
	if err != nil {
		slog.Warn("failed to load ANS file, skipping matrix", "node", nodeNumber, "menu", menuName, "error", err)
		return "LOGIN", nil
	}

	slog.Info("displaying pre-login matrix screen", "node", nodeNumber, "count", len(options))

	// Ensure cursor is restored when we exit the matrix screen
	defer func() {
		terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25h"), outputMode)
	}()

	selectedIndex := 0
	maxTries := 10
	tries := 0

	// Draw the initial screen
	if err := drawMatrixScreen(terminal, ansBackground, options, selectedIndex, outputMode); err != nil {
		slog.Error("failed to draw matrix screen", "node", nodeNumber, "error", err)
		return "LOGIN", nil
	}

	// Apply the pre-login idle timeout on the shared InputHandler.
	// nil = no authenticated user yet; sysop exemption applies post-login only.
	getSessionIH(s).SetSessionIdleTimeout(e.idleTimeout(nil))

	sessionIH := getSessionIH(s)
	for tries < maxTries {
		key, err := sessionIH.ReadKey()
		if err != nil {
			if errors.Is(err, editor.ErrIdleTimeout) {
				e.handleIdleTimeout(terminal, outputMode, nodeNumber, termHeight)
				return "DISCONNECT", nil
			}
			if errors.Is(err, io.EOF) {
				return "DISCONNECT", io.EOF
			}
			return "DISCONNECT", fmt.Errorf("failed reading matrix input: %w", err)
		}

		newIndex := selectedIndex
		selectionMade := false

		switch key {
		case editor.KeyArrowUp:
			newIndex = selectedIndex - 1
			if newIndex < 0 {
				newIndex = len(options) - 1 // Wrap to bottom
			}

		case editor.KeyArrowDown:
			newIndex = selectedIndex + 1
			if newIndex >= len(options) {
				newIndex = 0 // Wrap to top
			}

		case editor.KeyEnter:
			selectionMade = true

		case editor.KeyEsc:
			// Bare ESC (sequences already decoded by ReadKey) — ignore

		case ' ':
			// Spacebar redraws screen (matches Pascal behavior)
			_ = drawMatrixScreen(terminal, ansBackground, options, selectedIndex, outputMode) // best-effort redraw

		default:
			if key < 32 || key > 126 {
				continue // ignore non-printable / special keys
			}
			r := rune(key)

			// Direct selection by number
			if r >= '1' && r <= '9' {
				numIndex := int(r - '1')
				if numIndex < len(options) {
					selectedIndex = numIndex
					_ = drawMatrixOptions(terminal, options, selectedIndex, outputMode) // best-effort redraw
					selectionMade = true
				}
				break
			}

			// Check for hotkey match (explicit HotKey field from BAR file)
			keyStr := strings.ToUpper(string(r))
			matchedHotkey := false
			for i, opt := range options {
				if keyStr == opt.HotKey {
					selectedIndex = i
					_ = drawMatrixOptions(terminal, options, selectedIndex, outputMode) // best-effort redraw
					selectionMade = true
					matchedHotkey = true
					break
				}
			}
			if !matchedHotkey {
				e.showUndefinedMenuInput(terminal, outputMode, nodeNumber)
				_ = drawMatrixScreen(terminal, ansBackground, options, selectedIndex, outputMode) // best-effort redraw
			}
		}

		if newIndex != selectedIndex {
			selectedIndex = newIndex
			_ = drawMatrixOptions(terminal, options, selectedIndex, outputMode) // best-effort redraw
		}

		if selectionMade {
			// Look up the command for this option's hotkey
			hotkey := options[selectedIndex].HotKey
			action, ok := commandMap[hotkey]
			if !ok {
				slog.Warn("no command mapped for hotkey", "node", nodeNumber, "hotkey", hotkey)
				continue
			}
			slog.Info("matrix selection", "node", nodeNumber, "text", options[selectedIndex].Text, "action", action)

			result, err := e.processMatrixAction(action, s, terminal, userManager, nodeNumber, outputMode, termWidth, termHeight)
			if err != nil {
				return result, err
			}
			if result == "LOGIN" || result == "DISCONNECT" {
				return result, nil
			}

			// For actions that return to the matrix (like NEWUSER, CHECKACCESS),
			// redraw the screen and continue
			tries++
			selectedIndex = 0
			_ = drawMatrixScreen(terminal, ansBackground, options, selectedIndex, outputMode) // best-effort redraw
		}
	}

	// Max tries exceeded
	slog.Info("matrix max tries exceeded, disconnecting", "node", nodeNumber)
	return "DISCONNECT", nil
}

// processMatrixAction handles the selected matrix menu action.
func (e *MenuExecutor) processMatrixAction(
	action string,
	s ssh.Session,
	terminal *term.Terminal,
	userManager *user.UserMgr,
	nodeNumber int,
	outputMode ansi.OutputMode,
	termWidth int,
	termHeight int,
) (string, error) {
	switch action {
	case "LOGIN":
		// Show PRELOGON ANSI file before login screen (matches Pascal: Printfile(PRELOGON.x) + HoldScreen)
		e.showPrelogon(s, terminal, nodeNumber, outputMode, termWidth, termHeight)
		return "LOGIN", nil

	case "NEWUSER":
		// Clear screen immediately when transitioning from matrix to new user flow
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
		terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25h"), outputMode) // Show cursor
		err := e.handleNewUserApplication(s, terminal, userManager, nodeNumber, outputMode, termWidth, termHeight)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "DISCONNECT", io.EOF
			}
			slog.Error("new user application error from matrix", "node", nodeNumber, "error", err)
		}
		return "MATRIX", nil // Return to matrix after signup

	case "CHECKACCESS":
		e.handleCheckAccess(s, terminal, userManager, nodeNumber, outputMode)
		return "MATRIX", nil // Return to matrix after check

	case "DISCONNECT":
		terminalio.WriteStringCP437(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.MatrixDisconnecting)), outputMode)
		return "DISCONNECT", nil

	default:
		slog.Warn("unknown matrix action", "node", nodeNumber, "action", action)
		e.showUndefinedMenuInput(terminal, outputMode, nodeNumber)
		return "MATRIX", nil
	}
}

// handleCheckAccess prompts for a handle and shows their validation status.
func (e *MenuExecutor) handleCheckAccess(
	s ssh.Session,
	terminal *term.Terminal,
	userManager *user.UserMgr,
	nodeNumber int,
	outputMode ansi.OutputMode,
) {
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)

	terminalio.WriteStringCP437(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.MatrixCheckAccessPrompt)), outputMode)

	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return
	}

	handle := strings.TrimSpace(input)
	if handle == "" {
		return
	}

	foundUser, exists := userManager.GetUser(handle)

	if !exists {
		terminalio.WriteStringCP437(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.MatrixUserNotFound)), outputMode)
	} else if foundUser.Validated {
		msg := fmt.Sprintf(e.LoadedStrings.MatrixAccountValidated, foundUser.Handle, foundUser.AccessLevel)
		terminalio.WriteStringCP437(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
	} else {
		msg := fmt.Sprintf(e.LoadedStrings.MatrixAccountNotValidated, foundUser.Handle)
		terminalio.WriteStringCP437(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)

		// If NUV is enabled, show voting progress for this candidate.
		cfg := e.GetServerConfig()
		if cfg.UseNUV {
			nuvMu.Lock()
			nd, err := loadNUVData(e.RootConfigPath)
			nuvMu.Unlock()
			if err == nil {
				for _, c := range nd.Candidates {
					if strings.EqualFold(c.Handle, foundUser.Handle) {
						yes := nuvYesCount(&c)
						no := len(c.Votes) - yes
						nuvSuffix := "yes needed to reach threshold"
						if cfg.NUVValidate {
							nuvSuffix = "yes needed to validate"
						}
						nuvMsg := fmt.Sprintf("\r\n|07Your application is in the voting queue.\r\n"+
							"|07Votes so far: |10%d Yes|07, |12%d No|07  |08(|10%d|08 %s)\r\n",
							yes, no, cfg.NUVYesVotes, nuvSuffix)
						terminalio.WriteStringCP437(terminal, ansi.ReplacePipeCodes([]byte(nuvMsg)), outputMode)
						break
					}
				}
			}
		}
	}

	// Pause
	pausePrompt := e.LoadedStrings.PauseString
	if pausePrompt == "" {
		pausePrompt = "\r\n|07Press |15[ENTER]|07 to continue... "
	}
	terminalio.WriteStringCP437(terminal, ansi.ReplacePipeCodes([]byte(pausePrompt)), outputMode)
	_, _ = readLineFromSessionIH(s, terminal)
}

// showPrelogon displays a random PRELOGON ANSI file before the login screen.
// Matches Pascal: Printfile(PRELOGON.x) + HoldScreen where x is random 1..NumPrelogon.
// Looks for numbered files (PRELOGON.1, PRELOGON.2, ...) first, falls back to PRELOGON.ANS.
func (e *MenuExecutor) showPrelogon(s ssh.Session, terminal *term.Terminal, nodeNumber int, outputMode ansi.OutputMode, termWidth, termHeight int) {
	ansiDir := filepath.Join(e.MenuSetPath, "ansi")

	// Look for numbered PRELOGON files (Pascal pattern: PRELOGON.1, PRELOGON.2, ...)
	var candidates []string
	for i := 1; i <= 20; i++ {
		path := filepath.Join(ansiDir, fmt.Sprintf("PRELOGON.%d", i))
		if _, err := os.Stat(path); err == nil {
			candidates = append(candidates, path)
		} else {
			break // Stop at first gap
		}
	}

	// Fall back to single PRELOGON.ANS
	if len(candidates) == 0 {
		path := filepath.Join(ansiDir, "PRELOGON.ANS")
		if _, err := os.Stat(path); err == nil {
			candidates = append(candidates, path)
		}
	}

	if len(candidates) == 0 {
		return // No PRELOGON files found
	}

	// Pick a random file
	idx := 0
	if len(candidates) > 1 {
		idx = int(time.Now().UnixNano() % int64(len(candidates)))
	}

	// Use GetAnsiFileContent to automatically strip SAUCE metadata
	rawContent, err := ansi.GetAnsiFileContent(candidates[idx])
	if err != nil {
		slog.Warn("failed to read prelogon file", "node", nodeNumber, "file", candidates[idx], "error", err)
		return
	}

	slog.Info("displaying prelogon screen", "node", nodeNumber, "file", filepath.Base(candidates[idx]))
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	// For CP437 mode, write raw bytes directly to avoid UTF-8 false positives
	if outputMode == ansi.OutputModeCP437 {
		_, _ = terminal.Write(rawContent) // best-effort display
	} else {
		terminalio.WriteProcessedBytes(terminal, rawContent, outputMode)
	}

	// HoldScreen — pause before proceeding to login
	terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
	e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
}

// drawMatrixScreen clears the screen, draws the ANSI background, and highlights the selected option.
func drawMatrixScreen(
	terminal *term.Terminal,
	ansBackground []byte,
	options []LightbarOption,
	selectedIndex int,
	outputMode ansi.OutputMode,
) error {
	// Clear screen and draw background
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	// For CP437 mode, write raw bytes directly to avoid UTF-8 false positives
	if outputMode == ansi.OutputModeCP437 {
		_, _ = terminal.Write(ansBackground) // best-effort display
	} else {
		terminalio.WriteProcessedBytes(terminal, ansBackground, outputMode)
	}

	// Draw options with highlighting
	return drawMatrixOptions(terminal, options, selectedIndex, outputMode)
}

// drawMatrixOptions redraws the menu option text with the current selection highlighted.
// Uses DOS color codes from LightbarOption (via colorCodeToAnsi) for rendering.
func drawMatrixOptions(
	terminal *term.Terminal,
	options []LightbarOption,
	selectedIndex int,
	outputMode ansi.OutputMode,
) error {
	for i, opt := range options {
		// Position cursor at this option
		posCmd := fmt.Sprintf("\x1b[%d;%dH", opt.Y, opt.X)
		terminalio.WriteProcessedBytes(terminal, []byte(posCmd), outputMode)

		// Apply color based on selection (DOS color code → ANSI escape)
		var colorCode int
		if i == selectedIndex {
			colorCode = opt.HighlightColor
		} else {
			colorCode = opt.RegularColor
		}
		terminalio.WriteProcessedBytes(terminal, []byte(colorCodeToAnsi(colorCode)), outputMode)

		// Write the option text
		terminalio.WriteProcessedBytes(terminal, []byte(opt.Text), outputMode)

		// Reset attributes
		terminalio.WriteProcessedBytes(terminal, []byte("\x1b[0m"), outputMode)
	}

	// Hide cursor after drawing
	terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25l"), outputMode)
	return nil
}
