package reddit

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// fakeFetcher serves fixed pages by URL and counts requests.
type fakeFetcher struct {
	pages map[string]string
	calls []string
}

func (f *fakeFetcher) Fetch(url string) (string, error) {
	f.calls = append(f.calls, url)
	p, ok := f.pages[url]
	if !ok {
		return "", fmt.Errorf("no page for %s", url)
	}
	return p, nil
}
func (f *fakeFetcher) Close() {}

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

func newTestImporter(t *testing.T, mm *message.MessageManager, f *fakeFetcher) *Importer {
	t.Helper()
	return &Importer{
		MM: mm, Fetcher: f, Sel: shippedSelectors(t),
		Cfg:   Config{PageDelaySeconds: 1, MaxPostsPerSync: 25},
		Store: testStore(t),
		Sleep: func(time.Duration) {},
	}
}

func testStore(t *testing.T) *Store {
	t.Helper()
	st, err := OpenStore(filepath.Join(t.TempDir(), "reddit", "reddit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
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

var bbs = Mapping{Subreddit: "bbs", AreaTag: "REDDIT_BBS", Enabled: true}

func TestSyncImportsPostsAndThreadsComments(t *testing.T) {
	mm := newTestArea(t)
	res := newTestImporter(t, mm, &fakeFetcher{pages: miniPages(t)}).SyncSubreddit(bbs)
	if res.Err != nil || res.Posts != 2 || res.Comments != 4 {
		t.Fatalf("result = %+v", res)
	}
	ids := mm.FindMessagesByMSGID(1, []string{"t3_aaa111@reddit", "t1_c1@reddit", "t1_c2@reddit", "t1_c3@reddit", "t1_c5@reddit"})
	if len(ids) != 5 {
		t.Fatalf("MSGIDs found: %v", ids)
	}
	post, _ := mm.GetMessage(1, ids["t3_aaa111@reddit"])
	if post.From != "u/alice" || post.To != "All" || post.Subject != "First post" ||
		!strings.Contains(post.Body, "Hello there <https://example.com>") || !strings.HasSuffix(strings.TrimSpace(post.Body), postURL) {
		t.Errorf("post message = %+v", post)
	}
	nested, _ := mm.GetMessage(1, ids["t1_c2@reddit"])
	if nested.Subject != "Re: First post" || nested.ReplyToNum != ids["t1_c1@reddit"] {
		t.Errorf("nested reply = %+v, want reply to #%d", nested, ids["t1_c1@reddit"])
	}
	kl, _ := mm.GetMessageKludges(1, ids["t3_aaa111@reddit"])
	joined := strings.Join(kl, "\n")
	if !strings.Contains(joined, "CHRS: UTF-8 4") || !strings.Contains(joined, "REDDIT: "+postURL) {
		t.Errorf("kludges = %v", kl)
	}
}

func TestSyncSecondRunImportsNothing(t *testing.T) {
	mm := newTestArea(t)
	f := &fakeFetcher{pages: miniPages(t)}
	im := newTestImporter(t, mm, f)
	im.SyncSubreddit(bbs)
	f.calls = nil
	res := im.SyncSubreddit(bbs)
	if res.Err != nil || res.Posts != 0 || res.Comments != 0 {
		t.Fatalf("second run = %+v", res)
	}
	if len(f.calls) != 1 {
		t.Errorf("second run loaded %v, want only the listing", f.calls)
	}
	if n, _ := mm.GetMessageCountForArea(1); n != 6 {
		t.Errorf("area holds %d messages, want 6", n)
	}
}

func TestSyncNewCommentAddsOnlyThatComment(t *testing.T) {
	mm := newTestArea(t)
	pages := miniPages(t)
	f := &fakeFetcher{pages: pages}
	im := newTestImporter(t, mm, f)
	im.SyncSubreddit(bbs)

	pages[listingURL] = strings.Replace(pages[listingURL], `data-comments-count="2"`, `data-comments-count="5"`, 1)
	// c2's replies box is the first empty child div on the page.
	pages[postPage] = strings.Replace(pages[postPage], `<div class="child"></div>`,
		`<div class="child"><div class="sitetable listing"><div class=" thing id-t1_c4 comment" data-fullname="t1_c4" data-type="comment" data-author="erin"><div class="entry"><p class="tagline"><time class="live-timestamp" datetime="2026-10-03T04:00:00+00:00">1h</time></p><form class="usertext"><div class="usertext-body"><div class="md"><p>Later</p></div></div></form></div><div class="child"></div></div></div></div>`, 1)
	res := im.SyncSubreddit(bbs)
	if res.Err != nil || res.Posts != 0 || res.Comments != 1 {
		t.Fatalf("result = %+v", res)
	}
	ids := mm.FindMessagesByMSGID(1, []string{"t1_c2@reddit", "t1_c4@reddit"})
	later, _ := mm.GetMessage(1, ids["t1_c4@reddit"])
	if later.ReplyToNum != ids["t1_c2@reddit"] {
		t.Errorf("new comment replies to #%d, want #%d", later.ReplyToNum, ids["t1_c2@reddit"])
	}
}

func TestSyncReplyToRemovedComment(t *testing.T) {
	mm := newTestArea(t)
	newTestImporter(t, mm, &fakeFetcher{pages: miniPages(t)}).SyncSubreddit(bbs)
	ids := mm.FindMessagesByMSGID(1, []string{"t1_c3@reddit", "t1_c5@reddit"})
	reply, _ := mm.GetMessage(1, ids["t1_c5@reddit"])
	if reply.ReplyToNum != ids["t1_c3@reddit"] {
		t.Errorf("reply to the removed comment points at #%d, want #%d", reply.ReplyToNum, ids["t1_c3@reddit"])
	}
}

func TestSyncImportsDeletedAuthor(t *testing.T) {
	mm := newTestArea(t)
	newTestImporter(t, mm, &fakeFetcher{pages: miniPages(t)}).SyncSubreddit(bbs)
	n := mm.FindMessageByMSGID(1, "t1_c3@reddit")
	m, _ := mm.GetMessage(1, n)
	if m.From != "u/[deleted]" || !strings.Contains(m.Body, "[removed]") {
		t.Errorf("deleted comment = %+v", m)
	}
}

func TestSyncKeepsUTF8(t *testing.T) {
	mm := newTestArea(t)
	newTestImporter(t, mm, &fakeFetcher{pages: miniPages(t)}).SyncSubreddit(bbs)
	m, _ := mm.GetMessage(1, mm.FindMessageByMSGID(1, "t3_bbb222@reddit"))
	if m.Subject != "A link & more ☃" || !strings.Contains(m.Body, "https://example.com/page") {
		t.Errorf("link post = %+v", m)
	}
}

func TestSyncMissingAreaIsReported(t *testing.T) {
	mm := newTestArea(t)
	f := &fakeFetcher{pages: miniPages(t)}
	res := newTestImporter(t, mm, f).SyncSubreddit(Mapping{Subreddit: "bbs", AreaTag: "NOPE", Enabled: true})
	if res.Err == nil || !strings.Contains(res.Err.Error(), "NOPE") {
		t.Errorf("err = %v", res.Err)
	}
	if len(f.calls) != 0 {
		t.Errorf("fetched %v for a missing area", f.calls)
	}
}

func TestSyncDryRunWritesNothing(t *testing.T) {
	mm := newTestArea(t)
	var out strings.Builder
	im := newTestImporter(t, mm, &fakeFetcher{pages: miniPages(t)})
	im.DryRun, im.Out = true, &out
	res := im.SyncSubreddit(bbs)
	if res.Err != nil || res.Posts != 2 {
		t.Fatalf("result = %+v", res)
	}
	if n, _ := mm.GetMessageCountForArea(1); n != 0 {
		t.Errorf("dry run wrote %d messages", n)
	}
	if !strings.Contains(out.String(), "First post") {
		t.Errorf("dry run printed %q", out.String())
	}
}

func TestStorePersistsCountsAndRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reddit", "reddit.db")
	st, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.Count("bbs", "t3_x"); ok || err != nil {
		t.Fatalf("unseen post: ok=%v err=%v", ok, err)
	}
	if err := st.Set("bbs", "t3_x", 3); err != nil {
		t.Fatal(err)
	}
	if err := st.Set("bbs", "t3_x", 7); err != nil { // upsert
		t.Fatal(err)
	}
	when := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	if err := st.RecordRun(SubResult{Subreddit: "bbs", AreaTag: "REDDIT_BBS", Posts: 2, Comments: 5, Pages: 3}, when); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordRun(SubResult{Subreddit: "bbs", AreaTag: "REDDIT_BBS", Err: fmt.Errorf("boom")}, when.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	st, err = OpenStore(path) // reopen: the data is on disk
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if n, ok, err := st.Count("bbs", "t3_x"); !ok || n != 7 || err != nil {
		t.Errorf("Count = %d, %v, %v", n, ok, err)
	}
	last, at, ok, err := st.LastRun("bbs")
	if err != nil || !ok || !at.Equal(when.Add(time.Hour)) || last.Err == nil || last.Err.Error() != "boom" {
		t.Errorf("LastRun = %+v at %v, ok=%v err=%v", last, at, ok, err)
	}
}

// A run stops before a page load that could carry it past its deadline, so
// the scheduler never kills it mid-run (which would leave a tab open in the
// person's Chrome). The next run picks up what was left.
func TestSyncStopsAtRunDeadline(t *testing.T) {
	mm := newTestArea(t)
	f := &fakeFetcher{pages: miniPages(t)}
	im := newTestImporter(t, mm, f)
	clock := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	im.Now = func() time.Time { return clock }
	im.Sleep = func(d time.Duration) { clock = clock.Add(d) }
	im.Cfg.PageDelaySeconds = 20
	// A load is only started if the pause before it plus PageTimeout fits
	// before the deadline. The fake fetch takes no time, so only pauses move
	// the clock: the third load would be checked at +20s and could end at
	// +100s, so a deadline of +99s allows the listing and one post page.
	im.Deadline = clock.Add(2*20*time.Second + PageTimeout - time.Second)

	res := im.SyncSubreddit(bbs)
	if !errors.Is(res.Err, ErrRunTimeLimit) {
		t.Fatalf("err = %v, want ErrRunTimeLimit", res.Err)
	}
	if len(f.calls) != 2 {
		t.Fatalf("loaded %v, want the listing and one post page", f.calls)
	}

	// The next run, with time to spare, finishes the job without duplicates.
	im.Deadline = time.Time{}
	res = im.SyncSubreddit(bbs)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if n, _ := mm.GetMessageCountForArea(1); n != 6 {
		t.Errorf("area holds %d messages after the follow-up run, want 6", n)
	}
}
