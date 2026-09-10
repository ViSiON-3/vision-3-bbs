package menu

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// TestSysopNoticeQueueRoundTrip covers enqueue → drain: notices accumulate per
// user, drain returns them in order and clears the queue, and a second drain is
// empty. A missing file drains empty rather than erroring.
func TestSysopNoticeQueueRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sysop_notices.json")

	// Draining a never-written store is empty, not an error.
	if got, err := drainSysopNotices(path, 1); err != nil || len(got) != 0 {
		t.Fatalf("drain of empty store = (%v, %v), want (nil, no error)", got, err)
	}

	for _, text := range []string{"first", "second"} {
		if err := enqueueSysopNotice(path, 1, sysopNotice{Text: text}); err != nil {
			t.Fatalf("enqueue %q: %v", text, err)
		}
	}
	// A different user's queue is independent.
	if err := enqueueSysopNotice(path, 2, sysopNotice{Text: "other"}); err != nil {
		t.Fatalf("enqueue for user 2: %v", err)
	}

	got, err := drainSysopNotices(path, 1)
	if err != nil {
		t.Fatalf("drain user 1: %v", err)
	}
	if len(got) != 2 || got[0].Text != "first" || got[1].Text != "second" {
		t.Fatalf("drain user 1 = %v, want [first second] in order", got)
	}

	// Draining again yields nothing — each notice is shown once.
	if again, _ := drainSysopNotices(path, 1); len(again) != 0 {
		t.Errorf("second drain returned %v, want empty", again)
	}

	// User 2's queue is untouched by user 1's drain.
	if other, _ := drainSysopNotices(path, 2); len(other) != 1 || other[0].Text != "other" {
		t.Errorf("user 2 drain = %v, want [other]", other)
	}
}

// TestHumanizeAge covers the unit boundaries and the plural/singular split. The
// notice string reads "signed up %s ago", so the helper returns the age alone.
func TestHumanizeAge(t *testing.T) {
	cases := []struct {
		age  time.Duration
		want string
	}{
		{-5 * time.Minute, "less than a minute"}, // clock moved backwards
		{0, "less than a minute"},
		{59 * time.Second, "less than a minute"},
		{time.Minute, "1 minute"},
		{90 * time.Second, "1 minute"},
		{59 * time.Minute, "59 minutes"},
		{time.Hour, "1 hour"},
		{23 * time.Hour, "23 hours"},
		{24 * time.Hour, "1 day"},
		{6 * 24 * time.Hour, "6 days"},
		{7 * 24 * time.Hour, "1 week"},
		{30 * 24 * time.Hour, "4 weeks"},
	}
	for _, c := range cases {
		if got := humanizeAge(c.age); got != c.want {
			t.Errorf("humanizeAge(%v) = %q, want %q", c.age, got, c.want)
		}
	}
}

// TestRenderSysopNotice covers the three display paths: a new-user notice
// rendered against the clock it is read on, an entry queued by a build that
// predates the structured fields, and a sysop who has blanked the string.
func TestRenderSysopNotice(t *testing.T) {
	e := &MenuExecutor{}
	e.SetStrings(config.StringsConfig{NewUserSysopNotice: "New user: %s signed up %s ago from node %d."})
	queued := time.Date(2026, 9, 10, 4, 55, 0, 0, time.UTC)
	now := queued.Add(9 * time.Hour)

	t.Run("new user notice states the age at display time", func(t *testing.T) {
		n := sysopNotice{
			Text:      "New user: Ne1 just signed up from node 1.",
			Handle:    "Ne1",
			Node:      1,
			CreatedAt: queued,
		}
		want := "New user: Ne1 signed up 9 hours ago from node 1."
		if got := e.renderSysopNotice(n, now); got != want {
			t.Errorf("renderSysopNotice() = %q, want %q", got, want)
		}
	})

	t.Run("notice queued before the structured fields falls back to its text", func(t *testing.T) {
		n := sysopNotice{Text: "New user: Darth Vader just signed up from node 1.", CreatedAt: queued}
		if got := e.renderSysopNotice(n, now); got != n.Text {
			t.Errorf("renderSysopNotice() = %q, want the stored text %q", got, n.Text)
		}
	})

	t.Run("blank string falls back rather than printing an empty line", func(t *testing.T) {
		blank := &MenuExecutor{}
		n := sysopNotice{Text: "New user: Ne1 just signed up from node 1.", Handle: "Ne1", Node: 1, CreatedAt: queued}
		if got := blank.renderSysopNotice(n, now); got != n.Text {
			t.Errorf("renderSysopNotice() = %q, want the stored text %q", got, n.Text)
		}
	})
}
