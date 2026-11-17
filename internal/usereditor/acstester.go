package usereditor

import (
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/acs"
	"github.com/ViSiON-3/vision-3-bbs/internal/perm"
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
	// The call's values for the session terms (#1075), on a row of their
	// own. A local call's address is loopback, so IP is asked only for a
	// remote one.
	acsNode
	acsConn
	acsEnc
	acsWidth
	acsAddr
)

// acsTester is the ACS tester's state (#608): an expression and an explicit
// context for the user being edited, by default no session and the current
// time. The result comes from acs.Render, the same lines the BBS shows.
type acsTester struct {
	inputs                   map[int]*textinput.Model // the text fields: expression, time, minutes, call values
	session, local, terminal bool
	focus                    int
	lines                    []string // what the output area shows
	scroll                   int
	sysOp, coSysOp           int            // sysOpLevel and coSysOpLevel, for SYSOP, COSYSOP and T
	location                 *time.Location // the board's timezone, for AGE
	roles                    *perm.Roles    // configs/roles.json's, for ROLE and PERM
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
			{acsNode, 4, "1"},
			{acsConn, 6, acs.TransportSSH},
			{acsEnc, 5, acs.EncodingCP437},
			{acsWidth, 4, "80"},
			{acsAddr, 45, acs.TesterDefaultAddr.String()}, // the longest IPv6 text, IPv4 tail included
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
		t.inputs[acsNode].Width = 4
		t.inputs[acsConn].Width = 6
		t.inputs[acsEnc].Width = 5
		t.inputs[acsWidth].Width = 4
		t.inputs[acsAddr].Width = 13
	}
	t.session, t.local, t.terminal = false, false, true
	t.inputs[acsTime].SetValue("")
	for _, id := range []int{acsElapsed, acsCredit, acsNode, acsConn, acsEnc, acsWidth, acsAddr} {
		t.inputs[id].SetValue("")
	}
	t.sysOp, t.coSysOp, t.location = m.sysOpLevel, m.coSysOpLevel, m.boardLocation
	t.roles = m.roles
	t.scroll = 0
	m.mode = modeACSTest
	t.setFocus(acsExpr)
	t.lines = acs.ContextLines(acs.Tester{SysOpLevel: t.sysOp, CoSysOpLevel: t.coSysOp, Location: m.boardLocation, Roles: t.roles}, m.editingUser(), acsOutputWidth)
}

// order is the fields that can take focus now.
func (t *acsTester) order() []int {
	if !t.session {
		return []int{acsExpr, acsTime, acsSession}
	}
	order := []int{acsExpr, acsTime, acsSession, acsLocal, acsTerminal, acsElapsed, acsCredit, acsNode, acsConn, acsEnc, acsWidth}
	if !t.local {
		order = append(order, acsAddr)
	}
	return order
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
	at, err := acs.ParseTesterTime(t.inputs[acsTime].Value(), time.Now(), t.location)
	if err != nil {
		return acs.Tester{}, "Time: enter hh:mm or yyyy-mm-dd hh:mm, or leave it blank for now."
	}
	tc := acs.Tester{At: at, SysOpLevel: t.sysOp, CoSysOpLevel: t.coSysOp, Location: t.location, Roles: t.roles}
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
		s := &acs.TesterSession{Local: t.local, Terminal: t.terminal, Elapsed: elapsed, Credit: credit}
		if msg := t.callValues(s); msg != "" {
			return acs.Tester{}, msg
		}
		tc.Session = s
	}
	return tc, ""
}

// callValues reads the call's node, connection, encoding, width and, for
// a remote call, address into s; a blank field is the default. It returns
// the message to show for a value that can't be used.
func (t *acsTester) callValues(s *acs.TesterSession) string {
	value := func(id int) string { return strings.ToLower(strings.TrimSpace(t.inputs[id].Value())) }
	whole := func(id int, set *int) bool {
		v := value(id)
		if v == "" {
			return true
		}
		n, err := strconv.Atoi(v)
		*set = n
		return err == nil && n >= 1
	}
	if !whole(acsNode, &s.NodeNum) || !whole(acsWidth, &s.Cols) {
		return "Node and Width take a whole number from 1."
	}
	if s.Conn = value(acsConn); s.Conn != "" && !slices.Contains(acs.Transports, s.Conn) {
		return "Conn is one of " + strings.Join(acs.Transports, ", ") + "."
	}
	if s.Enc = value(acsEnc); s.Enc != "" && !slices.Contains(acs.Encodings, s.Enc) {
		return "Enc is one of " + strings.Join(acs.Encodings, ", ") + "."
	}
	if v := value(acsAddr); v != "" && !t.local {
		a, err := netip.ParseAddr(v)
		if err != nil || a.Zone() != "" {
			return "IP takes an address, such as 10.0.0.5 or fd00::5."
		}
		s.Addr = a
	}
	return ""
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

// formRows is the form above the output: expression, time, session, the
// call's values while there is a session, and a separator.
func (t acsTester) formRows() int {
	if t.session {
		return 5
	}
	return 4
}

// acsOutputRows is how many output lines fit: the screen less the title and
// help bars, the box borders and the form.
func (m Model) acsOutputRows() int { return max(m.height-4-m.acsTest.formRows(), 1) }

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
		text(&r, "  none: session terms fail, T starts now")
	}
	end(&r)
	if t.session {
		r = acsRow{}
		// A blank Conn is local for a local call; the field's hint says so.
		t.inputs[acsConn].Placeholder = (&acs.TesterSession{Local: t.local}).Transport()
		field(&r, acsNode, " Node      ", 4, "")
		field(&r, acsConn, "  Conn", 6, "")
		field(&r, acsEnc, "  Enc", 5, "")
		field(&r, acsWidth, "  Width", 4, "")
		if t.local {
			text(&r, "  IP: 127.0.0.1")
		} else {
			field(&r, acsAddr, "  IP", 14, "")
		}
		end(&r)
	}
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
