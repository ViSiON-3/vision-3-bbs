package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/source"
)

func sourceTestBoard(t *testing.T, extra map[string]string) (configDir, dataDir string) {
	t.Helper()
	sleep = func(time.Duration) {}
	t.Cleanup(func() { sleep = time.Sleep })
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
	for name, body := range extra {
		files[name] = body
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(configDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return configDir, dataDir
}

// pageFetcher serves the Reddit fixtures by URL.
type pageFetcher struct{ pages map[string]string }

func (f pageFetcher) Page(url string, d source.Definition) (string, error) {
	if p, ok := f.pages[url]; ok {
		return p, nil
	}
	return "", fmt.Errorf("no page for %s", url)
}
func (pageFetcher) Script(string, source.Definition, string) ([]source.Item, error) {
	return nil, errors.New("no scripts")
}
func (pageFetcher) Close() {}

func redditFixtures(t *testing.T) pageFetcher {
	t.Helper()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("..", "..", "internal", "source", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	return pageFetcher{pages: map[string]string{
		"https://old.reddit.com/r/bbs/new/":                        read("mini_listing.html"),
		"https://old.reddit.com/r/bbs/comments/aaa111/first_post/": read("mini_post.html"),
		"https://old.reddit.com/r/bbs/comments/bbb222/a_link/":     `<div class="content" role="main"><div class=" thing link" data-fullname="t3_bbb222" data-type="link" data-author="bob" data-timestamp="1790900000000" data-url="https://example.com/page" data-permalink="/r/bbs/comments/bbb222/a_link/" data-comments-count="0"><div class="entry"><p class="title"><a class="title" href="https://example.com/page">A link &amp; more ☃</a></p></div></div></div>`,
	}}
}

func noFetcher(t *testing.T) func(source.Config, source.Definition) (source.Fetcher, error) {
	return func(source.Config, source.Definition) (source.Fetcher, error) {
		t.Fatal("a fetcher was opened")
		return nil, nil
	}
}

// helper reddit sync is an alias of source sync --source reddit, and prints
// the same lines the Reddit gateway did.
func TestRedditSyncAlias(t *testing.T) {
	configDir, dataDir := sourceTestBoard(t, nil)
	o := redditSyncArgs([]string{"--config", configDir, "--data", dataDir, "--sub", "bbs"})
	if o.Source != "reddit" || o.Feed != "bbs" || o.ConfigDir != configDir || o.DataDir != dataDir {
		t.Fatalf("opts = %+v", o)
	}
	f := redditFixtures(t)
	var out, errs strings.Builder
	code := runSourceSync(o, func(source.Config, source.Definition) (source.Fetcher, error) { return f, nil }, &out, &errs)
	if code != 0 || out.String() != "r/bbs -> REDDIT_BBS: 2 posts, 4 comments, 3 pages\n" {
		t.Errorf("code %d, output %q", code, out.String())
	}
	out.Reset()
	errs.Reset()
	if code := runSourceSync(o, func(source.Config, source.Definition) (source.Fetcher, error) { return f, nil }, &out, &errs); code != 0 ||
		out.String() != "r/bbs -> REDDIT_BBS: 0 posts, 0 comments, 1 pages\n" {
		t.Errorf("second run: code %d, output %q", code, out.String())
	}
}

func TestSourceSyncMissingChromeReportsError(t *testing.T) {
	configDir, dataDir := sourceTestBoard(t, nil)
	var out, errs strings.Builder
	code := runSourceSync(sourceSyncOpts{ConfigDir: configDir, DataDir: dataDir},
		func(source.Config, source.Definition) (source.Fetcher, error) {
			return nil, errors.New("chrome not found")
		}, &out, &errs)
	if code == 0 || !strings.Contains(errs.String(), "chrome not found") || out.String() != "" {
		t.Errorf("code %d, stdout %q, stderr %q", code, out.String(), errs.String())
	}
}

func TestSourceSyncUnknownFeed(t *testing.T) {
	configDir, dataDir := sourceTestBoard(t, nil)
	var out, errs strings.Builder
	code := runSourceSync(sourceSyncOpts{ConfigDir: configDir, DataDir: dataDir, Source: "reddit", Feed: "nosuch"}, noFetcher(t), &out, &errs)
	if code == 0 || !strings.Contains(errs.String(), "nosuch") || out.String() != "" {
		t.Errorf("code %d, stdout %q, stderr %q", code, out.String(), errs.String())
	}
}

func TestSourceSyncUnknownSource(t *testing.T) {
	configDir, dataDir := sourceTestBoard(t, nil)
	var out, errs strings.Builder
	code := runSourceSync(sourceSyncOpts{ConfigDir: configDir, DataDir: dataDir, Source: "nosuch"}, noFetcher(t), &out, &errs)
	if code == 0 || !strings.Contains(errs.String(), "nosuch") || out.String() != "" {
		t.Errorf("code %d, stdout %q, stderr %q", code, out.String(), errs.String())
	}
}

// A board still carrying reddit_selectors.json is told it is no longer read.
func TestSourceSyncNotesOldSelectors(t *testing.T) {
	configDir, dataDir := sourceTestBoard(t, map[string]string{"reddit_selectors.json": `{}`})
	f := redditFixtures(t)
	var out, errs strings.Builder
	runSourceSync(sourceSyncOpts{ConfigDir: configDir, DataDir: dataDir}, func(source.Config, source.Definition) (source.Fetcher, error) { return f, nil }, &out, &errs)
	if !strings.Contains(out.String(), "reddit_selectors.json is no longer read") || !strings.Contains(out.String(), "sources/reddit.json") {
		t.Errorf("output %q", out.String())
	}
}

// loggingFetcher records each page load in a log it shares with sleep.
type loggingFetcher struct {
	pageFetcher
	log *[]string
}

func (f loggingFetcher) Page(url string, d source.Definition) (string, error) {
	*f.log = append(*f.log, "load "+url)
	return f.pageFetcher.Page(url, d)
}

// Feeds of one source share the pause between page loads, as subreddits did
// in the Reddit gateway: the second feed's listing waits like any other load.
func TestSourceSyncPausesBetweenFeeds(t *testing.T) {
	areas, _ := json.Marshal([]message.MessageArea{
		{ID: 1, Tag: "REDDIT_BBS", Name: "r/bbs", BasePath: "msgbases/reddit_bbs", AreaType: "local", ACSWrite: "S256"},
		{ID: 2, Tag: "REDDIT_TWO", Name: "r/two", BasePath: "msgbases/reddit_two", AreaType: "local", ACSWrite: "S256"},
	})
	configDir, dataDir := sourceTestBoard(t, map[string]string{
		"message_areas.json": string(areas),
		"reddit.json": `{"subreddits":[{"subreddit":"bbs","area_tag":"REDDIT_BBS","enabled":true},
			{"subreddit":"two","area_tag":"REDDIT_TWO","enabled":true,"comments":false}]}`,
	})
	if err := os.MkdirAll(filepath.Join(dataDir, "msgbases", "reddit_two"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := redditFixtures(t)
	f.pages["https://old.reddit.com/r/two/new/"] = `<div class="content" role="main"></div>`
	var log []string
	sleep = func(d time.Duration) { log = append(log, "sleep") }
	t.Cleanup(func() { sleep = time.Sleep })
	var out, errs strings.Builder
	if code := runSourceSync(sourceSyncOpts{ConfigDir: configDir, DataDir: dataDir},
		func(source.Config, source.Definition) (source.Fetcher, error) { return loggingFetcher{f, &log}, nil }, &out, &errs); code != 0 {
		t.Fatalf("code %d, output %q", code, out.String())
	}
	two := slices.Index(log, "load https://old.reddit.com/r/two/new/")
	if two < 1 || log[two-1] != "sleep" {
		t.Errorf("r/two's listing loaded without a pause before it: %v", log)
	}
}

// A reddit_selectors.json the sysop never edited is the copy setup made; it
// gets no note, so a scheduled run's output stays as it was.
func TestSourceSyncQuietAboutUntouchedSelectors(t *testing.T) {
	shipped, err := os.ReadFile(filepath.Join("..", "..", "internal", "source", "testdata", "shipped_reddit_selectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	// The copy as git checks it out on Linux, and as it does on Windows.
	lf := strings.ReplaceAll(string(shipped), "\r\n", "\n")
	for name, content := range map[string]string{"LF": lf, "CRLF": strings.ReplaceAll(lf, "\n", "\r\n")} {
		t.Run(name, func(t *testing.T) {
			configDir, dataDir := sourceTestBoard(t, map[string]string{"reddit_selectors.json": content})
			f := redditFixtures(t)
			var out, errs strings.Builder
			runSourceSync(sourceSyncOpts{ConfigDir: configDir, DataDir: dataDir}, func(source.Config, source.Definition) (source.Fetcher, error) { return f, nil }, &out, &errs)
			if strings.Contains(out.String(), "reddit_selectors.json") {
				t.Errorf("output %q mentions an unedited reddit_selectors.json", out.String())
			}
		})
	}
}

// A source whose fetcher can't open (Chrome down) fails its own feeds; the
// other sources' feeds still run.
func TestSourceSyncOtherSourcesRunWhenOneCannotOpen(t *testing.T) {
	areas, _ := json.Marshal([]message.MessageArea{
		{ID: 1, Tag: "REDDIT_BBS", Name: "r/bbs", BasePath: "msgbases/reddit_bbs", AreaType: "local", ACSWrite: "S256"},
		{ID: 2, Tag: "NEWS", Name: "News", BasePath: "msgbases/news", AreaType: "local", ACSWrite: "S256"},
	})
	configDir, dataDir := sourceTestBoard(t, map[string]string{
		"message_areas.json": string(areas),
		"sources.json": `{"feeds":[{"source":"reddit","target":"bbs","area_tag":"REDDIT_BBS","enabled":true},
			{"source":"news","target":"tech","area_tag":"NEWS","enabled":true}]}`,
	})
	for _, d := range []string{filepath.Join(dataDir, "msgbases", "news"), filepath.Join(configDir, "sources")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	def := `{"name":"news","fetch":"http","listing_url":"https://news.example/{feed}/","content":"main",
		"listing":{"item":"article","fields":{"id":{"attr":"id"},"title":{"sel":"h2","text":true}}},
		"message":{"from":"News","msgid_suffix":"news"}}`
	if err := os.WriteFile(filepath.Join(configDir, "sources", "news.json"), []byte(def), 0o644); err != nil {
		t.Fatal(err)
	}
	news := pageFetcher{pages: map[string]string{"https://news.example/tech/": `<main><article id="s1"><h2>One</h2></article></main>`}}
	var out, errs strings.Builder
	code := runSourceSync(sourceSyncOpts{ConfigDir: configDir, DataDir: dataDir}, func(_ source.Config, d source.Definition) (source.Fetcher, error) {
		if d.Fetch == "browser" {
			return nil, errors.New("chrome is down")
		}
		return news, nil
	}, &out, &errs)
	if code == 0 || errs.String() != "r/bbs -> REDDIT_BBS: error: chrome is down\n" ||
		out.String() != "news/tech -> NEWS: 1 items, 0 replies, 1 pages\n" {
		t.Errorf("code %d, stdout %q, stderr %q", code, out.String(), errs.String())
	}
}
