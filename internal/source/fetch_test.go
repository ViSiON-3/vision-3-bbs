package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestHTTPFetcherPage(t *testing.T) {
	inner := fixture(t, "mini_listing.html")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.UserAgent(), "ViSiON/3 ") {
			t.Errorf("User-Agent = %q", r.UserAgent())
		}
		_, _ = fmt.Fprintf(w, `<html><body><div id="junk">secret session token</div>%s</body></html>`, inner)
	}))
	t.Cleanup(srv.Close)
	page, err := NewHTTPFetcher(5*time.Second).Page(srv.URL, redditDef(t))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(page, "secret") || !strings.Contains(page, `role="main"`) {
		t.Errorf("Page kept more or less than the content element: %.120q", page)
	}
	items, err := ExtractListing(page, redditDef(t))
	if err != nil || len(items) == 0 {
		t.Errorf("content element did not hold the items: %v, %d items", err, len(items))
	}
}

func TestHTTPFetcherErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gone" {
			http.Error(w, "gone", http.StatusNotFound)
			return
		}
		_, _ = fmt.Fprint(w, `<html><body><p>no content here</p></body></html>`)
	}))
	t.Cleanup(srv.Close)
	f := NewHTTPFetcher(5 * time.Second)
	if _, err := f.Page(srv.URL+"/gone", redditDef(t)); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("404 page: err = %v", err)
	}
	if _, err := f.Page(srv.URL, redditDef(t)); err == nil {
		t.Error("a page without the content element was accepted")
	}
	if _, err := f.Script(srv.URL, redditDef(t), "return []"); err == nil {
		t.Error("HTTPFetcher ran a script")
	}
}

// recordingFetcher records which Fetcher method each call used.
type recordingFetcher struct {
	calls []string
	page  string
	items []Item
}

func (r *recordingFetcher) Page(u string, d Definition) (string, error) {
	r.calls = append(r.calls, "page "+u)
	return r.page, nil
}
func (r *recordingFetcher) Script(u string, d Definition, script string) ([]Item, error) {
	r.calls = append(r.calls, "script "+u)
	return r.items, nil
}
func (r *recordingFetcher) Close() {}

func TestListingUsesScriptOrSelectors(t *testing.T) {
	d := redditDef(t)
	rf := &recordingFetcher{page: fixture(t, "mini_listing.html")}
	items, err := Listing(rf, d, "bbs")
	if err != nil || len(items) == 0 || rf.calls[0] != "page https://old.reddit.com/r/bbs/new/" {
		t.Errorf("selectors: items %d, err %v, calls %v", len(items), err, rf.calls)
	}

	d.Listing = Extraction{Script: "return [{id: 's1'}]"}
	rf = &recordingFetcher{items: []Item{{ID: "s1"}}}
	items, err = Listing(rf, d, "bbs")
	if err != nil || len(items) != 1 || rf.calls[0] != "script https://old.reddit.com/r/bbs/new/" {
		t.Errorf("script: items %+v, err %v, calls %v", items, err, rf.calls)
	}
}

func TestThreadParentsFromScript(t *testing.T) {
	d := redditDef(t)
	d.Thread = &Extraction{Script: "…"}
	rf := &recordingFetcher{items: []Item{{ID: "root"}, {ID: "a"}, {ID: "b", ParentID: "a"}}}
	root, replies, err := Thread(rf, d, "https://example.com/t")
	if err != nil {
		t.Fatal(err)
	}
	if root.ID != "root" || len(replies) != 2 || replies[0].ParentID != "root" || replies[1].ParentID != "a" {
		t.Errorf("root %+v replies %+v", root, replies)
	}
	if _, _, err := Thread(&recordingFetcher{}, d, "https://example.com/t"); err == nil {
		t.Error("a script returning no items gave a thread")
	}
}

// TestBrowserScript runs a script in a real headless Chrome. It is skipped
// where no Chrome is installed.
func TestBrowserScript(t *testing.T) {
	chrome := findChrome()
	if chrome == "" {
		t.Skip("no Chrome or Chromium installed")
	}
	alloc, cancel := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(chrome))...)
	t.Cleanup(cancel)
	f, err := newBrowserFetcher(alloc, func() {}, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.Close)

	page := "data:text/html," + url.PathEscape(`<main><article id="a1" data-at="2026-10-22T05:00:00Z">One</article><article id="a2" data-at="2026-10-22T06:00:00Z">Two</article></main>`)
	d := Definition{Name: "test", Fetch: "browser", Ready: "main", Content: "main"}
	items, err := f.Script(page, d, `return [...document.querySelectorAll("article")].map(a => ({id: a.id, title: a.textContent, posted_at: a.dataset.at}))`)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 22, 6, 0, 0, 0, time.UTC)
	if len(items) != 2 || items[1].ID != "a2" || items[1].Title != "Two" || !items[1].PostedAt.Equal(want) {
		t.Errorf("items = %+v", items)
	}
	html, err := f.Page(page, d)
	if err != nil || !strings.Contains(html, `id="a1"`) {
		t.Errorf("Page = %.80q, %v", html, err)
	}
	if _, err := f.Script(page, d, `throw new Error("boom")`); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("a throwing script gave err = %v", err)
	}
}

func findChrome() string {
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "chrome",
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// TestBrowserFetcherAttachesAndClosesItsTab starts a headless Chrome with a
// debugging port, as a sysop's Chrome runs, and checks the fetcher opens its
// own tab there and closes it again. It is skipped where no Chrome is
// installed.
func TestBrowserFetcherAttachesAndClosesItsTab(t *testing.T) {
	chrome := findChrome()
	if chrome == "" {
		t.Skip("no Chrome or Chromium installed")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	cmd := exec.Command(chrome, "--headless=new", "--no-first-run", "--disable-gpu",
		fmt.Sprintf("--remote-debugging-port=%d", port), "--user-data-dir="+chromeProfile(t), "about:blank")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	debugURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	pages := func() int {
		resp, err := http.Get(debugURL + "/json/list")
		if err != nil {
			return -1
		}
		defer func() { _ = resp.Body.Close() }()
		var ts []struct{ Type string }
		_ = json.NewDecoder(resp.Body).Decode(&ts)
		n := 0
		for _, tg := range ts {
			if tg.Type == "page" {
				n++
			}
		}
		return n
	}
	for i := 0; pages() < 0 && i < 50; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	before := pages()

	f, err := NewBrowserFetcher(debugURL, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got := pages(); got != before+1 {
		t.Errorf("pages open after attaching = %d, want %d", got, before+1)
	}
	d := Definition{Name: "test", Fetch: "browser", Ready: "main", Content: "main"}
	if html, err := f.Page("data:text/html,"+url.PathEscape(`<main id="m">hi</main>`), d); err != nil || !strings.Contains(html, `id="m"`) {
		t.Errorf("Page = %.80q, %v", html, err)
	}
	f.Close()
	for i := 0; pages() != before && i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if got := pages(); got != before {
		t.Errorf("pages open after Close = %d, want %d: the fetcher left its tab", got, before)
	}
}

// A Chrome with every window closed refuses a background tab ("no browser
// is open"); the fetcher must still get a tab. Skipped where no Chrome is
// installed.
func TestBrowserFetcherWithNoWindowOpen(t *testing.T) {
	chrome := findChrome()
	if chrome == "" {
		t.Skip("no Chrome or Chromium installed")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	cmd := exec.Command(chrome, "--headless=new", "--no-first-run", "--disable-gpu",
		fmt.Sprintf("--remote-debugging-port=%d", port), "--user-data-dir="+chromeProfile(t), "about:blank")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	debugURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	var targets []struct{ ID, Type string }
	for i := 0; i < 50; i++ {
		if resp, err := http.Get(debugURL + "/json/list"); err == nil {
			_ = json.NewDecoder(resp.Body).Decode(&targets)
			_ = resp.Body.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, tg := range targets {
		if tg.Type == "page" {
			if resp, err := http.Get(debugURL + "/json/close/" + tg.ID); err == nil {
				_ = resp.Body.Close()
			}
		}
	}
	time.Sleep(300 * time.Millisecond)

	f, err := NewBrowserFetcher(debugURL, 20*time.Second)
	if err != nil {
		t.Fatalf("attaching to a Chrome with no window: %v", err)
	}
	defer f.Close()
	d := Definition{Name: "test", Fetch: "browser", Ready: "main", Content: "main"}
	if html, err := f.Page("data:text/html,"+url.PathEscape(`<main id="m">hi</main>`), d); err != nil || !strings.Contains(html, `id="m"`) {
		t.Errorf("Page = %.80q, %v", html, err)
	}
}

// Script items get the same ID checks as selector items: an empty ID would
// become the MSGID "@<suffix>" and swallow every later empty-ID item.
func TestScriptItemsAreChecked(t *testing.T) {
	d := Definition{Name: "news", Fetch: "browser", Threads: true, ListingURL: "https://news.example/{feed}/",
		Listing: Extraction{Script: "x", IDPattern: "^s"}, Thread: &Extraction{Script: "y", IDPattern: "^r"},
		Message: MessageShape{MsgIDSuffix: "news"}}
	rf := &recordingFetcher{items: []Item{{ID: ""}, {ID: "x1"}, {ID: "s1"}}}
	items, err := Listing(rf, d, "tech")
	if err != nil || len(items) != 1 || items[0].ID != "s1" {
		t.Errorf("listing = %+v, %v; want only s1", items, err)
	}
	rf.items = []Item{{ID: "s1"}, {ID: ""}, {ID: "r1"}, {ID: "zz"}}
	root, replies, err := Thread(rf, d, "https://news.example/s1")
	if err != nil || root.ID != "s1" || len(replies) != 1 || replies[0].ID != "r1" {
		t.Errorf("thread = %+v %+v, %v; want root s1 and reply r1", root, replies, err)
	}
	rf.items = []Item{{ID: ""}, {ID: "r1"}}
	if _, _, err := Thread(rf, d, "https://news.example/s1"); err == nil {
		t.Error("a thread whose root has no ID was accepted")
	}
}

// One script item with a missing or odd posted_at gets a zero time; it does
// not fail the whole page.
func TestDecodeScriptItemsTimes(t *testing.T) {
	items, err := decodeScriptItems(`[{"id":"a","posted_at":""},{"id":"b","posted_at":"yesterday"},{"id":"c","posted_at":"2026-10-22T06:00:00Z"},{"id":"d"}]`)
	if err != nil || len(items) != 4 {
		t.Fatalf("items = %+v, %v", items, err)
	}
	if !items[0].PostedAt.IsZero() || !items[1].PostedAt.IsZero() || !items[3].PostedAt.IsZero() ||
		!items[2].PostedAt.Equal(time.Date(2026, 10, 22, 6, 0, 0, 0, time.UTC)) {
		t.Errorf("times = %v %v %v %v", items[0].PostedAt, items[1].PostedAt, items[2].PostedAt, items[3].PostedAt)
	}
	if _, err := decodeScriptItems(`{"id":"a"}`); err == nil {
		t.Error("an object instead of a list was accepted")
	}
}

// chromeProfile is an empty profile directory for a test's Chrome, removed
// when the test ends. It is not t.TempDir: Chrome's helper processes outlive
// the kill of the browser by a moment and still write to the profile, and
// TempDir fails the test when its RemoveAll finds the directory non-empty.
// Call it before registering the cleanup that kills Chrome, so the removal
// runs after the kill.
func chromeProfile(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "chrome-profile")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		var err error
		for i := 0; i < 20; i++ {
			if err = os.RemoveAll(dir); err == nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Logf("left Chrome profile %s behind: %v", dir, err)
	})
	return dir
}
