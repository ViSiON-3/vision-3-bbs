package reddit

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
)

// Fetcher returns the content section of a rendered page.
type Fetcher interface {
	Fetch(url string) (string, error)
	Close()
}

// ChromeFetcher drives one tab in a Chrome that a person started with remote
// debugging and logged into Reddit. Reddit accepts that session; it challenges
// a Chrome that chromedp launches itself.
type ChromeFetcher struct {
	ctx         context.Context
	cancelTab   context.CancelFunc
	cancelAlloc context.CancelFunc
	sel         Selectors
	timeout     time.Duration
}

// NewChromeFetcher attaches to the Chrome at debugURL and opens a tab.
func NewChromeFetcher(debugURL string, sel Selectors, timeout time.Duration) (*ChromeFetcher, error) {
	allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(context.Background(), debugURL)
	ctx, cancelTab := chromedp.NewContext(allocCtx)
	if err := chromedp.Run(ctx); err != nil {
		cancelTab()
		cancelAlloc()
		return nil, fmt.Errorf("attaching to Chrome at %s (is it running, and is the SSH tunnel up?): %w", debugURL, err)
	}
	return &ChromeFetcher{ctx: ctx, cancelTab: cancelTab, cancelAlloc: cancelAlloc, sel: sel, timeout: timeout}, nil
}

// Fetch loads url, waits for the ready selector (old Reddit may bounce
// through its login page first), and returns the content section's HTML.
// Only that section is returned: the rest of the page carries the account's
// name and CSRF token.
func (f *ChromeFetcher) Fetch(url string) (string, error) {
	ctx, cancel := context.WithTimeout(f.ctx, f.timeout)
	defer cancel()
	var page string
	err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitReady(f.sel.ReadySelector, chromedp.ByQuery),
		chromedp.Evaluate(fmt.Sprintf(`(document.querySelector(%q) || {outerHTML: ""}).outerHTML`, f.sel.ContentSelector), &page),
	)
	if err != nil {
		return "", fmt.Errorf("loading %s: %w", url, err)
	}
	if page == "" {
		return "", fmt.Errorf("loading %s: no %s on the page", url, f.sel.ContentSelector)
	}
	return page, nil
}

// Close closes the tab. Chrome itself keeps running.
func (f *ChromeFetcher) Close() {
	f.cancelTab()
	f.cancelAlloc()
}
