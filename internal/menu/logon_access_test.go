package menu

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwkapi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Login is decided by level alone. Anything telling a user whether they can
// get on must agree with executor_login_auth.go, which denies a caller whose
// level is below logonLevel and never reads the validated flag.
func TestCanLogonAtLevel(t *testing.T) {
	cases := []struct {
		name       string
		logonLevel int
		access     int
		want       bool
	}{
		{"shipped defaults: new user meets the threshold", 10, 10, true},
		{"above the threshold", 10, 25, true},
		{"below the threshold", 10, 1, false},
		{"one short", 10, 9, false},
		{"threshold disabled with zero", 0, 0, true},
		{"threshold disabled, low level still passes", 0, 1, true},
		{"negative threshold is treated as disabled", -1, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := config.ServerConfig{LogonLevel: c.logonLevel}
			if got := canLogonAtLevel(cfg, c.access); got != c.want {
				t.Errorf("canLogonAtLevel(logonLevel=%d, access=%d) = %v, want %v",
					c.logonLevel, c.access, got, c.want)
			}
		})
	}
}

// The validated flag must not influence the answer, since login does not read
// it. This pins the distinction the whole issue is about.
func TestCanLogonIsIndependentOfValidation(t *testing.T) {
	cfg := config.ServerConfig{LogonLevel: 10}

	// An unvalidated account at the shipped new-user level can log on.
	if !canLogonAtLevel(cfg, 10) {
		t.Error("an account at logonLevel should be able to log on regardless of validation")
	}
	// A validated account below the threshold still cannot.
	if canLogonAtLevel(cfg, 5) {
		t.Error("an account below logonLevel cannot log on, validated or not")
	}
}

// The API login must refuse what the terminal login refuses once the password
// verifies: an owed intro, then a level below logonLevel (which is also where a
// banned account, at level 0, ends up).
func TestQWKAPILoginDenial(t *testing.T) {
	cases := []struct {
		name         string
		logonLevel   int
		access       int
		introPending bool
		want         string
	}{
		{"ordinary user", 10, 25, false, ""},
		{"below logon level", 10, 5, false, qwkapi.DenyLogonLevel},
		{"banned (level 0)", 10, 0, false, qwkapi.DenyLogonLevel},
		{"intro owed, even above logon level", 10, 25, true, qwkapi.DenyIntroPending},
		{"logon level disabled", 0, 0, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := &MenuExecutor{}
			e.SetServerConfig(config.ServerConfig{LogonLevel: c.logonLevel})
			u := &user.User{Handle: "caller", AccessLevel: c.access, IntroPending: c.introPending}
			if got := e.QWKAPILoginDenial(u); got != c.want {
				t.Errorf("QWKAPILoginDenial = %q, want %q", got, c.want)
			}
		})
	}
}
