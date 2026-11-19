package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"golang.org/x/term"
)

// runKonfigSixel is runKonfig on a session whose terminal answered the
// connect probe with sixel.
func runKonfigSixel(t *testing.T, um *user.UserMgr, u *user.User, keys string, sixel bool) (*testterm.Term, *user.User) {
	t.Helper()
	screen := testterm.New(80, 24)
	sess := testterm.NewSession(screen, keys)
	storeSessionSixel(sess, sixel)
	t.Cleanup(func() {
		resetSessionIH(sess)
		sessionTermSizes.Delete(sess)
		sessionSixelCaps.Delete(sess)
	})
	c := &cmdCtx{
		e:           konfigTestExecutor(t),
		s:           sess,
		terminal:    term.NewTerminal(sess, ""),
		userManager: um,
		currentUser: u,
		nodeNumber:  1,
		outputMode:  ansi.OutputModeUTF8,
		termWidth:   80,
		termHeight:  24,
	}
	got, _, err := runUserKonfig(c, "")
	if err != nil {
		t.Fatalf("runUserKonfig: %v", err)
	}
	return screen, got
}

// The setting cycles Auto, what the terminal answered, the opposite, and
// back to Auto, and is saved each time.
func TestKonfigSixelCycle(t *testing.T) {
	for _, tc := range []struct {
		probed bool
		want   []string
	}{
		{false, []string{user.SixelOff, user.SixelOn, ""}},
		{true, []string{user.SixelOn, user.SixelOff, ""}},
	} {
		um, u := newUserConfigTestUser(t)
		for _, want := range tc.want {
			_, u = runKonfigSixel(t, um, u, "wq", tc.probed)
			if u.PreferredSixel != want || reloadUser(t, um).PreferredSixel != want {
				t.Fatalf("probed %v: PreferredSixel = %q, want %q", tc.probed, u.PreferredSixel, want)
			}
		}
	}
}

func TestKonfigSixelValue(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	for _, tc := range []struct {
		pref   string
		probed bool
		want   string
	}{
		{"", false, "Auto (Off)"},
		{"", true, "Auto (On)"},
		{user.SixelOn, false, "On"},
		{user.SixelOff, true, "Off"},
	} {
		u.PreferredSixel = tc.pref
		screen, _ := runKonfigSixel(t, um, u, "q", tc.probed)
		if got := konfigFieldText(screen, "[W] Sixel Graphics"); !strings.HasPrefix(got, tc.want) {
			t.Errorf("pref %q, probed %v: Sixel Graphics shows %q, want %q", tc.pref, tc.probed, got, tc.want)
		}
	}
}

func TestKonfigSixelStatus(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	screen, u := runKonfigSixel(t, um, u, "wq", false)
	if row := screen.Row(konfigEditRow); !strings.Contains(row, "Sixel Graphics is now Off") {
		t.Errorf("status row = %q", row)
	}
	u.PreferredSixel = user.SixelOn
	screen, _ = runKonfigSixel(t, um, u, "wq", false)
	if row := screen.Row(konfigEditRow); !strings.Contains(row, "Sixel Graphics is now Auto") {
		t.Errorf("status row = %q", row)
	}
}

// Hot Keys still works from its new place in the right column.
func TestKonfigHotKeysInRightColumn(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	screen, got := runKonfigSixel(t, um, u, "dq", false)
	if !got.HotKeys {
		t.Fatal("Hot Keys not switched")
	}
	if got := konfigFieldText(screen, "[D] Hot Keys"); got == "" {
		t.Errorf("Hot Keys not on the form:\n%s", screen.Snapshot())
	}
}
