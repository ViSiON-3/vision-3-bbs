package menu

import (
	"regexp"
	"testing"
)

// TestComputeVerticalLayout_NoBotTemplate pins the geometry when there is no
// BOT template: 2 rows are reserved (separator + command bar), and the file
// area starts right after the header.
func TestComputeVerticalLayout_NoBotTemplate(t *testing.T) {
	headerLines, botContent, botLineCount, reservedBottom, visibleRows, fileAreaStartRow, cmdBarRow, separatorRow :=
		computeVerticalLayout(24, []byte("Header line 1\r\nHeader line 2\r\n"), []byte{}, 5, "")

	if headerLines != 2 {
		t.Errorf("headerLines = %d, want 2", headerLines)
	}
	if botContent != "" {
		t.Errorf("botContent = %q, want empty", botContent)
	}
	if botLineCount != 0 {
		t.Errorf("botLineCount = %d, want 0", botLineCount)
	}
	if reservedBottom != 2 {
		t.Errorf("reservedBottom = %d, want 2", reservedBottom)
	}
	// visibleRows = termHeight - headerLines - reservedBottom - 1 = 24 - 2 - 2 - 1 = 19
	if visibleRows != 19 {
		t.Errorf("visibleRows = %d, want 19", visibleRows)
	}
	if fileAreaStartRow != 4 {
		t.Errorf("fileAreaStartRow = %d, want 4", fileAreaStartRow)
	}
	if cmdBarRow != 24 {
		t.Errorf("cmdBarRow = %d, want 24", cmdBarRow)
	}
	if separatorRow != 23 {
		t.Errorf("separatorRow = %d, want 23", separatorRow)
	}
}

// TestComputeVerticalLayout_WithBotTemplate pins that a non-empty BOT
// template reserves extra rows for its own line count, and that ^PAGE/
// ^TOTALPAGES placeholders are expanded before measuring it.
func TestComputeVerticalLayout_WithBotTemplate(t *testing.T) {
	headerLines, botContent, botLineCount, reservedBottom, visibleRows, fileAreaStartRow, cmdBarRow, separatorRow :=
		computeVerticalLayout(24, []byte("Header\r\n"), []byte("Page ^PAGE of ^TOTALPAGES\r\n"), 5, "")

	if headerLines != 1 {
		t.Errorf("headerLines = %d, want 1", headerLines)
	}
	if botContent != "Page ^PAGE of ^TOTALPAGES" {
		t.Errorf("botContent = %q, want %q", botContent, "Page ^PAGE of ^TOTALPAGES")
	}
	if botLineCount != 1 {
		t.Errorf("botLineCount = %d, want 1", botLineCount)
	}
	// reservedBottom = 2 (separator + cmdbar) + 1 (bot line) = 3
	if reservedBottom != 3 {
		t.Errorf("reservedBottom = %d, want 3", reservedBottom)
	}
	// visibleRows = 24 - 1 - 3 - 1 = 19
	if visibleRows != 19 {
		t.Errorf("visibleRows = %d, want 19", visibleRows)
	}
	if fileAreaStartRow != 3 {
		t.Errorf("fileAreaStartRow = %d, want 3", fileAreaStartRow)
	}
	// cmdBarRow = max(1, termHeight-botLineCount) = max(1, 24-1) = 23
	if cmdBarRow != 23 {
		t.Errorf("cmdBarRow = %d, want 23", cmdBarRow)
	}
	if separatorRow != 22 {
		t.Errorf("separatorRow = %d, want 22", separatorRow)
	}
}

// TestComputeVerticalLayout_ClampsVisibleRows pins the floor: visibleRows
// never drops below 3 even on a tiny terminal.
func TestComputeVerticalLayout_ClampsVisibleRows(t *testing.T) {
	_, _, _, _, visibleRows, _, _, _ := computeVerticalLayout(5, []byte("Header\r\n"), []byte{}, 1, "")
	if visibleRows != 3 {
		t.Errorf("visibleRows = %d, want clamped to 3", visibleRows)
	}
}

// TestComputeDescMetrics pins the fixed-width description column geometry
// derived from a mid template: descPrefixLen is the ansi/pipe-stripped
// length of the template with ^DESC blanked out, descColWidth is what's left
// of termWidth for the description, and descIndentStr pads continuation
// lines to align under the description column.
func TestComputeDescMetrics(t *testing.T) {
	ansiRe, descPrefixLen, descColWidth, descIndentStr := computeDescMetrics("^MARK^NUM ^NAME ^DATE ^SIZE ^DESC", 80)

	if ansiRe == nil {
		t.Fatal("ansiRe is nil")
	}
	if _, ok := interface{}(ansiRe).(*regexp.Regexp); !ok {
		t.Fatal("ansiRe is not a *regexp.Regexp")
	}
	// Prefix (with ^DESC blanked): " " + "  1" + " " + "            " + " " + "01/01/00" + " " + "     " + " "
	// = 1+3+1+12+1+8+1+5+1 = 33
	wantPrefixLen := 33
	if descPrefixLen != wantPrefixLen {
		t.Errorf("descPrefixLen = %d, want %d", descPrefixLen, wantPrefixLen)
	}
	wantColWidth := 80 - wantPrefixLen - 1
	if descColWidth != wantColWidth {
		t.Errorf("descColWidth = %d, want %d", descColWidth, wantColWidth)
	}
	if len(descIndentStr) != descPrefixLen {
		t.Errorf("descIndentStr length = %d, want %d", len(descIndentStr), descPrefixLen)
	}
}

// TestComputeDescMetrics_ClampsNarrowTerminal pins the 20-column floor for
// descColWidth on a terminal too narrow for the fixed-width prefix.
func TestComputeDescMetrics_ClampsNarrowTerminal(t *testing.T) {
	_, _, descColWidth, _ := computeDescMetrics("^MARK^NUM ^NAME ^DATE ^SIZE ^DESC", 10)
	if descColWidth != 20 {
		t.Errorf("descColWidth = %d, want clamped to 20", descColWidth)
	}
}
