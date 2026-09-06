package scripting

import (
	"fmt"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/dop251/goja"
)

// evalWithUser exposes u to a script as `u` and returns the expression's value.
func evalWithUser(t *testing.T, u *user.User, expr string) goja.Value {
	t.Helper()
	vm := goja.New()
	if err := vm.Set("u", userToJS(vm, u)); err != nil {
		t.Fatalf("set user: %v", err)
	}
	v, err := vm.RunString(expr)
	if err != nil {
		t.Fatalf("eval %q: %v", expr, err)
	}
	return v
}

func TestUnixOrZeroMapsZeroTimeToZero(t *testing.T) {
	if got := unixOrZero(time.Time{}); got != 0 {
		t.Errorf("unixOrZero(zero) = %d, want 0 (not %d)", got, time.Time{}.Unix())
	}
	stamp := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	if got := unixOrZero(stamp); got != stamp.Unix() {
		t.Errorf("unixOrZero(%v) = %d, want %d", stamp, got, stamp.Unix())
	}
}

func TestPreviousLoginExposedToScripts(t *testing.T) {
	prev := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	u := &user.User{
		Handle:        "Felonius",
		LastLogin:     time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC), // this session
		PreviousLogin: prev,
	}

	if got := evalWithUser(t, u, "u.previousLogin").ToInteger(); got != prev.Unix() {
		t.Errorf("u.previousLogin = %d, want %d", got, prev.Unix())
	}
	// lastLogin is unchanged: scripts may already depend on it.
	if got := evalWithUser(t, u, "u.lastLogin").ToInteger(); got != u.LastLogin.Unix() {
		t.Errorf("u.lastLogin = %d, want %d", got, u.LastLogin.Unix())
	}
	// The distinction that makes the field worth having.
	if evalWithUser(t, u, "u.lastLogin === u.previousLogin").ToBoolean() {
		t.Error("lastLogin and previousLogin must not be the same value")
	}
}

// A first-time caller must not be handed -62135596800.
func TestFirstTimeCallerPreviousLoginIsZeroAndFalsy(t *testing.T) {
	u := &user.User{Handle: "Newbie", LastLogin: time.Now()} // PreviousLogin zero

	if got := evalWithUser(t, u, "u.previousLogin").ToInteger(); got != 0 {
		t.Errorf("u.previousLogin = %d, want 0 for a first-time caller", got)
	}
	// Falsy, so `if (user.previousLogin)` reads naturally in a script.
	if evalWithUser(t, u, "!!u.previousLogin").ToBoolean() {
		t.Error("previousLogin should be falsy when there is no previous visit")
	}
}

// The trap this field exists to fix: a script asking "what is new since this
// user was last here?" finds nothing when it compares against lastLogin,
// because that is stamped at authentication before any script runs.
func TestScriptNewSinceLastVisitWorksWithPreviousLogin(t *testing.T) {
	prev := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	u := &user.User{
		Handle:        "Felonius",
		LastLogin:     time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC),
		PreviousLogin: prev,
	}

	// Four items, two posted since the user was last on.
	script := `
		var posted = [
			%d, %d,   // before the previous visit
			%d, %d    // after it
		];
		function countNewerThan(cut) {
			return posted.filter(function (p) { return p > cut; }).length;
		}
		[countNewerThan(u.previousLogin), countNewerThan(u.lastLogin)];
	`
	expr := fmt.Sprintf(script,
		prev.Add(-48*time.Hour).Unix(), prev.Add(-time.Hour).Unix(),
		prev.Add(time.Hour).Unix(), prev.Add(24*time.Hour).Unix())

	res := evalWithUser(t, u, expr).Export().([]interface{})
	withPrev := toInt(res[0])
	withLast := toInt(res[1])

	if withPrev != 2 {
		t.Errorf("using previousLogin found %d new items, want 2", withPrev)
	}
	if withLast != 0 {
		t.Errorf("using lastLogin found %d new items, want 0 — that is the trap", withLast)
	}
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return -1
	}
}
