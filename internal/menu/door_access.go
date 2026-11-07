package menu

import (
	"log/slog"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// doorAllowed reports whether u may use door d: list it, see its details or
// launch it. Both of the door's own rules must pass: min_access_level, and
// its acs string, evaluated like any other ACS against the caller's session
// and call start time. An empty or all-whitespace acs adds no restriction and
// never lifts the numeric minimum, and there is no sysop bypass: a sysop the
// rules exclude is excluded. An acs with any problem denies the door to
// everyone, as any ACS string does. No user (not logged in) is never allowed.
//
// Every door entry point checks this before any side effect (dropfile,
// process, script or remote connection), and checks it again at launch even
// when the door was listed moments before, since a flag, the time of day or
// the configuration may have changed in between. A door category's rules are
// not part of it: they filter the generated door menu, not launches.
func doorAllowed(d config.DoorConfig, u *user.User, s ssh.Session, terminal *term.Terminal, startTime time.Time) bool {
	if u == nil {
		return false
	}
	return doorRulesPass(d, u.AccessLevel, func(rule string) bool {
		return checkACS(rule, u, s, terminal, startTime)
	})
}

// doorRulesPass is doorAllowed for a caller at level whose ACS strings allows
// evaluates. It lets the generated door menu apply the same rule with an
// evaluator that remembers its answers, rather than parse every door's ACS
// on every redraw.
func doorRulesPass(d config.DoorConfig, level int, allows func(string) bool) bool {
	if d.MinAccessLevel > 0 && level < d.MinAccessLevel {
		return false
	}
	rule := strings.TrimSpace(d.ACS)
	return rule == "" || allows(rule)
}

// logDoorDenied records a refused door at WARN, with the rule that refused it.
func logDoorDenied(msg string, node int, u *user.User, code string, d config.DoorConfig) {
	slog.Warn(msg, "node", node, "handle", u.Handle, "level", u.AccessLevel,
		"door", code, "required", d.MinAccessLevel, "acs", d.ACS)
}
