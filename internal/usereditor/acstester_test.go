package usereditor

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/ViSiON-3/vision-3-bbs/internal/acs"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// acsTesterOver opens the tester on Bob, a level-20 user with flags A and C.
func acsTesterOver(t *testing.T, w, h int) Model {
	t.Helper()
	m, _ := editorOver(t,
		&user.User{ID: 1, Handle: "Alice", AccessLevel: 255},
		&user.User{ID: 2, Handle: "Bob", AccessLevel: 20, Flags: "AC", Validated: true})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = updated.(Model)
	m = pinBackdrop(m, m.width, m.height)
	m = keys(t, m, tea.KeyMsg{Type: tea.KeyDown}) // to Bob
	m = enterMode(t, m, modeACSTest)
	if u := m.editingUser(); u.Handle != "Bob" {
		t.Fatalf("testing %s, want Bob", u.Handle)
	}
	return m
}

func keys(t *testing.T, m Model, msgs ...tea.KeyMsg) Model {
	t.Helper()
	for _, k := range msgs {
		u, _ := m.Update(k)
		m = u.(Model)
	}
	return m
}

func typed(s string) []tea.KeyMsg {
	var out []tea.KeyMsg
	for _, r := range s {
		out = append(out, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return out
}

var (
	enter = tea.KeyMsg{Type: tea.KeyEnter}
	tab   = tea.KeyMsg{Type: tea.KeyTab}
	space = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
)

// TestACSTesterDefaultContext: the tester opens with no session and the
// current time, says so before anything is tested, and shows what
// acs.Render gives for the user being edited.
func TestACSTesterDefaultContext(t *testing.T) {
	m := acsTesterOver(t, 80, 25)
	view := stripANSIGolden(m.View())
	for _, want := range []string{"ACS Tester: Bob", "Session:    none: session terms fail,", "(now)", "Session   : N"} {
		if !strings.Contains(view, want) {
			t.Errorf("screen lacks %q:\n%s", want, view)
		}
	}
	m = keys(t, m, append(typed("S50&(FA|!Q)"), enter)...)
	want := acs.Render(acs.Tester{SysOpLevel: 255, CoSysOpLevel: 250}.Explain("S50&(FA|!Q)", m.editingUser()), acsOutputWidth)
	if got := m.acsTest.lines; strings.Join(got[3:], "\n") != strings.Join(want[3:], "\n") {
		t.Errorf("lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	view = stripANSIGolden(m.View())
	if !strings.Contains(view, "Q is not an ACS term") {
		t.Errorf("explanation not on screen:\n%s", view)
	}
	if m = keys(t, m, tea.KeyMsg{Type: tea.KeyEscape}); m.mode != modeEdit {
		t.Errorf("Esc left mode %v, want the edit screen", m.mode)
	}
}

// TestACSTesterSession turns a session on, which tests again at once, and
// sets the time and the minutes T reads.
func TestACSTesterSession(t *testing.T) {
	m := acsTesterOver(t, 80, 25)
	m = keys(t, m, typed("T30&A&H22")...)
	m = keys(t, m, tab)
	m = keys(t, m, typed("22:15")...)
	m = keys(t, m, tab, space) // Session on: tests again
	if !m.acsTest.session {
		t.Fatal("Space didn't turn the session on")
	}
	if got := strings.Join(m.acsTest.lines, "\n"); !strings.Contains(got, "remote call, a terminal, 0 min in") || !strings.Contains(got, "Result:     PASS") {
		t.Errorf("after the session went on:\n%s", got)
	}
	m = keys(t, m, tab, tab, tab) // Local, Terminal, Mins In
	m = keys(t, m, typed("45")...)
	m = keys(t, m, enter)
	got := strings.Join(m.acsTest.lines, "\n")
	if !strings.Contains(got, "45 min in") || !strings.Contains(got, "minutes left") && !strings.Contains(got, "no time limit") {
		t.Errorf("after 45 minutes in:\n%s", got)
	}
	m = keys(t, m, tab)
	m = keys(t, m, typed("x")...)
	m = keys(t, m, enter)
	if got := strings.Join(m.acsTest.lines, "\n"); !strings.Contains(got, "whole number of minutes") {
		t.Errorf("a bad number: %s", got)
	}
}

// TestACSTesterGolden archives the tester screen with a fixed time.
func TestACSTesterGolden(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	for _, size := range []struct{ w, h int }{{80, 25}, {120, 45}} {
		m := acsTesterOver(t, size.w, size.h)
		m = keys(t, m, typed("S50&(FA|!Q)")...)
		m = keys(t, m, tab)
		m = keys(t, m, typed("2026-11-04 22:00")...)
		m = keys(t, m, tab, enter)
		checkGolden(t, fmt.Sprintf("acs_test_%dx%d", size.w, size.h), m.View())
	}
}

// TestACSTesterCallValues sets the call's node, connection, encoding, width
// and address on the second session row; a value that can't be used says
// so, and a local call skips the address, which is loopback.
func TestACSTesterCallValues(t *testing.T) {
	m := acsTesterOver(t, 80, 25)
	m = keys(t, m, typed("NODE=4&CONN=telnet&ENC=utf8&WIDTH>=132&IP:10.0.0.0/8")...)
	m = keys(t, m, tab, tab, space) // Time, Session on
	view := stripANSIGolden(m.View())
	if !strings.Contains(view, "Node      :") || !strings.Contains(view, "IP:") {
		t.Fatalf("no call row:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > 80 {
			t.Errorf("a line is %d cells wide: %q", w, line)
		}
	}
	m = keys(t, m, tab, tab, tab, tab, tab) // Local, Terminal, Mins In, Credit, Node
	m = keys(t, m, typed("4")...)
	m = keys(t, m, tab)
	m = keys(t, m, typed("ftp")...)
	m = keys(t, m, enter)
	if got := strings.Join(m.acsTest.lines, "\n"); !strings.Contains(got, "Conn is one of ssh, telnet, local") {
		t.Fatalf("a bad connection: %s", got)
	}
	m = keys(t, m, tea.KeyMsg{Type: tea.KeyBackspace}, tea.KeyMsg{Type: tea.KeyBackspace}, tea.KeyMsg{Type: tea.KeyBackspace})
	m = keys(t, m, typed("telnet")...)
	m = keys(t, m, tab)
	m = keys(t, m, typed("utf8")...)
	m = keys(t, m, tab)
	m = keys(t, m, typed("132")...)
	m = keys(t, m, tab)
	m = keys(t, m, typed("10.1.2.3")...)
	m = keys(t, m, enter)
	got := strings.Join(m.acsTest.lines, "\n")
	if !strings.Contains(got, "node 4, telnet from 10.1.2.3, utf8, 132 columns") || !strings.Contains(got, "Result:     PASS") {
		t.Errorf("with the call's values:\n%s", got)
	}
	m.acsTest.local = true
	if slices.Contains(m.acsTest.order(), acsAddr) {
		t.Error("a local call still asks for its address")
	}
}

// The IP field takes the longest IPv6 text there is, an IPv4-mapped
// address written out in full, without cutting off its last digit.
func TestACSTesterLongIPv6(t *testing.T) {
	m := acsTesterOver(t, 80, 25)
	const long = "0000:0000:0000:0000:0000:ffff:192.0.2.10"
	m.acsTest.inputs[acsAddr].SetValue(long)
	s := &acs.TesterSession{}
	if msg := m.acsTest.callValues(s); msg != "" {
		t.Fatalf("callValues: %s", msg)
	}
	if want := netip.MustParseAddr(long); s.Addr != want {
		t.Errorf("address = %s, want %s", s.Addr, want)
	}
}
