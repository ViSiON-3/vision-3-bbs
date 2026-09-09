package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeNA(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.na")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestParseNAFileSeparators covers the two shapes the published area lists
// come in: a tag and description separated by whitespace alone, and the
// dash-separated form tqwNet uses. The dash is a separator, not part of the
// description — left in place it becomes the leading characters of the area
// name every user sees.
func TestParseNAFileSeparators(t *testing.T) {
	areas, err := parseNAFile(writeNA(t, strings.Join([]string{
		"% tqwNet Echo List",
		"",
		"FSX_GEN         General Chat + More..",
		"TQW_ADS         - BBS Adverts",
		"TQW_GEN     -   General Chat",
		"; a semicolon comment",
		"# a hash comment",
	}, "\n")))
	if err != nil {
		t.Fatalf("parseNAFile: %v", err)
	}
	want := []naArea{
		{Tag: "FSX_GEN", Description: "General Chat + More.."},
		{Tag: "TQW_ADS", Description: "BBS Adverts"},
		{Tag: "TQW_GEN", Description: "General Chat"},
	}
	if len(areas) != len(want) {
		t.Fatalf("got %d areas, want %d: %+v", len(areas), len(want), areas)
	}
	for i, w := range want {
		if areas[i] != w {
			t.Errorf("area %d = %+v, want %+v", i, areas[i], w)
		}
	}
}

// TestParseNAFileRejectsFileEchoList covers pointing ftnsetup at a network's
// *file* echo list, which is easy to do when both lists ship as ".na" and one
// is named for the network. Every line begins with the literal word "Area", so
// the old parser read the tag as "Area" on each and would have created that
// many junk areas without a word of complaint.
func TestParseNAFileRejectsFileEchoList(t *testing.T) {
	_, err := parseNAFile(writeNA(t, strings.Join([]string{
		"% ==================================",
		"%  tqwNet Fileecho List",
		"%",
		"Area TQW_NODE\t\t0\t!\tWeekly Nodelists",
		"Area TQW_INFO\t\t0\t!\tWeekly Infopacks",
	}, "\n")))
	if err == nil {
		t.Fatal("expected an error for a file echo list, got none")
	}
	if !strings.Contains(err.Error(), "duplicate area tag") {
		t.Errorf("error should name the duplicate tag, got: %v", err)
	}
}

// TestParseNAFileSkipsPercentComments covers "%", which the published lists
// use for their headers. A header line's first word parsed as a tag would
// otherwise become an area.
func TestParseNAFileSkipsPercentComments(t *testing.T) {
	areas, err := parseNAFile(writeNA(t, strings.Join([]string{
		"% File Echo                   Description",
		"% ------------------------------------------",
		"TQW_TEST        Test Echo",
	}, "\n")))
	if err != nil {
		t.Fatalf("parseNAFile: %v", err)
	}
	if len(areas) != 1 || areas[0].Tag != "TQW_TEST" {
		t.Errorf("got %+v, want the single TQW_TEST area", areas)
	}
}
