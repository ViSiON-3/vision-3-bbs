package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// boardBanners are a board's customised quote banners, as on the live board.
var boardBanners = &config.StringsConfig{
	QuoteTop:    "|08--- |15^N |07Said |08---",
	QuoteBottom: "|08--- |15^N |07Done |08---|07",
	QuoteTopNet: "^N wrote:",
}

// TestQuoteBlockHasNoGapsInsideBanners checks the reader does not add blank
// lines between the editor's Said/Done banners and the quoted lines, using the
// bytes the editor stores.
func TestQuoteBlockHasNoGapsInsideBanners(t *testing.T) {
	body := "\x1b[1;30m--- \x1b[1;37mJ0hnny A1pha \x1b[0;37mSaid \x1b[1;30m---\r" +
		"JA> Tom Toro!\r" +
		"JA> https://condenaststore.com/featured/the-planet-got-destroyed-tom-toro.html\r" +
		"\x1b[1;30m--- \x1b[1;37mJ0hnny A1pha \x1b[0;37mDone \x1b[1;30m---\x1b[0;37m\r" +
		"\rliterally perfect"
	got := strings.Split(formatMessageBody(body, "", false, newQuoteBannerMatcher(boardBanners)), "\n")
	if len(got) != 6 || got[1] == "" || got[3] == "" || !strings.Contains(got[3], "Done") || got[4] != "" {
		t.Fatalf("lines:\n%q", got)
	}
}

// TestQuoteBlockGapsStayForPlainText keeps the separation for quotes that run
// into ordinary text, such as FTN replies without banners.
func TestQuoteBlockGapsStayForPlainText(t *testing.T) {
	body := "On 11 Sep, kombos said the following...\n ko> Hello fsxNet!\nGot your message."
	got := strings.Split(formatMessageBody(body, "", false, newQuoteBannerMatcher(boardBanners)), "\n")
	want := []string{"On 11 Sep, kombos said the following...", "", "|14 ko> Hello fsxNet!|07", "", "Got your message."}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestQuoteBannerMatcher covers built-in, configured and networked banners.
func TestQuoteBannerMatcher(t *testing.T) {
	m := newQuoteBannerMatcher(boardBanners)
	for line, want := range map[string]bool{
		"\x1b[1;30m[\x1b[1;37mJ0hnny A1pha \x1b[0;37mSaid\x1b[1;30m]": true, // built-in default
		"--- Mickey Done ---":           true,
		"Bucko wrote:":                  true,
		"--- ViSiON/3 v0.9.4/Linux":     false, // tearline
		"I said it was done":            false,
		"":                              false,
		"Some text that says Done ---x": false,
	} {
		if got := m.match(line); got != want {
			t.Errorf("match(%q) = %v, want %v", line, got, want)
		}
	}
	if newQuoteBannerMatcher(&config.StringsConfig{QuoteTop: "^N"}).match("anything") != false {
		t.Error("a template with no literal text matched an ordinary line")
	}
	var none quoteBannerMatcher
	if none.match("--- Mickey Said ---") {
		t.Error("nil matcher matched")
	}
}

// TestPlainQuoteBannersAreNotTearlines keeps an uncoloured "--- Name Done ---"
// banner against its quote rather than spacing and dimming it as a tearline,
// while a real tearline after it still is one.
func TestPlainQuoteBannersAreNotTearlines(t *testing.T) {
	body := "--- Mickey Said ---\nMi> hello\n--- Mickey Done ---\n\nreply\n--- ViSiON/3 v0.9.4/Linux"
	got := strings.Split(formatMessageBody(body, "", false, newQuoteBannerMatcher(boardBanners)), "\n")
	want := []string{"--- Mickey Said ---", "|14Mi> hello|07", "--- Mickey Done ---", "", "reply", "", "|08--- ViSiON/3 v0.9.4/Linux|07"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

// A banner using ^F is still recognised as a banner when reading the post back.
func TestQuoteBannerMatcherWhoTo(t *testing.T) {
	m := newQuoteBannerMatcher(&config.StringsConfig{QuoteTop: "|08[|15^N |07to |15^F|08]"})
	if !m.match("[Bucko to All]") {
		t.Error("a banner with ^F did not match")
	}
}
