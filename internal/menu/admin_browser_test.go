package menu

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runAdminBrowser drives adminUserLightbarBrowser through the test harness with
// the given scripted keystrokes, returning its result plus the captured output.
func runAdminBrowser(t *testing.T, users []*user.User, input string, selectOnEnter bool) (*user.User, bool, error, string) {
	t.Helper()
	ts := newTestSession(input)
	terminal := newTestTerminal(ts)
	u, ok, err := adminUserLightbarBrowser(ts, terminal, users, "Test Title", "pick one", ansi.OutputModeUTF8, selectOnEnter)
	resetSessionIH(ts)
	return u, ok, err, ts.output()
}

func browserTestUsers() []*user.User {
	return []*user.User{
		{ID: 1, Handle: "Alice", Validated: true},
		{ID: 2, Handle: "Bob", Validated: true},
		{ID: 3, Handle: "Carol", Validated: true},
	}
}

func TestAdminBrowser_QuitNoSelection(t *testing.T) {
	u, ok, err, out := runAdminBrowser(t, browserTestUsers(), "Q", true)
	if u != nil || ok || err != nil {
		t.Fatalf("quit = (%v, %v, %v), want (nil, false, nil)", u, ok, err)
	}
	if !strings.Contains(out, "Test Title") {
		t.Error("rendered output should contain the title")
	}
	if !strings.Contains(out, "Alice") {
		t.Error("rendered output should list the users")
	}
}

func TestAdminBrowser_EnterSelectsCurrent(t *testing.T) {
	u, ok, err, _ := runAdminBrowser(t, browserTestUsers(), "\r", true)
	if err != nil || !ok || u == nil || u.Handle != "Alice" {
		t.Fatalf("enter selected (%v, %v, %v), want Alice/true/nil", u, ok, err)
	}
}

func TestAdminBrowser_NavigateDownThenSelect(t *testing.T) {
	u, ok, _, _ := runAdminBrowser(t, browserTestUsers(), "j\r", true)
	if !ok || u == nil || u.Handle != "Bob" {
		t.Fatalf("j then enter selected %v, want Bob", u)
	}
}

func TestAdminBrowser_ArrowDownThenSelect(t *testing.T) {
	// Exercises ANSI escape-sequence decoding end-to-end through the harness.
	u, ok, _, _ := runAdminBrowser(t, browserTestUsers(), "\x1b[B\r", true)
	if !ok || u == nil || u.Handle != "Bob" {
		t.Fatalf("arrow-down then enter selected %v, want Bob", u)
	}
}

func TestAdminBrowser_NavigateUpClampsAtTop(t *testing.T) {
	// 'k' at the top stays on the first user.
	u, ok, _, _ := runAdminBrowser(t, browserTestUsers(), "k\r", true)
	if !ok || u == nil || u.Handle != "Alice" {
		t.Fatalf("k then enter selected %v, want Alice (clamped at top)", u)
	}
}

func TestAdminBrowser_EOFReturnsEOF(t *testing.T) {
	u, ok, err, _ := runAdminBrowser(t, browserTestUsers(), "", true)
	if !errors.Is(err, io.EOF) || u != nil || ok {
		t.Fatalf("EOF case = (%v, %v, %v), want (nil, false, io.EOF)", u, ok, err)
	}
}

func TestAdminBrowser_EnterIgnoredWhenSelectDisabled(t *testing.T) {
	// With selectOnEnter=false, Enter does nothing; input then hits EOF.
	u, ok, err, _ := runAdminBrowser(t, browserTestUsers(), "\r", false)
	if !errors.Is(err, io.EOF) || u != nil || ok {
		t.Fatalf("enter (selectOnEnter=false) = (%v, %v, %v), want (nil, false, io.EOF)", u, ok, err)
	}
}

func TestAdminBrowser_EmptyUserList(t *testing.T) {
	u, ok, err := adminUserLightbarBrowser(newTestSession(""), newTestTerminal(newTestSession("")), nil, "T", "i", ansi.OutputModeUTF8, true)
	if u != nil || ok || err != nil {
		t.Fatalf("empty list = (%v, %v, %v), want (nil, false, nil)", u, ok, err)
	}
}
