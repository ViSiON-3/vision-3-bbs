package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func fileListModeDisplay(mode string) string {
	switch strings.ToLower(mode) {
	case "classic":
		return "Classic"
	default:
		return "Lightbar"
	}
}

// runCfgToggle is a generic toggle handler for boolean user preferences.
func runCfgToggle(
	e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User,
	nodeNumber int, sessionStartTime time.Time, args string,
	outputMode ansi.OutputMode,
	fieldName string,
	getter func(*user.User) bool,
	setter func(*user.User, bool),
) (*user.User, string, error) {
	if currentUser == nil {
		return nil, "", nil
	}

	original := getter(currentUser)
	setter(currentUser, !original)

	if err := userManager.UpdateUser(currentUser); err != nil {
		setter(currentUser, original)
		slog.Error("failed to save field", "node", nodeNumber, "name", fieldName, "error", err)
		msg := fmt.Sprintf(e.LoadedStrings.CfgSaveError, fieldName)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	newVal := !original

	stateStr := e.LoadedStrings.CfgToggleOff
	if newVal {
		stateStr = e.LoadedStrings.CfgToggleOn
	}
	msg := fmt.Sprintf(e.LoadedStrings.CfgToggleFormat, fieldName, stateStr)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
	time.Sleep(500 * time.Millisecond)
	return currentUser, "", nil
}

// runCfgStringInput is a generic string input handler for user preferences.
func runCfgStringInput(
	e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User,
	nodeNumber int, outputMode ansi.OutputMode,
	fieldName string, maxLen int,
	getter func(*user.User) string,
	setter func(*user.User, string),
) (*user.User, string, error) {
	if currentUser == nil {
		return nil, "", nil
	}

	current := getter(currentUser)
	prompt := fmt.Sprintf(e.LoadedStrings.CfgStringPrompt, fieldName, maxLen)
	if current != "" {
		prompt = fmt.Sprintf(e.LoadedStrings.CfgStringPromptCurrent, fieldName, current, maxLen)
	}
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)

	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return currentUser, "", nil
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return currentUser, "", nil
	}

	// Rune-safe, not byte-safe: see the fix commit for why byte slicing here
	// would be a latent hazard even though it's unreachable today.
	if utf8.RuneCountInString(input) > maxLen {
		input = string([]rune(input)[:maxLen])
	}

	setter(currentUser, input)
	if err := userManager.UpdateUser(currentUser); err != nil {
		slog.Error("failed to save field", "node", nodeNumber, "name", fieldName, "error", err)
		return currentUser, "", nil
	}

	msg := fmt.Sprintf(e.LoadedStrings.CfgStringUpdated, fieldName)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
	time.Sleep(500 * time.Millisecond)
	return currentUser, "", nil
}
