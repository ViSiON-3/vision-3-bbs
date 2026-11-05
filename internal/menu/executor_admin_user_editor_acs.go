package menu

import (
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/acs"
	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// acsTesterKey opens the ACS tester for the selected user (#608).
const acsTesterKey = 't'

// acsTesterAllowed reports whether the editor's user may use the tester: the
// SYSOP keyword's level, sysOpLevel in config.json, as the admin menus use.
// An explanation can show what a user's private note holds and which flags
// and conferences they have, so the tester checks this itself rather than
// relying on how the editor was reached.
func (st *userEditorState) acsTesterAllowed() bool {
	return st.e.isSysOpOrAbove(st.currentUser)
}

// acsTesterScreen tests ACS expressions for one user. The context is the
// tester's own: no session and the current time until the sysop changes
// them, never the sysop's own session.
type acsTesterScreen struct {
	st            *userEditorState
	target        *user.User
	tester        acs.Tester
	expr          string
	notice        string
	width, height int
}

// runACSTester runs the tester for the selected user, as stored: staged
// edits are dealt with first, as for the infoform and device screens. It
// returns an error only when the session should end.
func (st *userEditorState) runACSTester(termWidth, termHeight int) error {
	sel := st.users[st.selectedIndex]
	target, ok := st.userManager.GetUserByID(sel.ID)
	if !ok {
		return nil
	}
	cfg := st.e.GetServerConfig()
	if termWidth <= 0 {
		termWidth = 80
	}
	if termHeight <= 0 {
		termHeight = 24
	}
	a := &acsTesterScreen{
		st: st, target: target,
		tester: acs.Tester{SysOpLevel: cfg.SysOpLevel, CoSysOpLevel: cfg.CoSysOpLevel, Location: config.LoadTimezone(cfg.Timezone)},
		width:  termWidth, height: termHeight,
	}
	if st.adminCursorHidden {
		_ = terminalio.WriteProcessedBytes(st.terminal, []byte("\x1b[?25h"), st.outputMode)
		defer terminalio.WriteProcessedBytes(st.terminal, []byte("\x1b[?25l"), st.outputMode)
	}
	return a.run()
}

func (a *acsTesterScreen) run() error {
	a.draw()
	if err := a.readExpression(); err != nil {
		return err
	}
	for {
		a.draw()
		a.st.e.writeString(a.st.terminal, a.st.outputMode, a.st.e.Strings().AcsTestMenu)
		key, err := a.st.ih.ReadKey()
		if err != nil {
			return err
		}
		a.write("\r\n")
		a.notice = ""
		switch key {
		case 'e', 'E':
			err = a.readExpression()
		case 's', 'S':
			err = a.readSession()
		case 't', 'T':
			err = a.readTime()
		case 'q', 'Q', '\r', '\n', editor.KeyEsc:
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (a *acsTesterScreen) write(s string) {
	_ = terminalio.WriteProcessedBytes(a.st.terminal, []byte(s), a.st.outputMode)
}

// draw clears the screen and shows the title and the tested expression, or
// only the context before one has been entered, paged to the terminal. The
// lines are plain text: handles and expressions go out as they are, never
// read as colour codes.
func (a *acsTesterScreen) draw() {
	str := a.st.e.Strings()
	a.write(ansi.ClearScreen())
	a.write(string(ansi.ReplacePipeCodes([]byte(str.AcsTestTitle+" |08/ |07"))) + a.target.Handle + "\r\n\r\n")
	var lines []string
	if a.expr == "" {
		lines = acs.ContextLines(a.tester, a.target, a.width-1)
	} else {
		lines = acs.Render(a.tester.Explain(a.expr, a.target), a.width-1)
	}
	// One row stays free for the More prompt; the title took two.
	rows := max(a.height-1, 4)
	used := 2
	for _, l := range lines {
		if used >= rows {
			if !a.st.e.pauseMore(a.st.s, a.st.terminal, a.st.outputMode, str.FileMorePrompt) {
				break
			}
			used = 0
		}
		a.write(l + "\r\n")
		used++
	}
	if a.notice != "" {
		a.write("\r\n" + string(ansi.ReplacePipeCodes([]byte(a.notice))) + "\r\n")
	}
	a.write("\r\n")
}

// readLine prompts with a strings.json value and reads a line, starting with
// initial already typed.
func (a *acsTesterScreen) readLine(prompt, initial string) (string, error) {
	a.st.e.writeString(a.st.terminal, a.st.outputMode, prompt)
	line, err := readLineFromSessionIHFrom(a.st.s, a.st.terminal, initial)
	return strings.TrimSpace(line), err
}

func (a *acsTesterScreen) readExpression() error {
	line, err := a.readLine(a.st.e.Strings().AcsTestExprPrompt, a.expr)
	if err != nil {
		return err
	}
	if line != "" {
		a.expr = line
	}
	return nil
}

// readTime sets the time the clock terms read; blank is the current time.
func (a *acsTesterScreen) readTime() error {
	line, err := a.readLine(a.st.e.Strings().AcsTestTimePrompt, "")
	if err != nil {
		return err
	}
	at, perr := acs.ParseTesterTime(line, time.Now())
	if perr != nil {
		a.notice = a.st.e.Strings().AcsTestBadTime
		return nil
	}
	a.tester.At = at
	return nil
}

// readSession asks whether to test with a session and, if so, for the
// values the session terms read. Answering no goes back to no session.
func (a *acsTesterScreen) readSession() error {
	str := a.st.e.Strings()
	yesNo := func(prompt string, def bool) (bool, error) {
		yes, err := a.st.e.PromptYesNoString(a.st.s, a.st.terminal, prompt, a.st.outputMode, a.st.nodeNumber, a.width, a.height, def)
		a.write("\r\n")
		return yes, err
	}
	// Enter keeps what's there: a session stays, none stays none.
	on, err := yesNo(str.AcsTestSessionPrompt, a.tester.Session != nil)
	if err != nil || !on {
		a.tester.Session = nil
		return err
	}
	s := acs.TesterSession{Terminal: true}
	if a.tester.Session != nil {
		s = *a.tester.Session
	}
	if s.Local, err = yesNo(str.AcsTestLocalPrompt, s.Local); err != nil {
		return err
	}
	if s.Terminal, err = yesNo(str.AcsTestTerminalPrompt, s.Terminal); err != nil {
		return err
	}
	// A bad answer asks that prompt again, keeping the answers before it; a
	// blank one keeps the current value.
	minutes := func(prompt string, cur time.Duration) (time.Duration, error) {
		for {
			line, err := a.readLine(prompt, strconv.Itoa(int(cur/time.Minute)))
			if err != nil {
				return 0, err
			}
			if line == "" {
				return cur, nil
			}
			if n, perr := strconv.Atoi(line); perr == nil && n >= 0 {
				return time.Duration(n) * time.Minute, nil
			}
			a.write("\r\n" + string(ansi.ReplacePipeCodes([]byte(str.AcsTestBadNumber))) + "\r\n")
		}
	}
	if s.Elapsed, err = minutes(str.AcsTestElapsedPrompt, s.Elapsed); err != nil {
		return err
	}
	if s.Credit, err = minutes(str.AcsTestCreditPrompt, s.Credit); err != nil {
		return err
	}
	a.tester.Session = &s
	return nil
}
