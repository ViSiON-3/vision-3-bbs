package menu

import (
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestNextNewsIDSurvivesDeletion(t *testing.T) {
	items := []NewsItem{{ID: 3}, {ID: 1}}
	if got := nextNewsID(items); got != 4 {
		t.Fatalf("nextNewsID = %d, want 4 (must not reuse a live ID)", got)
	}
	if got := nextNewsID(nil); got != 1 {
		t.Fatalf("nextNewsID(nil) = %d, want 1", got)
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
