package reddit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "reddit.json", `{"subreddits":[{"subreddit":"bbs","area_tag":"REDDIT_BBS","enabled":true}]}`)
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PageDelay() != 20*time.Second || cfg.MaxPostsPerSync != 25 {
		t.Errorf("defaults not applied: %+v", cfg)
	}
	if len(cfg.Subreddits) != 1 || cfg.Subreddits[0].AreaTag != "REDDIT_BBS" {
		t.Errorf("subreddits = %+v", cfg.Subreddits)
	}
}

func TestLoadConfigRejectsBadEntries(t *testing.T) {
	for name, body := range map[string]string{
		"missing file": "",
		"bad name":     `{"subreddits":[{"subreddit":"r/bbs","area_tag":"X","enabled":true}]}`,
		"empty tag":    `{"subreddits":[{"subreddit":"bbs","area_tag":"","enabled":true}]}`,
		"bad json":     `{`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if body != "" {
				writeFile(t, dir, "reddit.json", body)
			}
			if _, err := LoadConfig(dir); err == nil {
				t.Error("want an error")
			}
		})
	}
}

func TestLoadSelectorsFallsBackToShippedCopy(t *testing.T) {
	sel, err := LoadSelectors(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if sel.PostType != "link" || sel.IDAttr != "data-fullname" || sel.ChildClass != "child" {
		t.Errorf("shipped selectors not loaded: %+v", sel)
	}
	if got := sel.ListingURLFor("bbs"); got != "https://old.reddit.com/r/bbs/new/" {
		t.Errorf("ListingURLFor = %q", got)
	}
	if got := sel.PageURL("/r/bbs/comments/x/y/"); got != "https://old.reddit.com/r/bbs/comments/x/y/" {
		t.Errorf("PageURL = %q", got)
	}
}

func TestLoadSelectorsLocalFileWins(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "reddit_selectors.json", `{"listing_url":"https://x/r/{sub}/","page_origin":"https://x","content_selector":"main","ready_selector":"main","thing_element":"div","type_attr":"t","post_type":"p","comment_type":"c","id_attr":"id"}`)
	sel, err := LoadSelectors(dir)
	if err != nil {
		t.Fatal(err)
	}
	if sel.PostType != "p" || !strings.HasPrefix(sel.ListingURLFor("a"), "https://x/r/a") {
		t.Errorf("local selectors not used: %+v", sel)
	}
}

func TestLoadConfigDefaultDebugURL(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "reddit.json", `{"subreddits":[]}`)
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ChromeDebugURL != "http://127.0.0.1:9222" {
		t.Errorf("ChromeDebugURL = %q", cfg.ChromeDebugURL)
	}
}
