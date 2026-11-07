package usereditor

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/acs"
	"github.com/ViSiON-3/vision-3-bbs/internal/tuiart"
)

// acsTestField labels the edit-screen action that opens the ACS tester.
const acsTestField = "ACS Test"

// The tester's form, in focus order. The session values come after Session
// and are shown only when it is on.
const (
	acsExpr = iota
	acsTime
	acsSession
	acsLocal
	acsTerminal
	acsElapsed
	acsCredit
)

// acsTester is the ACS tester's state (#608): an expression and an explicit
// context for the user being edited, by default no session and the current
// time. The result comes from acs.Render, the same lines the BBS shows.
type acsTester struct {
	inputs                   map[int]*textinput.Model // the text fields: expression, time, minutes
	session, local, terminal bool
	focus                    int
	lines                    []string // what the output area shows
	scroll                   int
	sysOp, coSysOp           int            // sysOpLevel and coSysOpLevel, for SYSOP, COSYSOP and T
	location                 *time.Location // the board's timezone, for AGE
}

// acsTestFieldDef is the edit-screen action for the tester.
func acsTestFieldDef() fieldDef {
	return fieldDef{
		Label: acsTestField, Type: ftAction, Col: rightCol, Row: 16, Width: 16,
		Get: func(*userType) string { return "(Enter)" },
	}
}

// openACSTester switches to modeACSTest with the default context. The
// expression typed last time is kept, as a convenience for testing it
// against several users.
func (m *Model) openACSTester() {
	t := &m.acsTest
	if t.inputs == nil {
		t.inputs = map[int]*textinput.Model{}
		for _, f := range []struct {
			id, limit int
			holder    string
		}{
			{acsExpr, 255, "e.g. s20&fC"},
			{acsTime, 16, "now"},
			{acsElapsed, 4, "0"},
			{acsCredit, 4, "0"},
		} {
			ti := textinput.New()
			ti.Prompt = ""
			ti.CharLimit = f.limit
			ti.Placeholder = f.holder
			t.inputs[f.id] = &ti
		}
		// One cell less than the field shows, for the cursor.
		t.inputs[acsExpr].Width = 60
		t.inputs[acsTime].Width = 16
		t.inputs[acsElapsed].Width = 4
		t.inputs[acsCredit].Width = 4
	}
	t.session, t.local, t.terminal = false, false, true
	t.inputs[acsTime].SetValue("")
	t.inputs[acsElapsed].SetValue("")
	t.inputs[acsCredit].SetValue("")
	t.sysOp, t.coSysOp, t.location = m.sysOpLevel, m.coSysOpLevel, m.boardLocation
	t.scroll = 0
	m.mode = modeACSTest
	t.setFocus(acsExpr)
	t.lines = acs.ContextLines(acs.Tester{SysOpLevel: t.sysOp, CoSysOpLevel: t.coSysOp, Location: m.boardLocation}, m.editingUser(), acsOutputWidth)
}

// order is the fields that can take focus now.
func (t *acsTester) order() []int {
	if t.session {
		return []int{acsExpr, acsTime, acsSession, acsLocal, acsTerminal, acsElapsed, acsCredit}
	}
	return []int{acsExpr, acsTime, acsSession}
}

func (t *acsTester) setFocus(f int) {
	t.focus = f
	for id, in := range t.inputs {
		if id == f {
			in.Focus()
		} else {
			in.Blur()
		}
	}
}

func (t *acsTester) move(dir int) {
	order := t.order()
	i := 0
	for j, f := range order {
		if f == t.focus {
			i = j
		}
	}
	t.setFocus(order[(i+dir+len(order))%len(order)])
}

// toggle flips the yes/no field in focus and reports whether there was one.
func (t *acsTester) toggle(set *bool) bool {
	var b *bool
	switch t.focus {
	case acsSession:
		b = &t.session
	case acsLocal:
		b = &t.local
	case acsTerminal:
		b = &t.terminal
	default:
		return false
	}
	if set != nil {
		*b = *set
	} else {
		*b = !*b
	}
	return true
}

// tester builds the context from the form, or returns the message to show.
func (t *acsTester) tester() (acs.Tester, string) {
	at, err := acs.ParseTesterTime(t.inputs[acsTime].Value(), time.Now())
	if err != nil {
		return acs.Tester{}, "Time: enter hh:mm or yyyy-mm-dd hh:mm, or leave it blank for now."
	}
	tc := acs.Tester{At: at, SysOpLevel: t.sysOp, CoSysOpLevel: t.coSysOp, Location: t.location}
	if t.session {
		minutes := func(id int) (time.Duration, bool) {
			v := strings.TrimSpace(t.inputs[id].Value())
			if v == "" {
				return 0, true
			}
			n, err := strconv.Atoi(v)
			return time.Duration(n) * time.Minute, err == nil && n >= 0
		}
		elapsed, ok1 := minutes(acsElapsed)
		credit, ok2 := minutes(acsCredit)
		if !ok1 || !ok2 {
			return acs.Tester{}, "Minutes In and Chat Credit take a whole number of minutes."
		}
		tc.Session = &acs.TesterSession{Local: t.local, Terminal: t.terminal, Elapsed: elapsed, Credit: credit}
	}
	return tc, ""
}

// run tests the expression, or shows the context alone while there is none.
func (m *Model) runACSTest() {
	t := &m.acsTest
	t.scroll = 0
	tc, msg := t.tester()
	if msg != "" {
		t.lines = []string{msg}
		return
	}
	expr := strings.TrimSpace(t.inputs[acsExpr].Value())
	if expr == "" {
		t.lines = acs.ContextLines(tc, m.editingUser(), acsOutputWidth)
		return
	}
	t.lines = acs.Render(tc.Explain(expr, m.editingUser()), acsOutputWidth)
}

// updateACSTest handles keys on the tester screen. Enter tests; changing a
// yes/no field tests again at once, so the result on screen always matches
// the context shown with it.
func (m Model) updateACSTest(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	t := &m.acsTest
	switch msg.Type {
	case tea.KeyEscape:
		m.mode = modeEdit
		return m, nil
	case tea.KeyEnter:
		m.runACSTest()
		return m, nil
	case tea.KeyTab, tea.KeyDown:
		t.move(1)
		return m, textinput.Blink
	case tea.KeyShiftTab, tea.KeyUp:
		t.move(-1)
		return m, textinput.Blink
	case tea.KeyPgDown:
		t.scroll = min(t.scroll+m.acsOutputRows(), max(len(t.lines)-m.acsOutputRows(), 0))
		return m, nil
	case tea.KeyPgUp:
		t.scroll = max(t.scroll-m.acsOutputRows(), 0)
		return m, nil
	}
	if in, ok := t.inputs[t.focus]; ok {
		var cmd tea.Cmd
		*in, cmd = in.Update(msg)
		return m, cmd
	}
	yes, no := true, false
	switch strings.ToLower(msg.String()) {
	case " ":
		t.toggle(nil)
	case "y":
		t.toggle(&yes)
	case "n":
		t.toggle(&no)
	default:
		return m, nil
	}
	m.runACSTest()
	return m, nil
}

// acsRow builds one form row and counts its cells.
type acsRow struct {
	b strings.Builder
	n int
}

func (r *acsRow) add(s string, cells int) {
	r.b.WriteString(s)
	r.n += cells
}

// acsOutputWidth is the width the explanation is laid out to: the edit box
// less a column of margin each side.
const acsOutputWidth = 74

// acsFormRows is the form above the output: expression, time, session and a
// separator.
const acsFormRows = 4

// acsOutputRows is how many output lines fit: the screen less the title and
// help bars, the box borders and the form.
func (m Model) acsOutputRows() int { return max(m.height-4-acsFormRows, 1) }

// viewACSTestScreen draws the tester in the edit screen's box.
func (m Model) viewACSTestScreen() string {
	t := m.acsTest
	u := m.editingUser()
	const boxW = 76
	sc := tuiart.NewScreen(m.width, m.backdrop)
	sc.Line(editTitleStyle.Render(centerText("-- ViSiON/3 ACS Tester: "+u.Handle+" --", m.width)))
	padL := max(0, (m.width-boxW-2)/2)
	padR := max(0, m.width-padL-boxW-2)
	row := func(content string) {
		sc.Line(sc.Pad(padL, padR, editBorderStyle.Render("│")+content+editBorderStyle.Render("│")))
	}
	sc.Line(sc.Pad(padL, padR, editBorderStyle.Render("╒"+strings.Repeat("═", boxW)+"╕")))

	// field renders one label and value w cells wide; a focused text field
	// shows its input, cursor included.
	field := func(r *acsRow, id int, label string, w int, value string) {
		r.add(fieldLabelStyle.Render(label+": "), len(label)+2)
		if in, ok := t.inputs[id]; ok {
			if t.focus == id {
				view, n := tuiart.FitInput(in.View(), w)
				r.add(view+fieldDisplayStyle.Render(strings.Repeat(" ", max(w-n, 0))), w)
				return
			}
			if value = in.Value(); value == "" {
				value = in.Placeholder
			}
		}
		style := fieldDisplayStyle
		if t.focus == id {
			style = fieldEditStyle
		}
		r.add(style.Render(padRight(value, w)), w)
	}
	text := func(r *acsRow, s string) { r.add(fieldDisplayStyle.Render(s), len(s)) }
	yn := func(b bool) string {
		if b {
			return "Y"
		}
		return "N"
	}
	end := func(r *acsRow) { row(r.b.String() + fieldDisplayStyle.Render(strings.Repeat(" ", max(boxW-r.n, 0)))) }

	var r acsRow
	field(&r, acsExpr, " Expression", 61, "")
	end(&r)
	r = acsRow{}
	field(&r, acsTime, " Time      ", 17, "")
	text(&r, "  hh:mm or yyyy-mm-dd hh:mm; blank is now")
	end(&r)
	r = acsRow{}
	field(&r, acsSession, " Session   ", 1, yn(t.session))
	if t.session {
		field(&r, acsLocal, "  Local", 1, yn(t.local))
		field(&r, acsTerminal, "  Terminal", 1, yn(t.terminal))
		field(&r, acsElapsed, "  Mins In", 5, "")
		field(&r, acsCredit, "  Credit", 5, "")
	} else {
		text(&r, "  none: L/A fail the string, T starts now")
	}
	end(&r)
	row(separatorStyle.Render(strings.Repeat("─", boxW)))

	rows := m.acsOutputRows()
	for i := 0; i < rows; i++ {
		text := ""
		if j := t.scroll + i; j < len(t.lines) {
			text = t.lines[j]
		}
		row(editInfoValueStyle.Render(" " + padRight(text, boxW-1)))
	}
	sc.Line(sc.Pad(padL, padR, editBorderStyle.Render("╘"+strings.Repeat("═", boxW)+"╛")))
	sc.Last(helpBarStyle.Render(centerText("Enter - Test  Tab - Next Field  Space - Toggle  PgUp/PgDn - Scroll  ESC - Back", m.width)))
	return sc.String()
}
