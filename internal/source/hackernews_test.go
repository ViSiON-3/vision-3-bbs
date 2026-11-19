package source

import (
	"os"
	"strings"
	"testing"
)

func TestHackerNewsDefinitionAndExtraction(t *testing.T) {
	d, err := LoadDefinition(t.TempDir(), "hackernews")
	if err != nil {
		t.Fatal(err)
	}
	if d.Fetch != "http" || d.Threads {
		t.Fatalf("expected a plain HTTP headline source: %+v", d)
	}
	if err := d.CheckTarget("news"); err != nil {
		t.Fatal(err)
	}
	if err := d.CheckTarget("../item?id=123"); err == nil {
		t.Fatal("accepted unexpected target")
	}
	if got := d.ListingURLFor("news"); got != "https://news.ycombinator.com/news" {
		t.Fatal(got)
	}
	raw, err := os.ReadFile("testdata/hackernews_listing.html")
	if err != nil {
		t.Fatal(err)
	}
	items, err := ExtractListing(string(raw), d)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items: %+v", len(items), items)
	}
	if items[0].ID != "123" || items[0].Title != "Retro machines & modern code" || items[1].ID != "124" {
		t.Fatalf("items=%+v", items)
	}
	im := &Importer{Def: d}
	if got := im.rootBody(items[0]); got != "Retro machines & modern code\n\nhttps://example.org/retro" {
		t.Fatalf("body=%q", got)
	}
	if got := im.from(Feed{Target: "news"}, items[0], items[0]); got != "Hacker News" {
		t.Fatalf("from=%q", got)
	}
	if got := im.msgID(items[0].ID); got != "123@hackernews" {
		t.Fatalf("MSGID=%q", got)
	}
	if strings.Contains(im.rootBody(items[0]), "someone") {
		t.Fatal("listing metadata leaked into body")
	}
}

func TestHackerNewsImportIsIdempotent(t *testing.T) {
	d, err := LoadDefinition(t.TempDir(), "hackernews")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/hackernews_listing.html")
	if err != nil {
		t.Fatal(err)
	}
	mm := newTestArea(t)
	im := newTestImporter(t, mm, &pageFetcher{pages: map[string]string{d.ListingURLFor("news"): string(raw)}}, d)
	feed := Feed{Source: "hackernews", Target: "news", AreaTag: "REDDIT_BBS", Enabled: true}
	for i, want := range []int{2, 0} {
		result := im.SyncFeed(feed)
		if result.Err != nil || result.Items != want || result.Replies != 0 || result.Pages != 1 {
			t.Fatalf("run %d: %+v", i, result)
		}
	}
	base, err := mm.GetBase(1)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	if count, err := base.GetMessageCount(); err != nil || count != 2 {
		t.Fatalf("message count=%d err=%v", count, err)
	}
}
