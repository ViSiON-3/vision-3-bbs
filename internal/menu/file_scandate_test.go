package menu

import (
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TestFileNewscanCutoff covers the file newscan boundary: the SETFILESCANDATE
// override when set, otherwise "since previous logon".
func TestFileNewscanCutoff(t *testing.T) {
	prev := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	t.Run("default is previous login", func(t *testing.T) {
		u := &user.User{PreviousLogin: prev}
		if got := fileNewscanCutoff(u); !got.Equal(prev) {
			t.Fatalf("cutoff = %v, want previous login %v", got, prev)
		}
	})

	t.Run("override wins when set", func(t *testing.T) {
		override := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
		u := &user.User{PreviousLogin: prev, FileNewscanSince: &override}
		if got := fileNewscanCutoff(u); !got.Equal(override) {
			t.Fatalf("cutoff = %v, want override %v", got, override)
		}
	})

	t.Run("zero-time override means all files", func(t *testing.T) {
		zero := time.Time{}
		u := &user.User{PreviousLogin: prev, FileNewscanSince: &zero}
		if got := fileNewscanCutoff(u); !got.IsZero() {
			t.Fatalf("cutoff = %v, want zero (all files)", got)
		}
	})
}

// TestSetFileScanDatePersists pins SETFILESCANDATE: a date sets the cutoff,
// A means all files (zero time), R resets to the default, and each choice
// is saved to the user record.
func TestSetFileScanDatePersists(t *testing.T) {
	env := newMenuEnv(t)

	r := env.runCmd("SETFILESCANDATE", env.caller, "", "03/15/26\r")
	if !r.has("File newscan set to files since 03/15/2026.") {
		t.Errorf("date confirm missing:\n%s", r.text())
	}
	saved := env.mustDiskUser(env.caller.ID)
	if saved.FileNewscanSince == nil || saved.FileNewscanSince.Format("2006-01-02") != "2026-03-15" {
		t.Fatalf("saved cutoff = %v, want 2026-03-15", saved.FileNewscanSince)
	}

	env.runCmd("SETFILESCANDATE", env.caller, "", "a\r")
	if saved := env.mustDiskUser(env.caller.ID); saved.FileNewscanSince == nil || !saved.FileNewscanSince.IsZero() {
		t.Errorf("A: saved cutoff = %v, want zero", saved.FileNewscanSince)
	}
	env.runCmd("SETFILESCANDATE", env.caller, "", "R\r")
	if saved := env.mustDiskUser(env.caller.ID); saved.FileNewscanSince != nil {
		t.Errorf("R: saved cutoff = %v, want nil", saved.FileNewscanSince)
	}
}

// TestSetFileScanDateRejectsAndCancels pins that an invalid date, a blank
// line and Esc all leave the saved cutoff alone, a disconnect logs off, and
// no user is told to log in.
func TestSetFileScanDateRejectsAndCancels(t *testing.T) {
	env := newMenuEnv(t)
	keep := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	env.caller.FileNewscanSince = &keep
	if err := env.um.UpdateUser(env.caller); err != nil {
		t.Fatal(err)
	}

	if r := env.runCmd("SETFILESCANDATE", env.caller, "", "13/45/26\r"); !r.has("Invalid") {
		t.Errorf("bad date not refused:\n%s", r.text())
	}
	env.runCmd("SETFILESCANDATE", env.caller, "", "\r")
	env.runCmd("SETFILESCANDATE", env.caller, "", "\x1b")
	if saved := env.mustDiskUser(env.caller.ID); saved.FileNewscanSince == nil || !saved.FileNewscanSince.Equal(keep) {
		t.Errorf("saved cutoff = %v, want unchanged %v", saved.FileNewscanSince, keep)
	}
	if r := env.runCmd("SETFILESCANDATE", env.caller, "", ""); r.next != "LOGOFF" {
		t.Errorf("disconnect: next = %q, want LOGOFF", r.next)
	}
	if r := env.runCmd("SETFILESCANDATE", nil, "", ""); r.user != nil || r.raw == "" {
		t.Errorf("no user: want a login notice, got user %v", r.user)
	}
}
