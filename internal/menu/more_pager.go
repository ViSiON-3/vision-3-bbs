package menu

import (
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// minMorePage is the fewest lines a screen of paged output holds, however
// little room the caller's screen leaves.
const minMorePage = 5

// morePager pauses long output a screenful at a time. The caller writes its
// lines and calls Line after each one; once a screenful has gone by, Line
// shows the More prompt and waits:
//
//   - Y, Enter or Space: one more screenful
//   - C: the rest without stopping
//   - N or Q: stop, and Line reports false so the caller stops writing
//
// A caller who has turned More prompts off (User.MorePrompts) is never
// asked: the output runs on, as if C had been pressed at the start.
type morePager struct {
	e          *MenuExecutor
	s          ssh.Session
	terminal   *term.Terminal
	outputMode ansi.OutputMode
	prompt     string

	page    int // lines in a screenful
	lines   int // lines written since the last pause
	nonstop bool
	stopped bool
}

// morePromptsOn reports whether u wants long output paused (see
// User.MorePromptsOn); before login, with no user, it is on.
func morePromptsOn(u *user.User) bool {
	return u == nil || u.MorePromptsOn()
}

// newMorePager pages output for u on a screen height rows tall, of which
// reserved are taken by a header, footer or the prompt itself. prompt is the
// string the pause shows; empty means Strings().MorePrompt.
func (e *MenuExecutor) newMorePager(s ssh.Session, terminal *term.Terminal, outputMode ansi.OutputMode, u *user.User, height, reserved int, prompt string) *morePager {
	if height <= 0 {
		_, height = getTerminalSize(s)
	}
	if prompt == "" {
		prompt = e.Strings().MorePrompt
	}
	return &morePager{
		e: e, s: s, terminal: terminal, outputMode: outputMode, prompt: prompt,
		page:    max(height-reserved, minMorePage),
		nonstop: !morePromptsOn(u),
	}
}

// Line counts one line written and, after a screenful, pauses for the
// caller. It returns false once the caller has chosen to stop (or hung up);
// every later call returns false too.
func (p *morePager) Line() bool {
	if p.stopped {
		return false
	}
	p.lines++
	if p.nonstop || p.lines < p.page {
		return true
	}
	p.lines = 0
	if !p.ask() {
		p.stopped = true
	}
	return !p.stopped
}

// Lines counts n lines written at once, as Line does for each; it stops
// counting, and returns false, at the first one the caller stops on.
func (p *morePager) Lines(n int) bool {
	for range n {
		if !p.Line() {
			return false
		}
	}
	return true
}

// Stopped reports whether the caller chose to stop the output.
func (p *morePager) Stopped() bool { return p.stopped }

// ask shows the prompt and waits for an answer: true to go on.
func (p *morePager) ask() bool {
	p.e.writeString(p.terminal, p.outputMode, p.prompt)
	ih := getSessionIH(p.s)
	for {
		key, err := ih.ReadKey()
		if err != nil {
			return false
		}
		switch key {
		case 'y', 'Y', '\r', '\n', ' ':
		case 'c', 'C':
			p.nonstop = true
		case 'n', 'N', 'q', 'Q':
			terminalio.WriteProcessedBytes(p.terminal, []byte("\r\x1b[K\r\n"), p.outputMode)
			return false
		default:
			continue
		}
		terminalio.WriteProcessedBytes(p.terminal, []byte("\r\x1b[K"), p.outputMode)
		return true
	}
}
