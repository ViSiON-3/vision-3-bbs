package menu

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestAllocNewsIDNeverReuses(t *testing.T) {
	nd := &NewsData{Items: []NewsItem{{ID: 3}, {ID: 1}}}
	if got := allocNewsID(nd); got != 4 {
		t.Fatalf("allocNewsID = %d, want 4 (must not reuse a live ID)", got)
	}

	// The regression this exists for: deleting the highest-numbered item must
	// not free its ID, or a user holding it in their seen-set never sees the
	// replacement.
	nd.Items = []NewsItem{{ID: 1}} // sysop deletes 3 (and the just-allocated 4)
	if got := allocNewsID(nd); got == 3 || got == 4 {
		t.Fatalf("allocNewsID reused a retired ID: %d", got)
	}

	empty := &NewsData{}
	if got := allocNewsID(empty); got != 1 {
		t.Fatalf("first ID on an empty file = %d, want 1", got)
	}
	if got := allocNewsID(empty); got != 2 {
		t.Fatalf("second ID = %d, want 2", got)
	}
}

// A news.json written before NextID existed has no allocator value; the first
// allocation must still clear every live ID.
func TestAllocNewsIDLegacyFileWithoutNextID(t *testing.T) {
	nd := &NewsData{Items: []NewsItem{{ID: 7}, {ID: 2}}} // NextID zero
	if got := allocNewsID(nd); got != 8 {
		t.Fatalf("allocNewsID on a legacy file = %d, want 8", got)
	}
}

func TestNormalizeNewsIDs(t *testing.T) {
	nd := &NewsData{Items: []NewsItem{{ID: 2}, {ID: 2}, {ID: 0}, {ID: 5}}}
	if !normalizeNewsIDs(nd) {
		t.Fatal("expected normalizeNewsIDs to report a change")
	}
	seen := map[int]bool{}
	for _, it := range nd.Items {
		if it.ID <= 0 {
			t.Fatalf("item left with non-positive ID: %+v", it)
		}
		if seen[it.ID] {
			t.Fatalf("duplicate ID %d after normalization", it.ID)
		}
		seen[it.ID] = true
	}
	// The first holder of an ID keeps it so existing seen-sets stay valid.
	if nd.Items[0].ID != 2 || nd.Items[3].ID != 5 {
		t.Fatalf("normalization moved IDs it should have kept: %+v", nd.Items)
	}
	// Repaired items must not be given a low free number like 1 or 3: those
	// gaps usually mean a deleted item whose ID users may still be holding.
	for _, i := range []int{1, 2} {
		if nd.Items[i].ID <= 5 {
			t.Errorf("repaired item %d got recycled ID %d; expected a fresh one above 5", i, nd.Items[i].ID)
		}
	}
	if normalizeNewsIDs(nd) {
		t.Fatal("normalizeNewsIDs is not idempotent")
	}
}

func TestNewsSeenSetPrunesDeletedItems(t *testing.T) {
	u := &user.User{SeenNewsIDs: []int{1, 2, 9}}
	seen := newsSeenSet(u, []NewsItem{{ID: 1}, {ID: 2}})
	if !seen[1] || !seen[2] {
		t.Fatal("live IDs dropped from seen set")
	}
	if seen[9] {
		t.Fatal("ID 9 has no live item and should have been pruned")
	}
}

func TestStoreNewsSeenReportsChange(t *testing.T) {
	u := &user.User{}
	if !storeNewsSeen(u, map[int]bool{2: true, 1: true}) {
		t.Fatal("expected first store to report a change")
	}
	if got := u.SeenNewsIDs; len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("SeenNewsIDs = %v, want sorted [1 2]", got)
	}
	if storeNewsSeen(u, map[int]bool{1: true, 2: true}) {
		t.Fatal("storing an identical set should report no change")
	}
	if !storeNewsSeen(u, map[int]bool{}) {
		t.Fatal("clearing the set should report a change")
	}
	if u.SeenNewsIDs != nil {
		t.Fatalf("empty set should store as nil, got %v", u.SeenNewsIDs)
	}
}

func TestInitNewsSeenBackfillsOnlyOlderItems(t *testing.T) {
	prev := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	items := []NewsItem{
		{ID: 1, When: prev.Add(-24 * time.Hour)}, // before last visit -> seen
		{ID: 2, When: prev.Add(24 * time.Hour)},  // after last visit  -> unseen
		{ID: 3, When: prev.Add(-24 * time.Hour), Always: true},
	}

	u := &user.User{PreviousLogin: prev}
	seen := map[int]bool{}
	initNewsSeen(u, items, seen)

	if !u.NewsSeenInitialized {
		t.Fatal("NewsSeenInitialized not set")
	}
	if !seen[1] {
		t.Error("item posted before the previous visit should be back-filled as seen")
	}
	if seen[2] {
		t.Error("item posted after the previous visit must stay unseen")
	}
	if seen[3] {
		t.Error("always-items are never recorded as seen")
	}
}

func TestInitNewsSeenNewUserSeesEverything(t *testing.T) {
	// A brand-new account has a zero PreviousLogin: nothing is back-filled,
	// so the full news list is waiting on the first call.
	u := &user.User{}
	seen := map[int]bool{}
	initNewsSeen(u, []NewsItem{{ID: 1, When: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}}, seen)
	if seen[1] {
		t.Error("first-time caller should not have news marked as already seen")
	}
}

// newsWarningFor writes newsItems to a temp system and returns whatever
// WarnIfNewsUnwired logged for the given login sequence.
func newsWarningFor(t *testing.T, newsItems []NewsItem, seq []config.LoginItem) string {
	t.Helper()

	root := t.TempDir()
	configPath := filepath.Join(root, "configs")
	dataPath := filepath.Join(root, "data")
	for _, d := range []string{configPath, dataPath} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	if newsItems != nil {
		body, err := json.Marshal(NewsData{Items: newsItems})
		if err != nil {
			t.Fatalf("marshal news: %v", err)
		}
		// newsFilePath resolves to <configPath>/../data/news.json
		if err := os.WriteFile(filepath.Join(dataPath, "news.json"), body, 0o644); err != nil {
			t.Fatalf("write news.json: %v", err)
		}
	}

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(prev)

	WarnIfNewsUnwired(configPath, seq)
	return buf.String()
}

func TestWarnIfNewsUnwiredFiresWhenNewsIsStranded(t *testing.T) {
	got := newsWarningFor(t,
		[]NewsItem{{ID: 1, Title: "Welcome"}},
		[]config.LoginItem{{Command: "LASTCALLS"}, {Command: "USERSTATS"}})

	if !strings.Contains(got, "never be displayed at login") {
		t.Errorf("expected a warning about stranded news, got %q", got)
	}
	if !strings.Contains(got, "PRINTNEWS") {
		t.Errorf("warning should name the fix, got %q", got)
	}
}

func TestWarnIfNewsUnwiredSilentWhenWired(t *testing.T) {
	// Command matching is case-insensitive: LoadLoginSequence upper-cases, but
	// a hand-edited file read by another path may not have been normalized.
	for _, cmd := range []string{"PRINTNEWS", "printnews"} {
		got := newsWarningFor(t,
			[]NewsItem{{ID: 1, Title: "Welcome"}},
			[]config.LoginItem{{Command: "LASTCALLS"}, {Command: cmd}})
		if got != "" {
			t.Errorf("command %q: expected silence, got %q", cmd, got)
		}
	}
}

func TestWarnIfNewsUnwiredSilentWhenNoNews(t *testing.T) {
	// Nothing is being missed on a system with no news, wired or not.
	if got := newsWarningFor(t, nil, []config.LoginItem{{Command: "LASTCALLS"}}); got != "" {
		t.Errorf("no news.json: expected silence, got %q", got)
	}
	if got := newsWarningFor(t, []NewsItem{}, []config.LoginItem{{Command: "LASTCALLS"}}); got != "" {
		t.Errorf("empty news.json: expected silence, got %q", got)
	}
}
