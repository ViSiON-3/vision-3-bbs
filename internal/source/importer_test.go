package source

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// pageFetcher serves fixed pages by URL and counts requests.
type pageFetcher struct {
	pages map[string]string
	calls []string
}

func (f *pageFetcher) Page(url string, d Definition) (string, error) {
	f.calls = append(f.calls, url)
	p, ok := f.pages[url]
	if !ok {
		return "", fmt.Errorf("no page for %s", url)
	}
	return p, nil
}
func (f *pageFetcher) Script(url string, d Definition, script string) ([]Item, error) {
	return nil, errors.New("pageFetcher runs no scripts")
}
func (f *pageFetcher) Close() {}

func newTestArea(t *testing.T) *message.MessageManager {
	t.Helper()
	dir := t.TempDir()
	configDir, dataDir := filepath.Join(dir, "configs"), filepath.Join(dir, "data")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "msgbases", "reddit_bbs"), 0o755); err != nil {
		t.Fatal(err)
	}
	areas := []message.MessageArea{{ID: 1, Tag: "REDDIT_BBS", Name: "r/bbs", BasePath: "msgbases/reddit_bbs", AreaType: "local", ACSWrite: "S256"}}
	data, _ := json.Marshal(areas)
	if err := os.WriteFile(filepath.Join(configDir, "message_areas.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	mm, err := message.NewMessageManager(dataDir, configDir, "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	return mm
}

func newTestImporter(t *testing.T, mm *message.MessageManager, f Fetcher, d Definition) *Importer {
	t.Helper()
	return &Importer{
		MM: mm, Fetcher: f, Def: d, Store: openTestStore(t, t.TempDir()),
		PageDelay: time.Second, PageTimeout: 60 * time.Second, MaxItems: 25,
		Sleep: func(time.Duration) {},
	}
}

const (
	listingURL = "https://old.reddit.com/r/bbs/new/"
	postPage   = "https://old.reddit.com/r/bbs/comments/aaa111/first_post/"
	linkPage   = "https://old.reddit.com/r/bbs/comments/bbb222/a_link/"
	postURL    = "https://www.reddit.com/r/bbs/comments/aaa111/first_post/" // shown in messages
)

func miniPages(t *testing.T) map[string]string {
	return map[string]string{
		listingURL: fixture(t, "mini_listing.html"),
		postPage:   fixture(t, "mini_post.html"),
		linkPage:   `<div class="content" role="main"><div class=" thing link" data-fullname="t3_bbb222" data-type="link" data-author="bob" data-timestamp="1790900000000" data-url="https://example.com/page" data-permalink="/r/bbs/comments/bbb222/a_link/" data-comments-count="0"><div class="entry"><p class="title"><a class="title" href="https://example.com/page">A link &amp; more ☃</a></p></div></div></div>`,
	}
}

var bbs = Feed{Source: "reddit", Target: "bbs", AreaTag: "REDDIT_BBS", Enabled: true}

// goldenMessage matches internal/reddit's golden file, recorded from the
// importer this one replaces.
type goldenMessage struct {
	AreaTag  string   `json:"area_tag"`
	MsgNum   int      `json:"msg_num"`
	From     string   `json:"from"`
	To       string   `json:"to"`
	Subject  string   `json:"subject"`
	Text     string   `json:"text"`
	MsgID    string   `json:"msg_id"`
	ReplyID  string   `json:"reply_id"`
	ReplyTo  uint32   `json:"reply_to"`
	DateTime int64    `json:"date_time"`
	Kludges  []string `json:"kludges"`
}

func TestImportMatchesGolden(t *testing.T) {
	mm := newTestArea(t)
	res := newTestImporter(t, mm, &pageFetcher{pages: miniPages(t)}, redditDef(t)).SyncFeed(bbs)
	if res.Err != nil {
		t.Fatalf("sync: %v", res.Err)
	}
	base, err := mm.GetBase(1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = base.Close() }()
	n, err := base.GetMessageCount()
	if err != nil {
		t.Fatal(err)
	}
	var got []goldenMessage
	for i := 1; i <= n; i++ {
		m, err := base.ReadMessage(i)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, goldenMessage{AreaTag: "REDDIT_BBS", MsgNum: i, From: m.From, To: m.To, Subject: m.Subject, Text: m.Text,
			MsgID: m.MsgID, ReplyID: m.ReplyID, ReplyTo: m.Header.ReplyTo, DateTime: m.DateTime.Unix(), Kludges: m.Kludges})
	}
	data, err := os.ReadFile(filepath.Join("testdata", "reddit_golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want []goldenMessage
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		for i := range max(len(got), len(want)) {
			var g, w goldenMessage
			if i < len(got) {
				g = got[i]
			}
			if i < len(want) {
				w = want[i]
			}
			if !reflect.DeepEqual(g, w) {
				t.Errorf("message %d:\n got %+v\nwant %+v", i+1, g, w)
			}
		}
	}
}

func TestImportSecondRunImportsNothing(t *testing.T) {
	mm := newTestArea(t)
	f := &pageFetcher{pages: miniPages(t)}
	im := newTestImporter(t, mm, f, redditDef(t))
	im.SyncFeed(bbs)
	f.calls = nil
	res := im.SyncFeed(bbs)
	if res.Err != nil || res.Items != 0 || res.Replies != 0 {
		t.Fatalf("second run = %+v", res)
	}
	if len(f.calls) != 1 {
		t.Errorf("second run loaded %v, want only the listing", f.calls)
	}
}

func TestImportRaisedMarkerAddsOnlyTheNewReply(t *testing.T) {
	mm := newTestArea(t)
	pages := miniPages(t)
	im := newTestImporter(t, mm, &pageFetcher{pages: pages}, redditDef(t))
	im.SyncFeed(bbs)

	pages[listingURL] = strings.Replace(pages[listingURL], `data-comments-count="2"`, `data-comments-count="5"`, 1)
	pages[postPage] = strings.Replace(pages[postPage], `<div class="child"></div>`,
		`<div class="child"><div class="sitetable listing"><div class=" thing id-t1_c4 comment" data-fullname="t1_c4" data-type="comment" data-author="erin"><div class="entry"><p class="tagline"><time class="live-timestamp" datetime="2026-10-03T04:00:00+00:00">1h</time></p><form class="usertext"><div class="usertext-body"><div class="md"><p>Later</p></div></div></form></div><div class="child"></div></div></div></div>`, 1)
	res := im.SyncFeed(bbs)
	if res.Err != nil || res.Items != 0 || res.Replies != 1 {
		t.Fatalf("result = %+v", res)
	}
	ids := mm.FindMessagesByMSGID(1, []string{"t1_c2@reddit", "t1_c4@reddit"})
	later, _ := mm.GetMessage(1, ids["t1_c4@reddit"])
	if later.ReplyToNum != ids["t1_c2@reddit"] {
		t.Errorf("new reply points at #%d, want #%d", later.ReplyToNum, ids["t1_c2@reddit"])
	}
}

func TestImportMissingParentThreadsUnderRoot(t *testing.T) {
	mm := newTestArea(t)
	pages := miniPages(t)
	// A reply whose parent is not on the page: a nested comment moved out of
	// its parent's container, with its parent removed.
	d := redditDef(t)
	d.Thread.Parent = "" // every reply's parent is unknown to the extraction
	newTestImporter(t, mm, &pageFetcher{pages: pages}, d).SyncFeed(bbs)
	ids := mm.FindMessagesByMSGID(1, []string{"t3_aaa111@reddit", "t1_c2@reddit"})
	nested, _ := mm.GetMessage(1, ids["t1_c2@reddit"])
	if nested.ReplyToNum != ids["t3_aaa111@reddit"] {
		t.Errorf("orphan reply points at #%d, want the root #%d", nested.ReplyToNum, ids["t3_aaa111@reddit"])
	}
}

func TestImportMissingAreaIsReported(t *testing.T) {
	mm := newTestArea(t)
	f := &pageFetcher{pages: miniPages(t)}
	res := newTestImporter(t, mm, f, redditDef(t)).SyncFeed(Feed{Source: "reddit", Target: "bbs", AreaTag: "NOPE", Enabled: true})
	if res.Err == nil || !strings.Contains(res.Err.Error(), "NOPE") || len(f.calls) != 0 {
		t.Errorf("err = %v, calls %v", res.Err, f.calls)
	}
}

func TestImportDryRunWritesNothing(t *testing.T) {
	mm := newTestArea(t)
	var out strings.Builder
	im := newTestImporter(t, mm, &pageFetcher{pages: miniPages(t)}, redditDef(t))
	im.DryRun, im.Out = true, &out
	res := im.SyncFeed(bbs)
	if res.Err != nil || res.Items != 2 {
		t.Fatalf("result = %+v", res)
	}
	if n, _ := mm.GetMessageCountForArea(1); n != 0 {
		t.Errorf("dry run wrote %d messages", n)
	}
	if !strings.Contains(out.String(), "First post") {
		t.Errorf("dry run printed %q", out.String())
	}
}

func TestImportStopsAtRunDeadline(t *testing.T) {
	mm := newTestArea(t)
	f := &pageFetcher{pages: miniPages(t)}
	im := newTestImporter(t, mm, f, redditDef(t))
	clock := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	im.Now = func() time.Time { return clock }
	im.Sleep = func(d time.Duration) { clock = clock.Add(d) }
	im.PageDelay = 20 * time.Second
	im.Deadline = clock.Add(2*20*time.Second + im.PageTimeout - time.Second)

	res := im.SyncFeed(bbs)
	if !errors.Is(res.Err, ErrRunTimeLimit) || len(f.calls) != 2 {
		t.Fatalf("err = %v, loaded %v; want ErrRunTimeLimit after the listing and one thread", res.Err, f.calls)
	}
	im.Deadline = time.Time{}
	if res = im.SyncFeed(bbs); res.Err != nil {
		t.Fatal(res.Err)
	}
	if n, _ := mm.GetMessageCountForArea(1); n != 6 {
		t.Errorf("area holds %d messages after the follow-up run, want 6", n)
	}
}

// A flat source writes one message per item and never loads a thread page.
func TestImportFlatSource(t *testing.T) {
	mm := newTestArea(t)
	d := Definition{
		Name: "news", Fetch: "http", ListingURL: "https://news.example/{feed}/", PageOrigin: "https://news.example", Content: "main",
		Listing: Extraction{Item: "article", Fields: map[string]Field{
			"id":        {Attr: "id"},
			"title":     {Sel: "h2", Text: true},
			"author":    {Attr: "data-by"},
			"body":      {Sel: ".text", HTML: true},
			"url":       {Sel: "a", Attr: "href", URL: true},
			"posted_at": {Attr: "data-at", Time: "rfc3339"},
		}},
		Message: MessageShape{From: "{author}", MsgIDSuffix: "news", Kludge: "NEWS", Footer: true},
	}
	f := &pageFetcher{pages: map[string]string{"https://news.example/tech/": `<main>
		<article id="s2" data-by="Ann" data-at="2026-10-22T06:00:00Z"><h2>Second</h2><div class="text"><p>Two</p></div><a href="/s2">more</a></article>
		<article id="s1" data-by="Bo" data-at="2026-10-22T05:00:00Z"><h2>First</h2><div class="text"><p>One</p></div><a href="/s1">more</a></article>
	</main>`}}
	res := newTestImporter(t, mm, f, d).SyncFeed(Feed{Source: "news", Target: "tech", AreaTag: "REDDIT_BBS", Enabled: true})
	if res.Err != nil || res.Items != 2 || res.Replies != 0 || len(f.calls) != 1 {
		t.Fatalf("result = %+v, calls %v", res, f.calls)
	}
	first, _ := mm.GetMessage(1, 1) // oldest first
	if first.Subject != "First" || first.From != "Bo" || !strings.HasSuffix(strings.TrimSpace(first.Body), "https://news.example/s1") {
		t.Errorf("first message = %+v", first)
	}
	kl, _ := mm.GetMessageKludges(1, 1)
	if !strings.Contains(strings.Join(kl, "\n"), "NEWS: https://news.example/s1") {
		t.Errorf("kludges = %v", kl)
	}
}

// max_replies keeps a thread's first replies to the root and none of their
// replies.
func TestImportCapsTopReplies(t *testing.T) {
	mm := newTestArea(t)
	capped := Feed{Source: "reddit", Target: "bbs", AreaTag: "REDDIT_BBS", Enabled: true, MaxReplies: 1}
	res := newTestImporter(t, mm, &pageFetcher{pages: miniPages(t)}, redditDef(t)).SyncFeed(capped)
	if res.Err != nil || res.Items != 2 || res.Replies != 1 {
		t.Fatalf("result = %+v, want 2 items and 1 reply", res)
	}
	ids := mm.FindMessagesByMSGID(1, []string{"t1_c1@reddit", "t1_c2@reddit", "t1_c3@reddit", "t1_c5@reddit"})
	if ids["t1_c1@reddit"] == 0 || len(ids) != 1 {
		t.Errorf("imported %v, want only the first top-level reply t1_c1", ids)
	}
}

// A capped thread that already had as many replies as the cap is not
// reloaded when its marker rises: on a busy subreddit every count rises.
func TestImportCappedThreadNotReloaded(t *testing.T) {
	mm := newTestArea(t)
	pages := miniPages(t)
	f := &pageFetcher{pages: pages}
	im := newTestImporter(t, mm, f, redditDef(t))
	capped := Feed{Source: "reddit", Target: "bbs", AreaTag: "REDDIT_BBS", Enabled: true, MaxReplies: 1}
	im.SyncFeed(capped)
	pages[listingURL] = strings.Replace(pages[listingURL], `data-comments-count="2"`, `data-comments-count="50"`, 1)
	f.calls = nil
	if res := im.SyncFeed(capped); res.Err != nil || res.Replies != 0 {
		t.Fatalf("second run = %+v", res)
	}
	if len(f.calls) != 1 {
		t.Errorf("second run loaded %v, want only the listing", f.calls)
	}
}

// With replies off, items come in with their bodies and replies never do.
func TestImportItemsOnly(t *testing.T) {
	mm := newTestArea(t)
	off := false
	itemsOnly := Feed{Source: "reddit", Target: "bbs", AreaTag: "REDDIT_BBS", Enabled: true, Replies: &off}
	res := newTestImporter(t, mm, &pageFetcher{pages: miniPages(t)}, redditDef(t)).SyncFeed(itemsOnly)
	if res.Err != nil || res.Items != 2 || res.Replies != 0 {
		t.Fatalf("result = %+v, want 2 items and no replies", res)
	}
	post, _ := mm.GetMessage(1, mm.FindMessageByMSGID(1, "t3_aaa111@reddit"))
	if !strings.Contains(post.Body, "Hello there") {
		t.Errorf("post body = %q, want the item's text", post.Body)
	}
}
