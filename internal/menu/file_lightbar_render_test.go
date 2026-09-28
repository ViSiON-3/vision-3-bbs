package menu

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// newRenderLightbar builds a fileLightbar over files with the layout
// runListFilesLightbar would compute for an 80x24 screen, writing to a fresh
// test session whose output the caller reads back with ts.output().
func newRenderLightbar(t *testing.T, files []file.FileRecord, top, bot string) (*fileLightbar, *testSession) {
	t.Helper()
	ts := newTestSession("")
	t.Cleanup(func() { resetSessionIH(ts) })
	const width, height = 80, 24
	mid := "^MARK^NUM ^NAME ^DATE ^SIZE ^DESC"
	headerLines, botContent, botLineCount, reservedBottom, visibleRows, startRow, cmdBarRow, sepRow :=
		computeVerticalLayout(height, []byte(top), []byte(bot), len(files), "")
	ansiRe, prefixLen, colWidth, indent := computeDescMetrics(mid, width)
	return &fileLightbar{
		terminal:             newTestTerminal(ts),
		currentUser:          &user.User{},
		outputMode:           ansi.OutputModeUTF8,
		topTemplateBytes:     []byte(top),
		processedMidTemplate: mid,
		allFiles:             files,
		cmdEntries: []cmdEntry{
			{label: "Mark", hotkey: " ", highlightColor: "\x1b[7m", regularColor: "\x1b[0m"},
			{label: "Quit", hotkey: "q", highlightColor: "\x1b[7m", regularColor: "\x1b[0m"},
		},
		hiColorSeq:       "\x1b[44m",
		termWidth:        width,
		termHeight:       height,
		headerLines:      headerLines,
		botContent:       botContent,
		botLineCount:     botLineCount,
		reservedBottom:   reservedBottom,
		visibleRows:      visibleRows,
		ansiRe:           ansiRe,
		descPrefixLen:    prefixLen,
		descColWidth:     colWidth,
		descIndentStr:    indent,
		fileAreaStartRow: startRow,
		cmdBarRow:        cmdBarRow,
		separatorRow:     sepRow,
	}, ts
}

// renderTestFiles returns n records with multi-line descriptions.
func renderTestFiles(n int) []file.FileRecord {
	files := make([]file.FileRecord, n)
	for i := range files {
		files[i] = file.FileRecord{
			ID: uuid.New(), Filename: "file" + string(rune('a'+i)) + ".zip", Size: 2048,
			UploadedAt:  fixedUploadTime,
			Description: "first line\nsecond line\nthird line",
		}
	}
	return files
}

// TestBuildFileEntryRendersColumnsAndDIZ pins buildFileEntry's row: the
// number, 12-rune name, date, size and first description line on the main
// row, the rest of the DIZ as indented continuation rows, and a '*' for a
// tagged file.
func TestBuildFileEntryRendersColumnsAndDIZ(t *testing.T) {
	files := renderTestFiles(1)
	files[0].Filename = "a-very-long-filename.zip"
	lb, _ := newRenderLightbar(t, files, "Top\r\n", "")
	lb.currentUser.TaggedFileIDs = []uuid.UUID{files[0].ID}

	lines := lb.buildFileEntry(0, false, dizMaxLines)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3 (main + 2 continuation): %q", len(lines), lines)
	}
	main := testAnsiEscape.ReplaceAllString(lines[0], "")
	for _, want := range []string{"*  1", "a-very-long-", "01/02/26", "first line"} {
		if !strings.Contains(main, want) {
			t.Errorf("main row %q missing %q", main, want)
		}
	}
	if strings.Contains(main, "a-very-long-f") {
		t.Errorf("filename not cut at 12 runes: %q", main)
	}
	if cont := testAnsiEscape.ReplaceAllString(lines[2], ""); cont != lb.descIndentStr+"third line" {
		t.Errorf("continuation = %q, want indent + %q", cont, "third line")
	}

	if got := lb.buildFileEntry(0, false, 2); len(got) != 2 {
		t.Errorf("maxLines 2 gave %d lines, want 2", len(got))
	}
	if got := lb.buildFileEntry(5, false, 3); got != nil {
		t.Errorf("out-of-range index gave %q, want nil", got)
	}
}

// TestBuildFileEntryHighlightFillsWidth pins the highlighted row: it carries
// the highlight colour, drops the template's own colours, and is padded to
// the full terminal width.
func TestBuildFileEntryHighlightFillsWidth(t *testing.T) {
	lb, _ := newRenderLightbar(t, renderTestFiles(1), "Top\r\n", "")
	row := lb.buildFileEntry(0, true, 1)[0]
	if !strings.HasPrefix(row, lb.hiColorSeq) || !strings.HasSuffix(row, "\x1b[0m") {
		t.Errorf("highlighted row not wrapped in the highlight colour: %q", row)
	}
	if n := ansi.VisibleLength(row); n != lb.termWidth {
		t.Errorf("highlighted row is %d columns, want %d", n, lb.termWidth)
	}
}

// TestRenderFullDrawsEveryRegion pins renderFull: the top template with its
// file-count placeholder, each file row, the separator, the command bar
// labels, and the bottom template's page placeholders.
func TestRenderFullDrawsEveryRegion(t *testing.T) {
	lb, ts := newRenderLightbar(t, renderTestFiles(2), "Files: |FTOTAL\r\n", "Pg ^PAGE/^TOTALPAGES\r\nbottom line")
	if err := lb.renderFull(); err != nil {
		t.Fatalf("renderFull: %v", err)
	}
	out := testAnsiEscape.ReplaceAllString(ts.output(), "")
	for _, want := range []string{"Files: 2", "filea.zip", "fileb.zip", "second line", "─", " Mark ", " Quit ", "Pg 1/1", "bottom line"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q:\n%s", want, out)
		}
	}
}

// TestRenderFileAreaEmptyAndClipped pins renderFileArea's empty-area notice,
// and that a list taller than the screen stops at the visible rows.
func TestRenderFileAreaEmptyAndClipped(t *testing.T) {
	lb, ts := newRenderLightbar(t, nil, "Top\r\n", "")
	if err := lb.renderFileArea(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ts.output(), "No files in this area.") {
		t.Errorf("want the empty notice:\n%s", ts.output())
	}

	lb, ts = newRenderLightbar(t, renderTestFiles(20), "Top\r\n", "")
	if err := lb.renderFileArea(); err != nil {
		t.Fatal(err)
	}
	out := ts.output()
	if !strings.Contains(out, "filea.zip") {
		t.Errorf("first file not drawn")
	}
	if strings.Contains(out, "filet.zip") {
		t.Errorf("20th file drawn although %d rows fit only a few 3-line entries", lb.visibleRows)
	}
}

// TestRenderPageIndicatorTracksView pins that the bottom line reports the
// page the view is scrolled to, and is skipped without a bottom template.
func TestRenderPageIndicatorTracksView(t *testing.T) {
	lb, ts := newRenderLightbar(t, renderTestFiles(20), "Top\r\n", "Page ^PAGE of ^TOTALPAGES")
	lb.selectedIndex, lb.topIndex = 19, 19
	if err := lb.renderPageIndicator(); err != nil {
		t.Fatal(err)
	}
	cur, total := lb.calculatePageInfo()
	if cur != total || total < 2 {
		t.Fatalf("calculatePageInfo = %d/%d, want last page of several", cur, total)
	}
	want := fmt.Sprintf("Page %d of %d", cur, total)
	if !strings.Contains(ts.output(), want) {
		t.Errorf("want %q:\n%q", want, ts.output())
	}

	lb.botContent = ""
	before := len(ts.output())
	if err := lb.renderPageIndicator(); err != nil || len(ts.output()) != before {
		t.Errorf("empty bottom template still wrote output (err=%v)", err)
	}
}

// TestWriteFileRowClearsUnusedHeight pins that writeFileRow blanks the rows
// of an entry's allotted height that its lines do not fill.
func TestWriteFileRowClearsUnusedHeight(t *testing.T) {
	files := renderTestFiles(1)
	files[0].Description = "only line"
	lb, ts := newRenderLightbar(t, files, "Top\r\n", "")
	if err := lb.writeFileRow(5, 0, false, 3); err != nil {
		t.Fatal(err)
	}
	out := ts.output()
	for _, row := range []string{ansi.MoveCursor(6, 1) + "\x1b[2K", ansi.MoveCursor(7, 1) + "\x1b[2K"} {
		if !strings.Contains(out, row) {
			t.Errorf("row %q not cleared:\n%q", row, out)
		}
	}
}
