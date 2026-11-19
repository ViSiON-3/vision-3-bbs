package source

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func openTestStore(t *testing.T, dataDir string) *Store {
	t.Helper()
	st, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestStoreMarkers(t *testing.T) {
	st := openTestStore(t, t.TempDir())
	if _, seen, err := st.Marker("reddit", "bbs", "t3_a"); err != nil || seen {
		t.Fatalf("unseen item: seen=%v err=%v", seen, err)
	}
	if err := st.SetMarker("reddit", "bbs", "t3_a", "4"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetMarker("reddit", "bbs", "t3_a", "5"); err != nil {
		t.Fatal(err)
	}
	if m, seen, err := st.Marker("reddit", "bbs", "t3_a"); err != nil || !seen || m != "5" {
		t.Errorf("marker = %q seen=%v err=%v, want 5", m, seen, err)
	}
	if _, seen, _ := st.Marker("reddit", "other", "t3_a"); seen {
		t.Error("a marker leaked across feeds")
	}
}

func TestStoreRuns(t *testing.T) {
	st := openTestStore(t, t.TempDir())
	if _, _, ok, err := st.LastRun("reddit", "bbs"); err != nil || ok {
		t.Fatalf("no runs yet: ok=%v err=%v", ok, err)
	}
	at := time.Date(2026, 10, 22, 7, 0, 0, 0, time.UTC)
	_ = st.RecordRun(FeedResult{Source: "reddit", Feed: "bbs", AreaTag: "REDDIT_BBS", Items: 1}, at.Add(-time.Hour))
	if err := st.RecordRun(FeedResult{Source: "reddit", Feed: "bbs", AreaTag: "REDDIT_BBS", Items: 2, Replies: 3, Pages: 4, Err: errors.New("boom")}, at); err != nil {
		t.Fatal(err)
	}
	r, when, ok, err := st.LastRun("reddit", "bbs")
	if err != nil || !ok || !when.Equal(at) || r.Items != 2 || r.Replies != 3 || r.Pages != 4 || r.Err == nil || r.Err.Error() != "boom" {
		t.Errorf("last run = %+v at %v ok=%v err=%v", r, when, ok, err)
	}
}

// oldRedditSchema is internal/reddit's posts table.
const oldRedditSchema = `CREATE TABLE posts (subreddit TEXT NOT NULL, post_id TEXT NOT NULL, comment_count INTEGER NOT NULL, last_seen_at INTEGER NOT NULL, PRIMARY KEY (subreddit, post_id));`

func TestStoreMigratesRedditDB(t *testing.T) {
	data := t.TempDir()
	old := filepath.Join(data, "reddit", "reddit.db")
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", old)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{oldRedditSchema,
		`INSERT INTO posts VALUES ('bbs', 't3_abc', 7, 1790000000), ('bbs', 't3_def', 0, 1790000000)`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()

	st := openTestStore(t, data)
	if m, seen, err := st.Marker("reddit", "bbs", "t3_abc"); err != nil || !seen || m != "7" {
		t.Errorf("migrated marker = %q seen=%v err=%v, want 7", m, seen, err)
	}
	if err := st.SetMarker("reddit", "bbs", "t3_abc", "9"); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	st = openTestStore(t, data) // opening again must not overwrite newer markers
	if m, _, _ := st.Marker("reddit", "bbs", "t3_abc"); m != "9" {
		t.Errorf("second open reset the marker to %q", m)
	}
	if _, err := os.Stat(old); err != nil {
		t.Errorf("reddit.db was removed: %v", err)
	}
}

// A migration that fails (reddit.db unreadable mid-upgrade) runs again on
// the next open, rather than leaving source.db without the counts for good.
func TestStoreRetriesFailedMigration(t *testing.T) {
	data := t.TempDir()
	old := filepath.Join(data, "reddit", "reddit.db")
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("not a database"), 0o644); err != nil {
		t.Fatal(err)
	}
	if st, err := OpenStore(data); err == nil {
		_ = st.Close()
		t.Fatal("migrating from a broken reddit.db succeeded")
	}
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", old)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{oldRedditSchema, `INSERT INTO posts VALUES ('bbs', 't3_abc', 7, 1790000000)`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()
	st := openTestStore(t, data)
	if m, seen, err := st.Marker("reddit", "bbs", "t3_abc"); err != nil || !seen || m != "7" {
		t.Errorf("marker after retry = %q seen=%v err=%v, want 7", m, seen, err)
	}
}
