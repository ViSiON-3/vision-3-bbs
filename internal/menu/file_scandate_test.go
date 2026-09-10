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
