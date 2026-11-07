package menu

import (
	"strings"
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
