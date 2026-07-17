package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// challengeStrayLimit is the number of non-matching keys that trips the
// fail-fast scripted-payload rejection: the loop rejects on the Nth stray key
// (8 or more), mirroring botgate's stray-key flood rule.
const challengeStrayLimit = 8

// fallbackGatePrompt is used when the configured art file cannot be read. It
// keeps the "{KEY}"/"{PRESSES}"/"{TIMES}" tokens and the "##" countdown field
// intact, so substituteGateTokens and the live countdown work the same as for
// configured art.
const fallbackGatePrompt = "\x1b[0m\r\n Press {KEY} {PRESSES} {TIMES} if you're not a bot.\r\n You have ## seconds.\r\n"

// gatePromptOrFallback loads the gate art file, returning a built-in fallback
// (never an error) if it cannot be read, so a missing file never drops a
// caller. The returned bytes still contain the "{KEY}"/"{PRESSES}" tokens
// (and any "##" countdown field) unsubstituted; the caller is responsible for
// running substituteGateTokens.
func gatePromptOrFallback(e *MenuExecutor, fileName string, nodeNumber int) []byte {
	path := filepath.Join(e.MenuSetPath, "ansi", fileName)
	content, err := ansi.GetAnsiFileContent(path)
	if err != nil {
		slog.Warn("challenge gate art missing, using fallback", "node", nodeNumber, "file", path, "error", err)
		return []byte(fallbackGatePrompt)
	}
	return content
}

// RunChallengeGate shows the pre-login bot challenge. It returns true if the
// caller passed (pressed the configured key the required number of times before
// the timeout), false to disconnect. An io.EOF error indicates the caller hung
// up. Modeled on RunMatrixScreen; safe to call only when EnableChallengeGate.
func (e *MenuExecutor) RunChallengeGate(
	s ssh.Session,
	terminal *term.Terminal,
	nodeNumber int,
	outputMode ansi.OutputMode,
	termWidth int,
	termHeight int,
) (bool, error) {
	cfg := e.GetServerConfig()
	matchKey := parseChallengeKey(cfg.ChallengeGateKey)
	required := cfg.ChallengeGateRequiredPresses
	if required < 1 {
		required = 1
	}
	timeout := cfg.ChallengeGateTimeoutSeconds
	if timeout < 1 {
		timeout = 1
	}

	prompt := gatePromptOrFallback(e, cfg.ChallengeGateFile, nodeNumber)
	prompt = substituteGateTokens(prompt, cfg.ChallengeGateKey, required)
	row, col, width, hasField := findCountdownField(prompt)
	live := cfg.ChallengeGateLiveCountdown && hasField

	// Clear + home, then draw the prompt with the starting number substituted.
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	draw := prompt
	if hasField {
		draw = substituteCountdown(prompt, timeout, width)
	}
	writeGateArt(terminal, draw, outputMode)

	slog.Info("challenge gate presented", "node", nodeNumber, "key", cfg.ChallengeGateKey, "required", required, "timeout_s", timeout)

	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	remaining := timeout
	onTick := func() {
		if !live {
			return
		}
		remaining--
		if remaining < 0 {
			remaining = 0
		}
		upd := fmt.Sprintf("\x1b[%d;%dH%s", row, col, formatCountdownValue(remaining, width))
		terminalio.WriteProcessedBytes(terminal, []byte(upd), outputMode)
	}

	passed, err := runChallengeLoop(getSessionIH(s), time.Now, deadline, matchKey, required, challengeStrayLimit, time.Second, onTick)
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return false, io.EOF
		}
		slog.Error("challenge gate error", "node", nodeNumber, "error", err)
		return false, err
	}
	if passed {
		slog.Info("challenge gate passed", "node", nodeNumber)
	} else {
		slog.Info("challenge gate failed", "node", nodeNumber)
	}
	return passed, nil
}

// writeGateArt writes raw bytes in CP437 mode (avoiding UTF-8 false positives),
// or processed bytes otherwise — the same branch RunMatrixScreen uses.
func writeGateArt(terminal *term.Terminal, b []byte, outputMode ansi.OutputMode) {
	if outputMode == ansi.OutputModeCP437 {
		_, _ = terminal.Write(b) // best-effort display
		return
	}
	terminalio.WriteProcessedBytes(terminal, b, outputMode)
}
