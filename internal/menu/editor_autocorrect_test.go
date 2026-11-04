package menu

import (
	"strings"
	"testing"

	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func boolPtr(v bool) *bool { return &v }

// Autocorrect is on only where the area (or, when the area does not say,
// the board) has it on and the caller has not turned it off.
func TestEditorAutocorrectRule(t *testing.T) {
	on, off := &message.MessageArea{Autocorrect: boolPtr(true)}, &message.MessageArea{Autocorrect: boolPtr(false)}
	unset := &message.MessageArea{}
	callerOn, callerOff := &user.User{}, &user.User{AutocorrectOff: true}
	for _, tc := range []struct {
		name        string
		area        *message.MessageArea
		board       bool
		u           *user.User
		offered, ok bool
	}{
		{"area on, board off", on, false, callerOn, true, true},
		{"area off, board on", off, true, callerOn, false, false},
		{"area unset follows board on", unset, true, callerOn, true, true},
		{"area unset follows board off", unset, false, callerOn, false, false},
		{"no area follows board on", nil, true, callerOn, true, true},
		{"no area follows board off", nil, false, callerOn, false, false},
		{"caller off in an area that has it", on, false, callerOff, true, false},
		{"caller off with the board default", nil, true, callerOff, true, false},
		{"caller off where it is not offered", off, true, callerOff, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := editorAutocorrect(editor.EditorContext{}, tc.area, tc.board, tc.u)
			offered, ok := ctx.Autocorrect, ctx.Autocorrect && !ctx.AutocorrectOff
			if offered != tc.offered || ok != tc.ok {
				t.Errorf("got offered=%v on=%v, want offered=%v on=%v", offered, ok, tc.offered, tc.ok)
			}
		})
	}
}

// withAutocorrect hands the editor the caller's choice where autocorrect is
// offered, and Ctrl-O's save writes it to the caller's record.
func TestWithAutocorrect(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	e := &MenuExecutor{}
	e.SetServerConfig(config.ServerConfig{})

	if ctx := withAutocorrect(e, editor.EditorContext{}, &message.MessageArea{}, u, um); ctx.Autocorrect || ctx.SaveAutocorrect != nil {
		t.Errorf("offered with the board default off: %+v", ctx)
	}

	ctx := withAutocorrect(e, editor.EditorContext{NodeNumber: 3}, &message.MessageArea{Autocorrect: boolPtr(true)}, u, um)
	if !ctx.Autocorrect || ctx.AutocorrectOff || ctx.NodeNumber != 3 || ctx.SaveAutocorrect == nil {
		t.Fatalf("area on: %+v", ctx)
	}
	ctx.SaveAutocorrect(false)
	if !u.AutocorrectOff || !reloadUser(t, um).AutocorrectOff {
		t.Error("Ctrl-O off was not saved")
	}
	if ctx := withAutocorrect(e, editor.EditorContext{}, &message.MessageArea{Autocorrect: boolPtr(true)}, u, um); !ctx.Autocorrect || !ctx.AutocorrectOff {
		t.Errorf("saved choice not handed on: %+v", ctx)
	}
}

// runAutocorrectKonfig drives USERCONFIG on a board with the given default
// and message areas.
func runAutocorrectKonfig(t *testing.T, um *user.UserMgr, u *user.User, board bool, areas []message.MessageArea, keys string) *testterm.Term {
	t.Helper()
	e := konfigTestExecutor(t)
	e.SetServerConfig(config.ServerConfig{EditorAutocorrect: board})
	if len(areas) > 0 {
		mm, err := message.NewMessageManager(t.TempDir(), t.TempDir(), "TestBBS", nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range areas {
			if _, err := mm.AddArea(a); err != nil {
				t.Fatal(err)
			}
		}
		e.MessageMgr = mm
	}
	screen := testterm.New(80, 24)
	sess := testterm.NewSession(screen, keys)
	t.Cleanup(func() {
		resetSessionIH(sess)
		sessionTermSizes.Delete(sess)
	})
	c := &cmdCtx{e: e, s: sess, terminal: term.NewTerminal(sess, ""), userManager: um, currentUser: u,
		nodeNumber: 1, outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24}
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	return screen
}

// The caller's switch is shown only where autocorrect is offered somewhere,
// so boards that never turn it on see no change.
func TestKonfigAutocorrectShownWhereOffered(t *testing.T) {
	areaOn := message.MessageArea{Tag: "FIX", Name: "Fixed", AreaType: "local", Autocorrect: boolPtr(true)}
	areaUnset := message.MessageArea{Tag: "PLAIN", Name: "Plain", AreaType: "local"}
	for _, tc := range []struct {
		name  string
		board bool
		areas []message.MessageArea
		shown bool
	}{
		{"nowhere", false, []message.MessageArea{areaUnset}, false},
		{"board default", true, nil, true},
		{"one area", false, []message.MessageArea{areaUnset, areaOn}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			um, u := newUserConfigTestUser(t)
			snap := runAutocorrectKonfig(t, um, u, tc.board, tc.areas, "q").Snapshot()
			if got := strings.Contains(snap, "[P] Autocorrect"); got != tc.shown {
				t.Errorf("Autocorrect shown = %v, want %v:\n%s", got, tc.shown, snap)
			}
		})
	}
}

// P switches the caller's autocorrect and saves it on their record, the same
// field Ctrl-O in the editor writes.
func TestKonfigAutocorrectToggleSaves(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	screen := runAutocorrectKonfig(t, um, u, true, nil, "pq")
	if !u.AutocorrectOff || !reloadUser(t, um).AutocorrectOff {
		t.Fatal("Autocorrect not switched off and saved")
	}
	if !strings.Contains(screen.Row(konfigEditRow), "Autocorrect is now Off") {
		t.Errorf("status row = %q", screen.Row(konfigEditRow))
	}
	runAutocorrectKonfig(t, um, u, true, nil, "pq")
	if u.AutocorrectOff || reloadUser(t, um).AutocorrectOff {
		t.Error("Autocorrect not switched back on")
	}
}
