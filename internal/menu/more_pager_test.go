package menu

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// pagerRun writes n numbered lines through a pager for u on a 10-row screen
// with 2 rows reserved (8 lines a screen), feeding input as keystrokes, and
// returns the output and how many lines were written.
func pagerRun(t *testing.T, u *user.User, n int, input string) (string, int) {
	t.Helper()
	env := newMenuEnv(t)
	written := 0
	r := env.run(func(c *cmdCtx, _ string) (*user.User, string, error) {
		p := c.e.newMorePager(c.s, c.terminal, c.outputMode, u, 10, 2, "<MORE>")
		for i := 1; i <= n; i++ {
			c.e.writeString(c.terminal, c.outputMode, fmt.Sprintf("line %02d\r\n", i))
			written++
			if !p.Line() {
				break
			}
		}
		return nil, "", nil
	}, u, "", input)
	return r.text(), written
}

func boolp(b bool) *bool { return &b }

// TestMorePager pins the answers to the More prompt: Y, Enter and Space a
// screen more, C the rest without stopping, N and Q stop.
func TestMorePager(t *testing.T) {
	on := &user.User{ID: 7, Handle: "Pat"}
	for _, tc := range []struct {
		name, input string
		written     int
		prompts     int
	}{
		{"Y each time", "yyy", 20, 2},
		{"Enter and Space", "\r ", 20, 2},
		{"C runs on", "c", 20, 1},
		{"N stops", "n", 8, 1},
		{"Q stops", "q", 8, 1},
		{"other keys wait", "xzy\r", 20, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, written := pagerRun(t, on, 20, tc.input)
			if written != tc.written {
				t.Errorf("wrote %d lines, want %d:\n%s", written, tc.written, out)
			}
			if got := strings.Count(out, "<MORE>"); got != tc.prompts {
				t.Errorf("%d prompts, want %d:\n%s", got, tc.prompts, out)
			}
		})
	}
}

// TestMorePagerOff checks that a caller who turned More prompts off is never
// asked, and that an unset setting (every record before this change) is on.
func TestMorePagerOff(t *testing.T) {
	out, written := pagerRun(t, &user.User{ID: 7, Handle: "Pat", MorePrompts: boolp(false)}, 20, "")
	if written != 20 || strings.Contains(out, "<MORE>") {
		t.Errorf("More prompts off: wrote %d, prompted %v", written, strings.Contains(out, "<MORE>"))
	}
	if !(&user.User{}).MorePromptsOn() || !(&user.User{MorePrompts: boolp(true)}).MorePromptsOn() {
		t.Error("unset or true should be on")
	}
}

// TestUserListPages checks that a user list longer than the screen stops at
// the More prompt, and that N ends it there.
func TestUserListPages(t *testing.T) {
	env := newMenuEnv(t)
	for i := 0; i < 40; i++ {
		if _, err := env.um.AddUser("password1", fmt.Sprintf("Caller%02d", i), fmt.Sprintf("Caller %02d", i), ""); err != nil {
			t.Fatal(err)
		}
	}
	r := env.runCmd("LISTUSERS", env.caller, "", "n")
	text := r.text()
	if !strings.Contains(text, "More?") {
		t.Fatalf("no More prompt in a 40-user list:\n%s", text)
	}
	if shown := len(regexp.MustCompile(`Caller[0-9]{2}`).FindAllString(text, -1)); shown >= 40 {
		t.Errorf("N didn't stop the list: %d of 40 shown:\n%s", shown, text)
	}
	// By user number: the first screen starts with the earliest accounts.
	if i, j := strings.Index(text, "Caller00"), strings.Index(text, "Caller01"); i < 0 || j < 0 || i > j {
		t.Errorf("list not in user-number order:\n%s", text)
	}
}
