package menu

import (
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// The regression this guards: LastLogin is stamped with now() at
// authentication, so any newscan comparing against it finds nothing. The
// cutoff must come from PreviousLogin.
func TestNewscanSinceUsesPreviousLoginNotLastLogin(t *testing.T) {
	prev := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	u := &user.User{
		LastLogin:     time.Now(), // this session, as Authenticate leaves it
		PreviousLogin: prev,
	}

	got := newscanSince(u)
	if !got.Equal(prev) {
		t.Fatalf("newscanSince = %v, want PreviousLogin %v", got, prev)
	}
	if got.Equal(u.LastLogin) {
		t.Error("newscanSince returned LastLogin; newscans would never find anything")
	}
}

func TestNewscanSinceFirstTimeCallerAndNilUser(t *testing.T) {
	// First call: LastLogin already stamped, but no previous visit.
	u := &user.User{LastLogin: time.Now()}
	if got := newscanSince(u); !got.IsZero() {
		t.Errorf("first-time caller cutoff = %v, want zero", got)
	}
	if got := newscanSince(nil); !got.IsZero() {
		t.Errorf("nil user cutoff = %v, want zero", got)
	}
}

func TestIsNewSince(t *testing.T) {
	cut := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name     string
		postedAt time.Time
		since    time.Time
		want     bool
	}{
		{"posted after the last visit is new", cut.Add(time.Hour), cut, true},
		{"posted before the last visit is not", cut.Add(-time.Hour), cut, false},
		{"posted exactly at the cutoff is not new", cut, cut, false},
		{"zero cutoff means everything is new", cut.Add(-9999 * time.Hour), time.Time{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isNewSince(c.postedAt, c.since); got != c.want {
				t.Errorf("isNewSince(%v, %v) = %v, want %v", c.postedAt, c.since, got, c.want)
			}
		})
	}
}

// End-to-end shape of the bug for a rumors/file newscan style filter: with a
// user as Authenticate leaves them, items posted since the previous visit must
// still be found.
func TestNewscanFiltersFindItemsPostedSincePreviousVisit(t *testing.T) {
	prev := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	u := &user.User{LastLogin: time.Now(), PreviousLogin: prev}
	since := newscanSince(u)

	posted := []time.Time{
		prev.Add(-48 * time.Hour), // old
		prev.Add(-time.Second),    // just before the visit
		prev.Add(time.Second),     // just after: new
		prev.Add(24 * time.Hour),  // new
	}

	found := 0
	for _, p := range posted {
		if isNewSince(p, since) {
			found++
		}
	}
	if found != 2 {
		t.Errorf("found %d new items, want 2 — the cutoff is wrong", found)
	}
}
