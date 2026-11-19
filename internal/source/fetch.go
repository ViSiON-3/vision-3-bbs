package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/andybalholm/cascadia"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"golang.org/x/net/html"

	"github.com/ViSiON-3/vision-3-bbs/internal/version"
)

// Fetcher loads pages for a source.
type Fetcher interface {
	// Page loads url and returns the HTML of the definition's content
	// element, and nothing else from the page.
	Page(url string, d Definition) (string, error)
	// Script loads url, runs script in the page and decodes the []Item it
	// returns. The script is the body of an async function.
	Script(url string, d Definition, script string) ([]Item, error)
	Close()
}

// Listing reads a feed's listing page with the definition's selectors or
// script.
func Listing(f Fetcher, d Definition, feed string) ([]Item, error) {
	u := d.ListingURLFor(feed)
	if d.Listing.Script != "" {
		items, err := f.Script(u, d, d.Listing.Script)
		return checkedItems(items, d.Listing.IDPattern), err
	}
	page, err := f.Page(u, d)
	if err != nil {
		return nil, err
	}
	return ExtractListing(page, d)
}

// Thread reads a thread page. A thread script returns the root first and
// the replies after it in page order; a reply with no parent_id is a reply
// to the root.
func Thread(f Fetcher, d Definition, url string) (Item, []Item, error) {
	if d.Thread == nil {
		return Item{}, nil, fmt.Errorf("source %s has no thread extraction", d.Name)
	}
	if d.Thread.Format == "json" {
		fetcher, ok := f.(interface{ JSON(string) (any, error) })
		if !ok {
			return Item{}, nil, errors.New("fetcher does not support JSON details")
		}
		data, err := fetcher.JSON(url)
		if err != nil {
			return Item{}, nil, err
		}
		root, err := extractJSONRoot(data, d)
		return root, nil, err
	}
	if d.Thread.Script == "" {
		page, err := f.Page(url, d)
		if err != nil {
			return Item{}, nil, err
		}
		return ExtractThread(page, d)
	}
	items, err := f.Script(url, d, d.Thread.Script)
	if err != nil {
		return Item{}, nil, err
	}
	if len(items) == 0 {
		return Item{}, nil, fmt.Errorf("loading %s: the thread script returned no items", url)
	}
	root := items[0]
	if root.ID == "" {
		return Item{}, nil, fmt.Errorf("loading %s: the thread script's first item, the thread itself, has no id", url)
	}
	replies := checkedItems(items[1:], d.Thread.IDPattern)
	for i := range replies {
		if replies[i].ParentID == "" {
			replies[i].ParentID = root.ID
		}
	}
	return root, replies, nil
}

// checkedItems drops script items with no ID or one that doesn't match
// pattern, as selector extraction does: an empty ID would become the MSGID
// "@<suffix>" and hide every later item without one.
func checkedItems(items []Item, pattern string) []Item {
	var re *regexp.Regexp
	if pattern != "" {
		re = regexp.MustCompile(pattern) // compiled when the definition loaded
	}
	var kept []Item
	for _, it := range items {
		if it.ID != "" && (re == nil || re.MatchString(it.ID)) {
			kept = append(kept, it)
		}
	}
	return kept
}

// BrowserFetcher drives one tab in a Chrome that the sysop started with
// remote debugging and logged into the site. Sites that refuse automated
// clients accept that session; they challenge a Chrome a program launches.
//
// The tab is opened in the background, so loading pages does not bring a
// minimized Chrome window back up: the sysop can keep that Chrome out of
// sight.
type BrowserFetcher struct {
	ctx         context.Context
	cancelTab   context.CancelFunc
	cancelAlloc context.CancelFunc
	// closeTab closes a tab the fetcher opened through its own browser
	// connection; nil when chromedp opened the tab itself.
	closeTab func()
	timeout  time.Duration
}

// NewBrowserFetcher attaches to the Chrome at debugURL and opens a
// background tab.
func NewBrowserFetcher(debugURL string, timeout time.Duration) (*BrowserFetcher, error) {
	fail := func(err error) error {
		return fmt.Errorf("attaching to Chrome at %s (is it running, and is the SSH tunnel up?): %w", debugURL, err)
	}
	ws, err := browserWebSocketURL(debugURL)
	if err != nil {
		return nil, fail(err)
	}
	ownerCtx, cancelOwner := context.WithCancel(context.Background())
	owner, err := chromedp.NewBrowser(ownerCtx, ws)
	if err != nil {
		cancelOwner()
		return nil, fail(err)
	}
	exec := cdp.WithExecutor(ownerCtx, owner)
	tab, err := target.CreateTarget("about:blank").WithBackground(true).Do(exec)
	if err != nil {
		// With every window closed, Chrome refuses any new tab ("no browser
		// is open"), so open the tab in a window of its own. The window
		// comes forward.
		tab, err = target.CreateTarget("about:blank").WithNewWindow(true).Do(exec)
	}
	if err != nil {
		cancelOwner()
		return nil, fail(fmt.Errorf("opening a tab: %w", err))
	}
	closeTab := func() {
		ctx, cancel := context.WithTimeout(ownerCtx, 5*time.Second)
		_ = target.CloseTarget(tab).Do(cdp.WithExecutor(ctx, owner)) // a tab left open is harmless
		cancel()
		cancelOwner()
	}
	alloc, cancel := chromedp.NewRemoteAllocator(context.Background(), debugURL)
	f, err := newBrowserFetcher(alloc, cancel, timeout, chromedp.WithTargetID(tab))
	if err != nil {
		closeTab()
		return nil, fail(err)
	}
	f.closeTab = closeTab
	return f, nil
}

func newBrowserFetcher(alloc context.Context, cancelAlloc context.CancelFunc, timeout time.Duration, opts ...chromedp.ContextOption) (*BrowserFetcher, error) {
	ctx, cancelTab := chromedp.NewContext(alloc, opts...)
	if err := chromedp.Run(ctx); err != nil {
		cancelTab()
		cancelAlloc()
		return nil, err
	}
	return &BrowserFetcher{ctx: ctx, cancelTab: cancelTab, cancelAlloc: cancelAlloc, timeout: timeout}, nil
}

// browserWebSocketURL asks Chrome's debugging endpoint for its browser
// websocket.
func browserWebSocketURL(debugURL string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(strings.TrimRight(debugURL, "/") + "/json/version")
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }() // read side
	var v struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", fmt.Errorf("reading %s/json/version: %w", debugURL, err)
	}
	if v.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("%s/json/version gave no webSocketDebuggerUrl", debugURL)
	}
	return v.WebSocketDebuggerURL, nil
}

func (f *BrowserFetcher) load(ctx context.Context, url string, d Definition) error {
	ready := d.Ready
	if ready == "" {
		ready = "body"
	}
	if err := chromedp.Run(ctx, chromedp.Navigate(url), chromedp.WaitReady(ready, chromedp.ByQuery)); err != nil {
		return fmt.Errorf("loading %s: %w", url, err)
	}
	return nil
}

// Page implements Fetcher.
func (f *BrowserFetcher) Page(url string, d Definition) (string, error) {
	ctx, cancel := context.WithTimeout(f.ctx, f.timeout)
	defer cancel()
	if err := f.load(ctx, url, d); err != nil {
		return "", err
	}
	var page string
	expr := fmt.Sprintf(`(document.querySelector(%q) || {outerHTML: ""}).outerHTML`, d.Content)
	if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &page)); err != nil {
		return "", fmt.Errorf("loading %s: %w", url, err)
	}
	if page == "" {
		return "", fmt.Errorf("loading %s: no %s on the page", url, d.Content)
	}
	return page, nil
}

// Script implements Fetcher.
func (f *BrowserFetcher) Script(url string, d Definition, script string) ([]Item, error) {
	ctx, cancel := context.WithTimeout(f.ctx, f.timeout)
	defer cancel()
	if err := f.load(ctx, url, d); err != nil {
		return nil, err
	}
	// JSON.stringify keeps the result a plain string, so Go decodes posted_at
	// and the other fields itself.
	expr := "(async () => JSON.stringify(await (async () => {\n" + script + "\n})()))()"
	var out string
	err := chromedp.Run(ctx, chromedp.Evaluate(expr, &out, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
		return p.WithAwaitPromise(true)
	}))
	if err != nil {
		return nil, fmt.Errorf("running %s's script on %s: %w", d.Name, url, err)
	}
	items, err := decodeScriptItems(out)
	if err != nil {
		return nil, fmt.Errorf("%s's script on %s did not return a list of items: %w", d.Name, url, err)
	}
	return items, nil
}

// decodeScriptItems decodes the JSON a script returned. posted_at is parsed
// per item as RFC 3339; a missing or unreadable one is a zero time, as with
// selectors, rather than failing the page.
func decodeScriptItems(out string) ([]Item, error) {
	type scriptItem struct {
		Item
		PostedAt string `json:"posted_at"`
	}
	var raw []scriptItem
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, err
	}
	items := make([]Item, len(raw))
	for i, r := range raw {
		items[i] = r.Item
		items[i].PostedAt = parseTime(r.PostedAt, "rfc3339")
	}
	return items, nil
}

// Close closes the tab. Chrome itself keeps running.
func (f *BrowserFetcher) Close() {
	f.cancelTab()
	f.cancelAlloc()
	if f.closeTab != nil {
		f.closeTab()
	}
}

// HTTPFetcher loads pages with plain GET requests, for sites that need no
// browser.
type HTTPFetcher struct {
	client *http.Client
}

// NewHTTPFetcher returns a fetcher whose requests time out after timeout.
func NewHTTPFetcher(timeout time.Duration) *HTTPFetcher {
	return &HTTPFetcher{client: &http.Client{Timeout: timeout}}
}

// maxPageBytes caps a page read over HTTP.
const maxPageBytes = 16 << 20

// Page implements Fetcher.
func (f *HTTPFetcher) Page(url string, d Definition) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("loading %s: %w", url, err)
	}
	req.Header.Set("User-Agent", "ViSiON/3 "+version.Number)
	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("loading %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }() // read side
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("loading %s: status %d", url, resp.StatusCode)
	}
	doc, err := html.Parse(io.LimitReader(resp.Body, maxPageBytes))
	if err != nil {
		return "", fmt.Errorf("parsing %s: %w", url, err)
	}
	sel, err := cascadia.Compile(d.Content)
	if err != nil {
		return "", fmt.Errorf("content selector %q: %w", d.Content, err)
	}
	n := cascadia.Query(doc, sel)
	if n == nil {
		return "", fmt.Errorf("loading %s: no %s on the page", url, d.Content)
	}
	var b strings.Builder
	if err := html.Render(&b, n); err != nil {
		return "", fmt.Errorf("rendering %s: %w", url, err)
	}
	return b.String(), nil
}

// Script implements Fetcher; plain HTTP cannot run one.
func (f *HTTPFetcher) Script(string, Definition, string) ([]Item, error) {
	return nil, errors.New("scripts need fetch \"browser\"; this source fetches over plain HTTP")
}

// Close implements Fetcher.
func (f *HTTPFetcher) Close() {}
