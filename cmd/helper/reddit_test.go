package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/reddit"
)

func redditTestBoard(t *testing.T) (configDir, dataDir string) {
	t.Helper()
	dir := t.TempDir()
	configDir, dataDir = filepath.Join(dir, "configs"), filepath.Join(dir, "data")
	for _, d := range []string{configDir, filepath.Join(dataDir, "msgbases", "reddit_bbs")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	areas, _ := json.Marshal([]message.MessageArea{{ID: 1, Tag: "REDDIT_BBS", Name: "r/bbs", BasePath: "msgbases/reddit_bbs", AreaType: "local", ACSWrite: "S256"}})
	files := map[string]string{
		"message_areas.json": string(areas),
		"config.json":        `{"boardName":"Test"}`,
		"reddit.json":        `{"subreddits":[{"subreddit":"bbs","area_tag":"REDDIT_BBS","enabled":true}]}`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(configDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return configDir, dataDir
}

func TestRunSyncMissingChromeReportsError(t *testing.T) {
	configDir, dataDir := redditTestBoard(t)
	var out strings.Builder
	code := runRedditSync(redditSyncOpts{ConfigDir: configDir, DataDir: dataDir},
		func(string, reddit.Selectors) (reddit.Fetcher, error) { return nil, errors.New("chrome not found") }, &out)
	if code == 0 || !strings.Contains(out.String(), "chrome not found") {
		t.Errorf("code %d, output %q", code, out.String())
	}
}

func TestRunSyncUnknownSub(t *testing.T) {
	configDir, dataDir := redditTestBoard(t)
	var out strings.Builder
	code := runRedditSync(redditSyncOpts{ConfigDir: configDir, DataDir: dataDir, Sub: "nosuch"},
		func(string, reddit.Selectors) (reddit.Fetcher, error) {
			t.Fatal("Chrome attached for an unknown subreddit")
			return nil, nil
		}, &out)
	if code == 0 || !strings.Contains(out.String(), "nosuch") {
		t.Errorf("code %d, output %q", code, out.String())
	}
}
