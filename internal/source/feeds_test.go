package source

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// A reddit.json from before sources.json: a board with no sources.json
// keeps syncing with no config change.
const legacyRedditJSON = `{
  "chrome_debug_url": "http://127.0.0.1:9222",
  "page_delay_seconds": 20,
  "max_posts_per_sync": 25,
  "max_run_seconds": 540,
  "subreddits": [
    {"subreddit": "bbs", "area_tag": "REDDIT_BBS", "enabled": true},
    {"subreddit": "news", "area_tag": "REDDIT_NEWS", "enabled": true, "max_comments": 10},
    {"subreddit": "quiet", "area_tag": "REDDIT_QUIET", "enabled": false, "comments": false}
  ]
}`

func TestLoadConfigFromRedditJSON(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, map[string]string{"reddit.json": legacyRedditJSON}))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.FromRedditJSON || cfg.ChromeDebugURL != "http://127.0.0.1:9222" || cfg.MaxItemsPerSync != 25 ||
		cfg.PageDelay() != 20*time.Second || cfg.MaxRun() != 540*time.Second || cfg.PageTimeout() != 60*time.Second {
		t.Errorf("config = %+v", cfg)
	}
	if len(cfg.Feeds) != 3 {
		t.Fatalf("feeds = %+v", cfg.Feeds)
	}
	bbs, news, quiet := cfg.Feeds[0], cfg.Feeds[1], cfg.Feeds[2]
	if bbs.Source != "reddit" || bbs.Target != "bbs" || bbs.AreaTag != "REDDIT_BBS" || !bbs.Enabled || !bbs.WantReplies() {
		t.Errorf("bbs feed = %+v", bbs)
	}
	if news.MaxReplies != 10 {
		t.Errorf("news feed = %+v, want max_replies 10 from max_comments", news)
	}
	if quiet.Enabled || quiet.WantReplies() {
		t.Errorf("quiet feed = %+v, want disabled with replies off from comments false", quiet)
	}
}

func TestLoadConfigPrefersSourcesJSON(t *testing.T) {
	dir := writeConfig(t, map[string]string{
		"reddit.json":  legacyRedditJSON,
		"sources.json": `{"feeds":[{"source":"reddit","target":"retrobattlestations","area_tag":"REDDIT_RBS","enabled":true}]}`,
	})
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FromRedditJSON || len(cfg.Feeds) != 1 || cfg.Feeds[0].Target != "retrobattlestations" {
		t.Errorf("config = %+v", cfg)
	}
	if cfg.ChromeDebugURL != "http://127.0.0.1:9222" || cfg.MaxItemsPerSync != 25 || cfg.MaxRun() != 540*time.Second {
		t.Errorf("defaults = %+v", cfg)
	}
}

func TestLoadConfigRejects(t *testing.T) {
	for name, body := range map[string]string{
		"a target the definition refuses": `{"feeds":[{"source":"reddit","target":"r/bbs","area_tag":"REDDIT_BBS","enabled":true}]}`,
		"an unknown source":               `{"feeds":[{"source":"nosuch","target":"x","area_tag":"X","enabled":true}]}`,
		"an empty area tag":               `{"feeds":[{"source":"reddit","target":"bbs","area_tag":" ","enabled":true}]}`,
		"a negative max_replies":          `{"feeds":[{"source":"reddit","target":"bbs","area_tag":"REDDIT_BBS","enabled":true,"max_replies":-1}]}`,
		"an unknown key":                  `{"feedz":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadConfig(writeConfig(t, map[string]string{"sources.json": body}))
			if err == nil || !strings.Contains(err.Error(), "sources.json") {
				t.Errorf("err = %v, want an error naming sources.json", err)
			}
		})
	}
	if _, err := LoadConfig(writeConfig(t, map[string]string{"reddit.json": `{"subreddits":[{"subreddit":"r/bbs","area_tag":"X","enabled":true}]}`})); err == nil ||
		!strings.Contains(err.Error(), "reddit.json") {
		t.Errorf("a bad subreddit in reddit.json gave err = %v", err)
	}
}

func TestLoadConfigNeitherFile(t *testing.T) {
	_, err := LoadConfig(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "sources.json") {
		t.Errorf("err = %v, want one naming sources.json", err)
	}
}
