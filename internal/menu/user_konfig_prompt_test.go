package menu

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// konfigWithACS is konfigTestExecutor on a board whose customPromptACS is acs.
func konfigWithACS(t *testing.T, acs string) *MenuExecutor {
	t.Helper()
	e := konfigTestExecutor(t)
	e.SetServerConfig(config.ServerConfig{CustomPromptACS: acs})
	return e
}

// konfigCtx is a USERCONFIG context on an 80x24 screen fed keys.
func konfigCtx(t *testing.T, e *MenuExecutor, um *user.UserMgr, u *user.User, keys string) (*cmdCtx, *testterm.Term) {
	t.Helper()
	screen := testterm.New(80, 24)
	sess := testterm.NewSession(screen, keys)
	t.Cleanup(func() {
		resetSessionIH(sess)
		sessionTermSizes.Delete(sess)
	})
	return &cmdCtx{e: e, s: sess, terminal: term.NewTerminal(sess, ""), userManager: um, currentUser: u,
		nodeNumber: 1, outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24}, screen
}

func runKonfigOn(t *testing.T, e *MenuExecutor, um *user.UserMgr, u *user.User, keys string) *testterm.Term {
	t.Helper()
	c, screen := konfigCtx(t, e, um, u, keys)
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatalf("runUserKonfig: %v", err)
	}
	return screen
}

func TestKonfigMenuPromptEditsAndClears(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	e := konfigWithACS(t, "")
	runKonfigOn(t, e, um, u, "t"+keyClear+"  |11Hi |UH>  \rq")
	if got := reloadUser(t, um).CustomPrompt; got != "|11Hi |UH>" {
		t.Fatalf("CustomPrompt = %q", got)
	}
	screen := runKonfigOn(t, e, um, reloadUser(t, um), "t"+keyClear+"\rq")
	if got := reloadUser(t, um).CustomPrompt; got != "" {
		t.Fatalf("CustomPrompt = %q, want cleared", got)
	}
	if !strings.Contains(screen.Row(konfigEditRow), "cleared") {
		t.Errorf("status row = %q", screen.Row(konfigEditRow))
	}
}

func TestKonfigMenuPromptKeepsEighty(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	runKonfigOn(t, konfigWithACS(t, ""), um, u, "t"+keyClear+strings.Repeat("x", 90)+"\rq")
	if got := reloadUser(t, um).CustomPrompt; got != strings.Repeat("x", user.CustomPromptMaxLen) {
		t.Fatalf("CustomPrompt = %q (%d), want %d characters", got, len(got), user.CustomPromptMaxLen)
	}
}

// The preview shows the prompt as a menu would, on the help row; a blank one
// gives the help line back.
func TestKonfigMenuPromptPreview(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	c, screen := konfigCtx(t, konfigWithACS(t, ""), um, u, "")
	st := &konfigState{c: c, items: []*konfigItem{{help: "help line"}}}
	if err := st.previewCustomPrompt("|11Hi |UH> |CL"); err != nil {
		t.Fatal(err)
	}
	if row := screen.Row(konfigHelpRow); !strings.Contains(row, "Preview: Hi Tester> |CL") {
		t.Errorf("help row = %q", row)
	}
	u.GroupLocation = "Montr\u00e9al"
	if err := st.previewCustomPrompt("|GL>"); err != nil {
		t.Fatal(err)
	}
	if row := screen.Row(konfigHelpRow); !strings.Contains(row, "Preview: Montr\u00e9al>") {
		t.Errorf("help row = %q, want the location as typed", row)
	}
	if err := st.previewCustomPrompt("  "); err != nil {
		t.Fatal(err)
	}
	if row := screen.Row(konfigHelpRow); !strings.Contains(row, "help line") {
		t.Errorf("help row = %q, want the help line back", row)
	}
}

// A caller the board's ACS leaves out doesn't get the item.
func TestKonfigMenuPromptFollowsACS(t *testing.T) {
	label := konfigTestStrings(t).KonfigCustomPromptLabel
	_, probe := newUserConfigTestUser(t)
	lvl := probe.AccessLevel
	for acs, shown := range map[string]bool{
		"": true, "*": true, fmt.Sprintf("S%d", lvl): true,
		fmt.Sprintf("S%d", lvl+1): false, "S1&": false,
	} {
		um, u := newUserConfigTestUser(t)
		screen := runKonfigOn(t, konfigWithACS(t, acs), um, u, "t"+keyEsc+"q")
		var all strings.Builder
		for row := 1; row <= 24; row++ {
			all.WriteString(screen.Row(row))
		}
		if got := strings.Contains(all.String(), label); got != shown {
			t.Errorf("acs %q: item shown %v, want %v", acs, got, shown)
		}
		if !shown && reloadUser(t, um).CustomPrompt != "" {
			t.Errorf("acs %q: the hotkey still edited it", acs)
		}
	}
}
