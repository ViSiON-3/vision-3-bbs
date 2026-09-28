package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// adminTruncate trims surrounding whitespace before measuring, then appends an
// ASCII "..." ellipsis when it cuts (hard-cutting instead when max is 3 or
// less). These cases pin that behaviour. The expectations changed once, when
// the ellipsis moved from U+2026 to ASCII because CP437 cannot render U+2026;
// they should not change again without an equally deliberate reason.
func TestAdminTruncate(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{"empty input", "", 10, ""},
		{"whitespace only collapses to empty", "   ", 10, ""},
		{"max 0", "hello", 0, ""},
		{"shorter than max", "abc", 10, "abc"},
		{"exactly max", "Hello", 5, "Hello"},
		{"longer gets ellipsis", "Hello World", 8, "Hello..."},
		// The ellipsis is 3 runes, so anything up to and including 3 has no room
		// for it and hard-cuts instead. This boundary moved when the ellipsis
		// changed from the 1-rune U+2026 to ASCII "...".
		{"maxLen exactly 1 hard-cuts", "Hello", 1, "H"},
		{"maxLen 2 hard-cuts", "Hello", 2, "He"},
		{"maxLen 3 hard-cuts", "Hello", 3, "Hel"},
		{"maxLen 4 has room for the ellipsis", "Hello", 4, "H..."},
		{"maxLen 0 with non-empty trimmed value", "Hello", 0, ""},
		{"leading and trailing whitespace trimmed first", "  Hello  ", 10, "Hello"},
		{"trimmed then truncated", "  Hello World  ", 8, "Hello..."},
		{"multi-byte fits by runes", "héllo", 5, "héllo"},
		{"multi-byte truncation on rune boundary", "héllo wörld", 8, "héllo..."},
		{"cjk truncation on rune boundary", "日本語のメッセージ", 5, "日本..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := adminTruncate(tt.s, tt.max); got != tt.want {
				t.Errorf("adminTruncate(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
			}
		})
	}
}

// TestListUsers_ShowsSeededUsers pins the public user list: every live user
// appears with level and location, unvalidated users carry [NV], deleted
// users are hidden, and the header counts users pending validation (banned
// and deleted accounts are not pending).
func TestListUsers_ShowsSeededUsers(t *testing.T) {
	env := newMenuEnv(t)
	gone := &user.User{ID: 4, Handle: "Vanished", AccessLevel: 10, DeletedUser: true}
	env.seedUsers(
		&user.User{ID: 3, Handle: "Pending", GroupLocation: "Pittsburgh", AccessLevel: 5},
		gone,
		&user.User{ID: 5, Handle: "Exiled", AccessLevel: 0},
	)
	r := env.runCmd("LISTUSERS", env.caller, "", "\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("Sysop", "Caller", "Pending [NV]", "Pittsburgh", "255", "Pending validation: 1") {
		t.Errorf("output:\n%s", r.text())
	}
	if r.has("Vanished") {
		t.Errorf("deleted user listed:\n%s", r.text())
	}
}

// TestListUsers_DisconnectAtPauseLogsOff pins that losing the caller at the
// closing pause is reported as a logoff.
func TestListUsers_DisconnectAtPauseLogsOff(t *testing.T) {
	env := newMenuEnv(t)
	if r := env.runCmd("LISTUSERS", env.caller, "", ""); r.next != "LOGOFF" {
		t.Errorf("next = %q, want LOGOFF", r.next)
	}
}

// TestPendingValidationNotice pins that the login notice shows the pending
// count to sysops only, and stays silent when nothing is pending.
func TestPendingValidationNotice(t *testing.T) {
	env := newMenuEnv(t)
	if r := env.runCmd("PENDINGVALIDATIONNOTICE", env.sysop, "", ""); strings.TrimSpace(r.text()) != "" {
		t.Errorf("empty queue output: %q", r.text())
	}
	env.seedUsers(pendingNewbie(3, "A"), pendingNewbie(4, "B"), &user.User{ID: 5, Handle: "Banned"})
	r := env.runCmd("PENDINGVALIDATIONNOTICE", env.sysop, "", "")
	if !r.has("Validate user account [2]") {
		t.Errorf("sysop output: %q", r.text())
	}
	for _, u := range []*user.User{env.caller, nil} {
		if r := env.runCmd("PENDINGVALIDATIONNOTICE", u, "", ""); strings.TrimSpace(r.text()) != "" {
			t.Errorf("non-sysop %v saw notice: %q", u, r.text())
		}
	}
}

// TestToggleAllowNewUsers_PersistsFlag pins that the toggle flips the live
// flag, writes it to config.json, and reports the new state. (The handler
// always sleeps one second after reporting.)
func TestToggleAllowNewUsers_PersistsFlag(t *testing.T) {
	env := newMenuEnv(t)
	if !env.e.GetServerConfig().AllowNewUsers {
		t.Fatal("shipped config should allow new users")
	}
	r := env.runCmd("TOGGLEALLOWNEWUSERS", env.sysop, "", "")
	if r.err != nil || !r.has("New user registrations: CLOSED") {
		t.Errorf("err = %v output:\n%s", r.err, r.text())
	}
	if env.e.GetServerConfig().AllowNewUsers {
		t.Error("live config still allows new users")
	}
	disk, err := config.LoadServerConfig(env.cfgDir())
	if err != nil {
		t.Fatal(err)
	}
	if disk.AllowNewUsers {
		t.Error("config.json still allows new users")
	}
	if r := env.runCmd("TOGGLEALLOWNEWUSERS", nil, "", ""); r.raw != "" {
		t.Errorf("logged-out toggle wrote output: %q", r.text())
	}
}
