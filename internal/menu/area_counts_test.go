package menu

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// shippedTemplate reads a template from the v3 menu set that ships with the
// repository, so layout tests fail when the templates and the column widths
// drift apart.
func shippedTemplate(t *testing.T, name string) []byte {
	t.Helper()
	data, err := readTemplateFile(filepath.Join("..", "..", "menus", "v3", "templates", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

// renderPlain resolves pipe codes and strips the resulting ANSI so only the
// printable columns remain.
func renderPlain(b []byte) string {
	return stripAreaAnsi(string(ansi.ReplacePipeCodes(b)))
}

func TestAreaRowColumnsMatchHeader(t *testing.T) {
	top := shippedTemplate(t, "MSGAREA.TOP")
	if !bytes.Contains(top, []byte(areaColumnHeaderToken)) {
		t.Fatalf("MSGAREA.TOP is missing the %s token", areaColumnHeaderToken)
	}
	header := ""
	for _, line := range strings.Split(renderPlain(injectAreaColumnHeader(top)), "\r\n") {
		if strings.Contains(line, "Area") && strings.Contains(line, "Yours") {
			header = line
			break
		}
	}
	if header == "" {
		t.Fatal("the rendered column header row is missing")
	}

	row := renderPlain(shippedTemplate(t, "MSGAREA.MID"))
	row = strings.ReplaceAll(row, "^ID", padRight(areaNewFlag, areaGutterWidth))
	row = strings.ReplaceAll(row, "^NA", padRight("Area Name", areaNameWidth))
	row = strings.ReplaceAll(row, "^CF", padRight("Conf Name", areaConfWidth))
	row = strings.ReplaceAll(row, "^TM", ansi.PadLeft("987", areaTotalWidth))
	row = strings.ReplaceAll(row, "^NM", ansi.PadLeft("45", areaNewWidth))
	row = strings.ReplaceAll(row, "^YM", ansi.PadLeft("6", areaYoursWidth))
	row = strings.TrimRight(row, "\r\n")

	// Left-aligned columns start under their label; right-aligned counts end
	// under theirs.
	leftAligned := []struct{ label, value string }{
		{"Area", "Area Name"},
		{"Conf", "Conf Name"},
	}
	for _, c := range leftAligned {
		if got, want := strings.Index(row, c.value), strings.Index(header, c.label); got != want {
			t.Errorf("%q starts at column %d, header %q starts at %d", c.value, got, c.label, want)
		}
	}

	rightAligned := []struct{ label, value string }{
		{"Total", "987"},
		{"New", "45"},
		{"Yours", "6"},
	}
	for _, c := range rightAligned {
		got := strings.Index(row, c.value) + len(c.value)
		want := strings.Index(header, c.label) + len(c.label)
		if got != want {
			t.Errorf("%q ends at column %d, header %q ends at %d", c.value, got, c.label, want)
		}
	}

	// The NEW flag sits left of the area name.
	if got := strings.Index(row, areaNewFlag); got < 0 || got >= strings.Index(row, "Area Name") {
		t.Errorf("NEW flag at column %d, want left of the area name", got)
	}
}

func TestApplyAreaColumnTokensFormatsCounts(t *testing.T) {
	e := &MenuExecutor{} // no ConferenceMgr: the Conf column renders empty
	area := &message.MessageArea{Name: "BBS Support/Dev"}
	counts := message.AreaCounts{Total: 325, New: 4, Personal: 2}

	got := applyAreaColumnTokens("[^NA][^CF][^TM][^NM][^YM]", e, area, counts)
	want := "[" + padRight("BBS Support/Dev", areaNameWidth) + "]" +
		"[" + strings.Repeat(" ", areaConfWidth) + "]" +
		"[" + ansi.PadLeft("325", areaTotalWidth) + "]" +
		"[" + ansi.PadLeft("4", areaNewWidth) + "]" +
		"[" + ansi.PadLeft("2", areaYoursWidth) + "]"
	if got != want {
		t.Errorf("applyAreaColumnTokens:\n got %q\nwant %q", got, want)
	}
}

func TestApplyAreaColumnTokensTruncatesLongName(t *testing.T) {
	e := &MenuExecutor{}
	longName := strings.Repeat("x", areaNameWidth+10)
	got := applyAreaColumnTokens("^NA|", e, &message.MessageArea{Name: longName}, message.AreaCounts{})
	if idx := strings.Index(got, "|"); idx != areaNameWidth {
		t.Errorf("name column width = %d, want %d (%q)", idx, areaNameWidth, got)
	}
}

func TestCollectAreaCountsWithoutMessageManager(t *testing.T) {
	e := &MenuExecutor{}
	if counts := collectAreaCounts(e, []*message.MessageArea{{ID: 1}}, nil, 1); len(counts) != 0 {
		t.Errorf("no message manager: got %d entries, want 0", len(counts))
	}
}
