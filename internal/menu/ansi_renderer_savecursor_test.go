package menu

import (
	"strings"
	"testing"
)

// plainRow returns row n of the rendered buffer with styles and trailing
// blanks removed.
func plainRow(lines []string, n int) string {
	if n >= len(lines) {
		return ""
	}
	return strings.TrimRight(reWrapEsc.ReplaceAllString(lines[n], ""), " ")
}

// ESC[s / ESC[u were no-ops, so every restore left the cursor wherever the
// last write had put it and the text that followed landed in the wrong place.
// Art in the fsxNet Ads + ANSI Art echo leans on them heavily.
func TestANSIRendererRestoresSavedCursor(t *testing.T) {
	// Park at 5,10, save, wander off and write, restore, write again.
	art := "\x1b[5;10HANCHOR\x1b[s\x1b[20;40HELSEWHERE\x1b[uBACK"
	lines := RenderANSIArtToLines(art, 79, 30)

	if got := plainRow(lines, 4); got != strings.Repeat(" ", 9)+"ANCHORBACK" {
		t.Errorf("restored write landed wrong:\n got  %q\n want %q",
			got, strings.Repeat(" ", 9)+"ANCHORBACK")
	}
	if got := plainRow(lines, 19); got != strings.Repeat(" ", 39)+"ELSEWHERE" {
		t.Errorf("intervening write disturbed:\n got %q", got)
	}
}

// A save survives being restored: the mark stays put, so two restores both
// return to it and the second write lands on top of the first.
func TestANSIRendererRestoreIsRepeatable(t *testing.T) {
	art := "\x1b[3;5HX\x1b[s\x1b[10;10HY\x1b[uA\x1b[12;12HZ\x1b[uB"
	lines := RenderANSIArtToLines(art, 79, 30)

	// X at col 5, then A written at the mark, then B over the top of A.
	if got, want := plainRow(lines, 2), strings.Repeat(" ", 4)+"XB"; got != want {
		t.Errorf("repeat restore landed wrong:\n got  %q\n want %q", got, want)
	}
	if got, want := plainRow(lines, 9), strings.Repeat(" ", 9)+"Y"; got != want {
		t.Errorf("first detour disturbed:\n got %q", got)
	}
	if got, want := plainRow(lines, 11), strings.Repeat(" ", 11)+"Z"; got != want {
		t.Errorf("second detour disturbed:\n got %q", got)
	}
}

// A restore with nothing saved must not fling the cursor to the origin.
func TestANSIRendererRestoreWithoutSaveIsANoop(t *testing.T) {
	art := "\x1b[4;4HKEEP\x1b[uPUT"
	lines := RenderANSIArtToLines(art, 79, 30)

	if got := plainRow(lines, 3); got != strings.Repeat(" ", 3)+"KEEPPUT" {
		t.Errorf("bare restore moved the cursor: %q", got)
	}
	if got := plainRow(lines, 0); got != "" {
		t.Errorf("text was flung to the origin: %q", got)
	}
}

// Saving must not itself move the cursor.
func TestANSIRendererSaveDoesNotMoveTheCursor(t *testing.T) {
	withSave := RenderANSIArtToLines("\x1b[2;3HAB\x1b[sCD", 79, 10)
	without := RenderANSIArtToLines("\x1b[2;3HABCD", 79, 10)

	if a, b := plainRow(withSave, 1), plainRow(without, 1); a != b {
		t.Errorf("ESC[s changed the output:\n with %q\n without %q", a, b)
	}
}
