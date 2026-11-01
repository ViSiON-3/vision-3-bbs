package menu

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

const quoteTestText = "This is the quoted paragraph and it runs on well past the width of " +
	"any terminal a caller is likely to be using, so the reader has to wrap it"

// quoteRows renders body as the reader does and returns the rows with escapes
// removed, so tests can read them as a caller sees them.
func quoteRows(t *testing.T, body string, width int) []string {
	t.Helper()
	formatted := formatMessageBody(body, "", false, nil)
	lines := messageBodyLines(string(ansi.ReplaceColorPipeCodes([]byte(formatted))), "", width, ansi.OutputModeCP437)
	rows := make([]string, len(lines))
	for i, line := range lines {
		rows[i] = stripEscapes(line)
	}
	return rows
}

// assertQuoteRows checks every row carries lead, stays within width, and that
// the words read back in order with none cut.
func assertQuoteRows(t *testing.T, rows []string, lead, want string, width int) {
	t.Helper()
	var words []string
	for i, row := range rows {
		if !strings.HasPrefix(row, lead) {
			t.Errorf("row %d = %q, want prefix %q", i, row, lead)
			continue
		}
		if n := utf8.RuneCountInString(row); n > width {
			t.Errorf("row %d is %d columns, want at most %d: %q", i, n, width, row)
		}
		words = append(words, strings.Fields(strings.TrimPrefix(row, lead))...)
	}
	if got := strings.Join(words, " "); got != want {
		t.Errorf("words:\n got %q\nwant %q", got, want)
	}
}

// The issue's reproduction: a long line was cut mid-word and its continuation
// rows lost the prefix.
func TestReaderWrapsLongQuoteLineUnderItsPrefix(t *testing.T) {
	for _, tc := range []struct{ name, line, lead string }{
		{"initials", "RW> " + quoteTestText, " RW> "},
		{"already indented", " RW> " + quoteTestText, " RW> "},
		{"bare marker", "> " + quoteTestText, " > "},
		{"nested", "Bu>  Sh> " + quoteTestText, " Bu> Sh> "},
		{"doubled", "Sh>> " + quoteTestText, " Sh>> "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := quoteRows(t, tc.line, 60)
			if len(rows) < 2 {
				t.Fatalf("rows = %q, want the line wrapped", rows)
			}
			assertQuoteRows(t, rows, tc.lead, quoteTestText, 60)
		})
	}
}

// Lines the quoter's editor wrapped join and rewrap to the caller's width.
func TestReaderJoinsWrappedQuoteLines(t *testing.T) {
	src := wrapAsQuote("RW> ", quoteTestText+" "+quoteTestText, 72)
	if len(src) < 3 {
		t.Fatalf("fixture = %q, want several lines", src)
	}
	rows := quoteRows(t, strings.Join(src, "\n"), 100)
	if len(rows) >= len(src) {
		t.Errorf("rows = %q, want fewer than the %d stored lines", rows, len(src))
	}
	assertQuoteRows(t, rows, " RW> ", quoteTestText+" "+quoteTestText, 100)

	// The same paragraph on a narrower screen than it was wrapped for.
	assertQuoteRows(t, quoteRows(t, strings.Join(src, "\n"), 50), " RW> ", quoteTestText+" "+quoteTestText, 50)
}

// Lines stored with the new default's leading space join the same way, and the
// reader does not add a second space.
func TestReaderJoinsIndentedQuoteLines(t *testing.T) {
	src := wrapAsQuote("RW> ", quoteTestText+" "+quoteTestText, 72)
	for i := range src {
		src[i] = " " + src[i]
	}
	rows := quoteRows(t, strings.Join(src, "\n"), 100)
	if len(rows) >= len(src) {
		t.Errorf("rows = %q, want fewer than the %d stored lines", rows, len(src))
	}
	assertQuoteRows(t, rows, " RW> ", quoteTestText+" "+quoteTestText, 100)
}

// wrapAsQuote wraps text the way a quoter's editor stores it.
func wrapAsQuote(prefix, text string, width int) []string {
	var out []string
	cur := prefix
	for i, word := range strings.Fields(text) {
		if i > 0 && len(cur)+1+len(word) > width {
			out = append(out, cur)
			cur = prefix
		} else if i > 0 {
			cur += " "
		}
		cur += word
	}
	return append(out, cur)
}

// #551's rules hold on display: nothing here is a wrapped paragraph.
func TestReaderKeepsQuoteLinesApart(t *testing.T) {
	full := "RW> This line is the widest one and sets the width of the whole quote, ok"
	for _, tc := range []struct {
		name string
		body string
		want []string
	}{
		{
			"short lines",
			"RW> short\nRW> line",
			[]string{" RW> short", " RW> line"},
		},
		{
			"poem under a long reply",
			"RW> Roses are red\nRW> Violets are blue\nRW> Sysops are tired\nRW> Echomail is too\n\n" +
				"My reply here is an ordinary wrapped line of about seventy columns wide.",
			[]string{" RW> Roses are red", " RW> Violets are blue", " RW> Sysops are tired", " RW> Echomail is too", "",
				"My reply here is an ordinary wrapped line of about seventy columns wide."},
		},
		{
			"line ended early",
			full + "\nRW>\nRW> Hi all,\nRW> the next line",
			[]string{" " + full, " RW>", " RW> Hi all,", " RW> the next line"},
		},
		{
			"list",
			full + "\nRW>\nRW> - one\nRW> - two\nRW> 3. three",
			[]string{" " + full, " RW>", " RW> - one", " RW> - two", " RW> 3. three"},
		},
		{
			"depth change",
			full + "\nRW> Sh> deeper",
			[]string{" " + full, " RW> Sh> deeper"},
		},
		{
			"blank quote line",
			full + "\nRW>\nRW> next",
			[]string{" " + full, " RW>", " RW> next"},
		},
		{
			"table keeps its spacing",
			full + "\nRW>\nRW> Node    Calls   Up\nRW> 1          10    5",
			[]string{" " + full, " RW>", " RW> Node    Calls   Up", " RW> 1          10    5"},
		},
		{
			"indented body keeps its indent",
			"RW>   indented",
			[]string{" RW>   indented"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := quoteRows(t, tc.body, 80)
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("rows:\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

// Width is columns, whichever way the body is encoded, and a raw CP437 byte
// comes through as its character.
func TestReaderQuoteHandlesCP437AndAccentedInitials(t *testing.T) {
	text := strings.ReplaceAll(quoteTestText, "paragraph", "caf\x82 paragraph")
	decoded := strings.ReplaceAll(quoteTestText, "paragraph", "café paragraph")

	for _, tc := range []struct{ name, body string }{
		{"CP437", "\x90m> " + text},
		{"UTF-8", "Ém> " + decoded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := quoteRows(t, tc.body, 60)
			for _, row := range rows {
				if !utf8.ValidString(row) || strings.ContainsRune(row, utf8.RuneError) {
					t.Errorf("row %q has a mangled byte", row)
				}
			}
			assertQuoteRows(t, rows, " Ém> ", decoded, 60)
		})
	}
}

// A double-width rune takes two columns on a UTF-8 terminal, so the rows there
// are shorter in runes than in columns.
func TestReaderQuoteMeasuresColumnsInUTF8Mode(t *testing.T) {
	body := "RW> " + strings.TrimSpace(strings.Repeat("日本語 ", 30))
	for _, line := range messageBodyLines(body, "", 40, ansi.OutputModeUTF8) {
		if w := visibleColumns(line, ansi.OutputModeUTF8); w > 40 {
			t.Errorf("row is %d columns: %q", w, line)
		}
		if !strings.HasPrefix(line, " RW> ") {
			t.Errorf("row = %q, want the prefix", line)
		}
	}
}

// A URL wider than the screen has to be cut, but each piece stays in the quote.
func TestReaderQuoteCutsAnOversizedWordUnderThePrefix(t *testing.T) {
	url := "http://example.com/" + strings.Repeat("x", 120)
	rows := quoteRows(t, "RW> see "+url+" there", 60)
	assertQuoteRows(t, rows[:1], " RW> ", "see", 60)
	var joined strings.Builder
	for i, row := range rows[1:] {
		if !strings.HasPrefix(row, " RW> ") {
			t.Fatalf("row %d = %q, want the prefix", i+1, row)
		}
		joined.WriteString(strings.TrimPrefix(row, " RW> "))
	}
	if got := joined.String(); !strings.HasPrefix(got, url) {
		t.Errorf("the URL did not survive: %q", got)
	}
}

// Every row is yellow from its first column, and a colour the text set carries
// past the prefix on the next row.
func TestReaderQuoteKeepsColourOnEveryRow(t *testing.T) {
	yellow := "\x1b[1;33m"
	red := "\x1b[0;31m"
	body := yellow + "RW> " + red + quoteTestText + "\x1b[0;37m"
	lines := messageBodyLines(body, "", 60, ansi.OutputModeCP437)
	if len(lines) < 2 {
		t.Fatalf("lines = %q, want the quote wrapped", lines)
	}
	for i, line := range lines {
		if !strings.HasPrefix(line, yellow+" RW> ") {
			t.Errorf("line %d = %q, want it to open with the quote colour and prefix", i, line)
		}
		if i > 0 && !strings.HasPrefix(line, yellow+" RW> "+red) {
			t.Errorf("line %d = %q, want the text's colour restored after the prefix", i, line)
		}
	}
	if last := lines[len(lines)-1]; !strings.HasSuffix(last, "\x1b[0;37m") {
		t.Errorf("last line = %q, want it to close with the reset", last)
	}
}

// The reader's own colouring still finds quote lines with the new leading space.
func TestFormatMessageBodyColoursIndentedQuotes(t *testing.T) {
	got := formatMessageBody("Hello\n RW> quoted\n Ém> also\nBye", "", false, nil)
	want := "Hello\n\n|14 RW> quoted|07\n|14 Ém> also|07\n\nBye"
	if got != want {
		t.Errorf("formatted:\n got %q\nwant %q", got, want)
	}
}

// Everything that is not a quote body goes through as it was.
func TestReaderLeavesNonQuotesAlone(t *testing.T) {
	art := "RW> \x1b[5;10Hart that positions itself\x1b[1;1H" + quoteTestText
	for _, line := range messageBodyLines(art, "", 60, ansi.OutputModeCP437) {
		if strings.HasPrefix(stripEscapes(line), " RW>") {
			t.Errorf("art was reflowed: %q", line)
		}
	}

	for _, line := range []string{
		"--- Mystic BBS v1.12",
		" * Origin: Some Board> with a name (1:2/3)",
		"-> an arrow, not a quote",
		"a>b is not a quote",
	} {
		if got := reflowQuotes(line, 60, ansi.OutputModeCP437, nil); got != line {
			t.Errorf("reflowQuotes(%q) = %q, want it unchanged", line, got)
		}
	}

	// Colour-only art whose row happens to look like initials.
	block := "░▒▓> ████  ████"
	if got := reflowQuotes(block, 60, ansi.OutputModeUTF8, nil); got != block {
		t.Errorf("reflowQuotes(%q) = %q, want it unchanged", block, got)
	}
}

// A board may set a banner that looks just like a quote line.
func TestReaderLeavesAQuoteShapedBannerAlone(t *testing.T) {
	banners := newQuoteBannerMatcher(&config.StringsConfig{QuoteTop: "^N> said:"})
	body := "Buck> said:\nRW> quoted"
	got := messageBodyLinesWithBanners(body, "", 60, ansi.OutputModeCP437, banners)
	if got[0] != "Buck> said:" || got[1] != " RW> quoted" {
		t.Errorf("lines = %q", got)
	}
}

// A narrow screen under a deep prefix leaves no room to wrap into; the line
// falls back to being cut, which at least keeps every column on screen.
func TestReaderQuoteFallsBackWithNoRoomBesidePrefix(t *testing.T) {
	line := "Aa> Bb> Cc> Dd> Ee> " + quoteTestText
	for _, got := range messageBodyLines(line, "", 20, ansi.OutputModeCP437) {
		if w := visibleColumns(got, ansi.OutputModeCP437); w > 20 {
			t.Errorf("row is %d columns: %q", w, got)
		}
	}
}

func TestIsQuoteLineAgreesWithTheEditor(t *testing.T) {
	for line, want := range map[string]bool{
		"RW> text":     true,
		" RW> text":    true,
		"Ém> text":     true,
		"\x90m> text":  true,
		"Sh>> text":    true,
		"Bu> Sh> text": true,
		"> text":       true,
		">text":        true, // Usenet style, coloured as a quote as it always was
		"RW>":          true,
		"a>b text":     false,
		"-> arrow":     false,
		"plain text":   false,
	} {
		if got := isQuoteLine(line); got != want {
			t.Errorf("isQuoteLine(%q) = %v, want %v", line, got, want)
		}
	}
}
