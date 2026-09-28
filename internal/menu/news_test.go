package menu

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// seedNews writes nd as the env's news.json.
func seedNews(t *testing.T, env *menuEnv, nd *NewsData) {
	t.Helper()
	if err := saveNewsData(env.cfgDir(), nd); err != nil {
		t.Fatalf("seed news: %v", err)
	}
}

// loadEnvNews reloads news.json the way the handlers do.
func loadEnvNews(t *testing.T, env *menuEnv) *NewsData {
	t.Helper()
	nd, err := loadNewsData(env.cfgDir())
	if err != nil {
		t.Fatalf("reload news: %v", err)
	}
	return nd
}

// newsReloadUser reads user id back from users.json with a fresh manager, so
// the check sees what was persisted rather than the in-memory pointer.
func newsReloadUser(t *testing.T, env *menuEnv, id int) *user.User {
	t.Helper()
	um, err := user.NewUserManager(env.dataDir())
	if err != nil {
		t.Fatalf("reload users: %v", err)
	}
	u, ok := um.GetUserByID(id)
	if !ok {
		t.Fatalf("user %d not found after reload", id)
	}
	return u
}

// newsFixture has a once-only item, an always item, one only level 100+ may
// read, and one capped at level 5.
func newsFixture(when time.Time) *NewsData {
	return &NewsData{NextID: 5, Items: []NewsItem{
		{ID: 1, Title: "Once Item", From: "Sysop", When: when, Level: 0, Body: "once body text"},
		{ID: 2, Title: "Always Item", From: "Sysop", When: when, Level: 0, Always: true, Body: "always body text"},
		{ID: 3, Title: "Staff Item", From: "Sysop", When: when, Level: 100, Body: "staff body text"},
		{ID: 4, Title: "Newbie Item", From: "Sysop", When: when, Level: 0, MaxLevel: 5, Body: "newbie body text"},
	}}
}

// PRINTNEWS shows unseen items and always-items the caller's level allows,
// records the once-only item as seen on disk, and on the next login shows
// only the always-item.
func TestPrintNewsShowsUnseenOnceAndAlwaysEveryTime(t *testing.T) {
	env := newMenuEnv(t)
	seedNews(t, env, newsFixture(time.Now()))
	env.caller.NewsSeenInitialized = true

	r := env.runCmd("PRINTNEWS", env.caller, "", "\r\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("Once Item", "once body text", "Always Item", "always body text") {
		t.Errorf("first login:\n%s", r.text())
	}
	if r.has("Staff Item") || r.has("Newbie Item") {
		t.Errorf("items outside the caller's level range shown:\n%s", r.text())
	}
	if got := newsReloadUser(t, env, 2).SeenNewsIDs; !slices.Equal(got, []int{1}) {
		t.Errorf("persisted SeenNewsIDs = %v, want [1]", got)
	}

	r = env.runCmd("PRINTNEWS", env.caller, "", "\r\r")
	if r.has("Once Item") || !r.has("Always Item") {
		t.Errorf("second login should show only the always-item:\n%s", r.text())
	}
}

// Q in the viewer skips the rest of the backlog; the item that was on screen
// still counts as seen, the skipped one does not.
func TestPrintNewsQuitSkipsRest(t *testing.T) {
	env := newMenuEnv(t)
	now := time.Now()
	seedNews(t, env, &NewsData{NextID: 3, Items: []NewsItem{
		{ID: 1, Title: "First", When: now, Body: "one"},
		{ID: 2, Title: "Second", When: now, Body: "two"},
	}})
	env.caller.NewsSeenInitialized = true

	r := env.runCmd("PRINTNEWS", env.caller, "", "q\r")
	if !r.has("First") || r.has("Second") {
		t.Errorf("q should stop after the first item:\n%s", r.text())
	}
	if got := newsReloadUser(t, env, 2).SeenNewsIDs; !slices.Equal(got, []int{1}) {
		t.Errorf("SeenNewsIDs = %v, want [1]", got)
	}
}

// A caller who predates seen-tracking has news from before their previous
// visit back-filled as read, so only newer items appear.
func TestPrintNewsBackfillsOlderItems(t *testing.T) {
	env := newMenuEnv(t)
	prev := time.Now().Add(-24 * time.Hour)
	seedNews(t, env, &NewsData{NextID: 3, Items: []NewsItem{
		{ID: 1, Title: "Old News", When: prev.Add(-time.Hour), Body: "old"},
		{ID: 2, Title: "Fresh News", When: prev.Add(time.Hour), Body: "fresh"},
	}})
	env.caller.PreviousLogin = prev

	r := env.runCmd("PRINTNEWS", env.caller, "", "\r\r")
	if r.has("Old News") || !r.has("Fresh News") {
		t.Errorf("backfill:\n%s", r.text())
	}
	got := newsReloadUser(t, env, 2)
	if !got.NewsSeenInitialized || !slices.Equal(got.SeenNewsIDs, []int{1, 2}) {
		t.Errorf("persisted init=%v seen=%v, want true [1 2]", got.NewsSeenInitialized, got.SeenNewsIDs)
	}
}

// A disconnect while an item is on screen still records it as seen.
func TestPrintNewsDisconnectStillMarksSeen(t *testing.T) {
	env := newMenuEnv(t)
	seedNews(t, env, newsFixture(time.Now()))
	env.caller.NewsSeenInitialized = true

	env.runCmd("PRINTNEWS", env.caller, "", "")
	if got := newsReloadUser(t, env, 2).SeenNewsIDs; !slices.Equal(got, []int{1}) {
		t.Errorf("SeenNewsIDs = %v, want [1]", got)
	}
}

// No user, no news file, or a corrupt one: PRINTNEWS stays silent and
// never fails the login.
func TestPrintNewsQuietCases(t *testing.T) {
	env := newMenuEnv(t)
	if r := env.runCmd("PRINTNEWS", nil, "", ""); r.raw != "" || r.err != nil {
		t.Errorf("logged out: %q %v", r.raw, r.err)
	}
	if r := env.runCmd("PRINTNEWS", env.caller, "", ""); r.err != nil || strings.TrimSpace(r.text()) != "" {
		t.Errorf("no news: %q %v", r.text(), r.err)
	}
	if err := os.WriteFile(newsFilePath(env.cfgDir()), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := env.runCmd("PRINTNEWS", env.caller, "", ""); r.err != nil || r.raw != "" {
		t.Errorf("corrupt news: %q %v", r.raw, r.err)
	}
	if r := env.runCmd("LISTNEWS", env.caller, "", ""); !r.has("Error loading news.") {
		t.Errorf("LISTNEWS on corrupt news: %q", r.text())
	}
}

// LISTNEWS lists the items in the caller's level range, tags unread ones
// [NEW], rejects bad picks, and marks an item seen once it has been read.
func TestListNewsReadMarksSeen(t *testing.T) {
	env := newMenuEnv(t)
	seedNews(t, env, newsFixture(time.Now()))
	env.caller.NewsSeenInitialized = true

	// Pick 9 (invalid), then read item 1, then leave.
	r := env.runCmd("LISTNEWS", env.caller, "", "9\r1\r\r\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("System News", "Once Item", "Always Item", "[NEW]", "Invalid selection.", "once body text") {
		t.Errorf("list flow:\n%s", r.text())
	}
	if r.has("Staff Item") || r.has("Newbie Item") {
		t.Errorf("items outside the caller's level range listed:\n%s", r.text())
	}
	// Only the once-item carries [NEW]; always-items never do.
	if n := strings.Count(strings.SplitN(r.text(), "Read which item", 2)[0], "[NEW]"); n != 1 {
		t.Errorf("[NEW] tags in first listing = %d, want 1", n)
	}
	if got := newsReloadUser(t, env, 2).SeenNewsIDs; !slices.Equal(got, []int{1}) {
		t.Errorf("SeenNewsIDs = %v, want [1]", got)
	}

	// Next time the item is no longer new.
	r = env.runCmd("LISTNEWS", env.caller, "", "\r")
	if r.has("[NEW]") {
		t.Errorf("read item still tagged new:\n%s", r.text())
	}
}

// With nothing in the caller's level range LISTNEWS says so.
func TestListNewsNothingVisible(t *testing.T) {
	env := newMenuEnv(t)
	if r := env.runCmd("LISTNEWS", env.caller, "", "\r"); !r.has("No news available.") {
		t.Errorf("no news: %q", r.text())
	}
	seedNews(t, env, &NewsData{Items: []NewsItem{{ID: 1, Title: "Staff", Level: 200, Body: "x"}}})
	if r := env.runCmd("LISTNEWS", env.caller, "", "\r"); !r.has("No news available.") || r.has("Staff") {
		t.Errorf("only-hidden news: %q", r.text())
	}
	if r := env.runCmd("LISTNEWS", nil, "", "\r"); r.raw != "" {
		t.Errorf("logged out: %q", r.raw)
	}
}

// A body longer than the screen is paged: the status line counts the rows
// in view and the scroll keys bring later lines on screen.
func TestPrintNewsScrollsLongItem(t *testing.T) {
	env := newMenuEnv(t)
	var lines []string
	for i := 1; i <= 40; i++ {
		lines = append(lines, fmt.Sprintf("body row %02d", i))
	}
	seedNews(t, env, &NewsData{NextID: 2, Items: []NewsItem{{ID: 1, Title: "Long One", When: time.Now(), Body: strings.Join(lines, "\n")}}})
	env.caller.NewsSeenInitialized = true

	// down, page down, end, home, up (clamped), ^X, then continue.
	r := env.runCmd("PRINTNEWS", env.caller, "", "\x1b[B\x1b[6~\x1b[F\x1b[H\x1b[A\x18\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("of 40]", "body row 01", "body row 40") {
		t.Errorf("scrolling viewer:\n%s", r.text())
	}
	if !strings.Contains(r.text(), "[1-") || !strings.Contains(r.text(), "-40 of 40]") {
		t.Errorf("status line never showed the top and bottom of the item:\n%s", r.text())
	}
}
