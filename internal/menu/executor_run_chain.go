package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
)

// chainSeparator separates the commands of a chained command line (#949):
// "M;N" at MAIN runs M, then N at the menu M leads to. Typed first in hot key
// or lightbar mode, it opens a command line, so ";M;N" chains there too.
const chainSeparator = ";"

// splitChain breaks a command line into its chained commands, trimmed and in
// upper case, dropping empty ones so "m;;n;" is the same as "m;n". It returns
// nil when line holds nothing but separators and blanks.
func splitChain(line string) []string {
	var cmds []string
	for _, seg := range strings.Split(line, chainSeparator) {
		if seg = strings.ToUpper(strings.TrimSpace(seg)); seg != "" {
			cmds = append(cmds, seg)
		}
	}
	return cmds
}

// isLiteralKey reports whether input is, as typed, one of the keys of a
// command in commands. A menu that binds a key containing the separator keeps
// it: that key is matched whole rather than split into a chain.
func isLiteralKey(commands []CommandRecord, input string) bool {
	for _, cmd := range commands {
		for _, key := range strings.Fields(strings.ToUpper(cmd.Keys)) {
			if key == input {
				return true
			}
		}
	}
	return false
}

// startChain splits the line the caller just entered, st.userInput, into its
// chained commands when it has more than one. The first becomes st.userInput
// and the rest wait in st.pending, each to be matched in turn at whichever
// menu the one before it leaves the caller at. ok is false when the line was
// only separators: there is no command to run, and the menu is redisplayed
// (rather than running a ^M default, which is for Enter on its own).
func (st *runLoopState) startChain() (ok bool) {
	st.pending = nil
	if !strings.Contains(st.userInput, chainSeparator) || isLiteralKey(st.commands, st.userInput) {
		return true
	}
	cmds := splitChain(st.userInput)
	if len(cmds) == 0 {
		st.userInput = ""
		return false
	}
	st.userInput, st.pending = cmds[0], cmds[1:]
	return true
}

// nextChained takes the next waiting command of a chain into st.userInput,
// reporting false when none is waiting.
func (st *runLoopState) nextChained() bool {
	if len(st.pending) == 0 {
		return false
	}
	st.userInput, st.pending = st.pending[0], st.pending[1:]
	if len(st.pending) == 0 {
		st.pending = nil
	}
	return true
}

// cancelChain drops the rest of a chain, after a command that did not match,
// was not allowed, or failed: what follows was meant for a menu the caller
// may not now be at.
func (st *runLoopState) cancelChain() {
	st.pending = nil
}

// chainedSessionEnded reports whether the session ended before the next
// command of a chain could run. A typed selection learns that from its key
// read; a queued one reads nothing, so it checks the same things: the time
// limit, which may have run out during the menu's autoruns or screen, and an
// error a Fail latched on the session's InputHandler (a disconnect or idle
// timeout seen by code that did not pass it on). retAction and retErr are
// what Run returns when ended is true, as for the same end at a key read.
func (st *runLoopState) chainedSessionEnded() (retAction string, ended bool, retErr error) {
	if timeLimitReached(st.s) {
		st.e.handleTimeLimit(st.terminal, st.outputMode, st.nodeNumber)
		return "LOGOFF", true, nil
	}
	err := getSessionIH(st.s).Failure()
	switch {
	case err == nil:
		return "", false, nil
	case errors.Is(err, io.EOF):
		slog.Info("user disconnected before chained command", "menu", st.currentMenuName)
		return "LOGOFF", true, nil
	case errors.Is(err, editor.ErrIdleTimeout):
		st.e.handleSessionTimeout(st.s, st.terminal, st.outputMode, st.nodeNumber, st.termWidth, st.termHeight)
		return "LOGOFF", true, nil
	}
	slog.Error("session input failed before chained command", "menu", st.currentMenuName, "error", err)
	return "", true, fmt.Errorf("failed reading input: %w", err)
}

// drawChainedLightbar draws a lightbar menu's options when a queued command
// is taken there, which readMenuInput would otherwise do before reading, so
// the menu shows as it does to a caller choosing from it. The option for the
// queued command is highlighted when the bar has one. A bar that cannot be
// loaded or drawn is left out, as readMenuInput falls back to typed input.
func (st *runLoopState) drawChainedLightbar(result ansi.ProcessAnsiResult) {
	if !st.isLightbarMenu {
		return
	}
	options, err := loadLightbarOptions(st.currentMenuName, st.e)
	if err != nil || len(options) == 0 {
		return
	}
	selected := 0
	for i, opt := range options {
		if opt.HotKey == st.userInput {
			selected = i
			break
		}
	}
	if err := drawLightbarMenu(st.terminal, result.DisplayBytes, options, selected, st.outputMode, false); err != nil {
		slog.Warn("failed to draw lightbar menu for chained command", "menu", st.currentMenuName, "error", err)
	}
}
