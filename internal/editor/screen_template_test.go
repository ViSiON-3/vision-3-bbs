package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
)

// writeMenuSet creates a menu set holding the given ansi/ files and returns
// its path.
func writeMenuSet(t *testing.T, files map[string]string) string {
	t.Helper()
	menuSet := t.TempDir()
	ansiDir := filepath.Join(menuSet, "ansi")
	if err := os.MkdirAll(ansiDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(ansiDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return menuSet
}

// writeFooterTemplate writes an FSEDITORF.ANS whose second row is tagline. The
// first row of the file is never shown: Screen builds that row itself.
func writeFooterTemplate(t *testing.T, tagline string) string {
	t.Helper()
	return writeMenuSet(t, map[string]string{"FSEDITORF.ANS": "row one is rebuilt\r\n" + tagline})
}

// infoBarTemplate is a header in the shape of the shipped FSEDITOR.ANS: two
// field rows, then the info bar with the message number, the conference/area
// span and the node number, then the |#N marker giving the first editing row.
// The 30 dashes after @Z@ make the middle span 33 columns wide.
const infoBarTemplate = "To: @S#####@ From: @F#####@\r\n" +
	"Subj: @E##########@ [@I@]\r\n" +
	"[@#@]@Z@" + "------------------------------" + "#Node:@K@]\r\n" +
	"|#5"

func TestLoadHeaderTemplateRendersFieldsAndInfoBar(t *testing.T) {
	menuSet := writeMenuSet(t, map[string]string{"FSEDITOR.ANS": infoBarTemplate})
	tt := testterm.New(80, 24)
	s := NewScreen(tt, ansi.OutputModeUTF8, 80, 24)
	s.nodeNumber = 7
	s.nextMsgNum = 42
	s.confArea = "Local > General"

	if err := s.LoadHeaderTemplate(menuSet, "Hello", "bob", "alice", false); err != nil {
		t.Fatalf("LoadHeaderTemplate: %v", err)
	}
	s.DisplayHeader()

	if got, want := tt.Row(1), "To: bob      From: alice"; got != want {
		t.Errorf("Row(1) = %q, want %q", got, want)
	}
	if got, want := tt.Row(2), "Subj: Hello         [   ]"; got != want {
		t.Errorf("Row(2) = %q, want %q — the mode indicator stays blank until the first status update", got, want)
	}
	// Numbers are right-justified in their three columns over a shaded pad,
	// and the conference/area name is centred in a box across the middle span.
	if got, want := tt.Row(3), "[░42]───────▌ Local > General ▐───────#Node:░░7]"; got != want {
		t.Errorf("Row(3) = %q, want %q", got, want)
	}
	// The |#5 marker is an instruction, not art.
	if got := tt.Row(4); got != "" {
		t.Errorf("Row(4) = %q, want the |#5 geometry marker stripped", got)
	}
	if got := s.GetEditingStartY(); got != 5 {
		t.Errorf("GetEditingStartY() = %d, want 5 from the |#5 marker", got)
	}
	if got := s.GetScreenLines(); got != 18 {
		t.Errorf("GetScreenLines() = %d, want 18 (rows 5-22)", got)
	}
	if got := tt.Unhandled(); len(got) != 0 {
		t.Errorf("Unhandled() = %q, want empty", got)
	}
}

// colorizeConfAreaText's scheme, as it lands on screen: the first letter of a
// word light grey, the rest bright white, punctuation bright blue.
func TestInfoBarColoursConferenceAndArea(t *testing.T) {
	menuSet := writeMenuSet(t, map[string]string{"FSEDITOR.ANS": infoBarTemplate})
	tt := testterm.New(80, 24)
	s := NewScreen(tt, ansi.OutputModeUTF8, 80, 24)
	s.confArea = "Local > General"
	if err := s.LoadHeaderTemplate(menuSet, "s", "r", "f", false); err != nil {
		t.Fatalf("LoadHeaderTemplate: %v", err)
	}
	s.DisplayHeader()

	row := tt.Row(3)
	col := strings.Index(row, "Local")
	if col < 0 {
		t.Fatalf("Row(3) = %q, no conference name", row)
	}
	col = len([]rune(row[:col])) + 1 // byte offset -> 1-based column

	if c := tt.Cell(3, col); c.Rune != 'L' || c.Fg != 37 || c.Bold {
		t.Errorf("first letter = %+v, want 'L' in light grey (|07)", c)
	}
	if c := tt.Cell(3, col+1); c.Rune != 'o' || c.Fg != 37 || !c.Bold {
		t.Errorf("second letter = %+v, want 'o' in bright white (|15)", c)
	}
	if c := tt.Cell(3, col+6); c.Rune != '>' || c.Fg != 34 || !c.Bold {
		t.Errorf("separator = %+v, want '>' in bright blue (|09)", c)
	}
	if c := tt.Cell(3, col+8); c.Rune != 'G' || c.Fg != 37 || c.Bold {
		t.Errorf("first letter of the second word = %+v, want 'G' in light grey (|07)", c)
	}
}

// A message or node number too long for its three columns keeps its low digits.
func TestInfoBarNumbersKeepLastThreeDigits(t *testing.T) {
	menuSet := writeMenuSet(t, map[string]string{"FSEDITOR.ANS": infoBarTemplate})
	tt := testterm.New(80, 24)
	s := NewScreen(tt, ansi.OutputModeUTF8, 80, 24)
	s.nodeNumber = 123
	s.nextMsgNum = 98765
	if err := s.LoadHeaderTemplate(menuSet, "s", "r", "f", false); err != nil {
		t.Fatalf("LoadHeaderTemplate: %v", err)
	}
	s.DisplayHeader()

	// With no conference/area the middle span is a plain rule.
	want := "[765]" + strings.Repeat("─", 33) + "#Node:123]"
	if got := tt.Row(3); got != want {
		t.Errorf("Row(3) = %q, want %q", got, want)
	}
}

// Drawing the info bar must put the cursor back, since it runs in the middle
// of a full redraw.
func TestInfoBarRestoresCursor(t *testing.T) {
	menuSet := writeMenuSet(t, map[string]string{"FSEDITOR.ANS": infoBarTemplate})
	tt := testterm.New(80, 24)
	s := NewScreen(tt, ansi.OutputModeUTF8, 80, 24)
	if err := s.LoadHeaderTemplate(menuSet, "s", "r", "f", false); err != nil {
		t.Fatalf("LoadHeaderTemplate: %v", err)
	}

	s.GoXY(12, 9)
	s.renderInfoRow()

	if row, col := tt.Cursor(); row != 9 || col != 12 {
		t.Errorf("Cursor() = (%d,%d), want (9,12)", row, col)
	}
}

func TestBuildCenteredSection(t *testing.T) {
	for _, tc := range []struct {
		name, confArea string
		width          int
		want           string
	}{
		{"no width", "Local", 0, ""},
		{"no name is a plain rule", "", 5, "<B>─────"},
		{"too narrow for the box", "Local", 2, "<B>──"},
		// Widths 3 and 4 fit the box but not a character of the name (#516).
		{"box with no room for a name is a rule", "Local", 3, "<B>───"},
		{"box with no room for a name is a rule, 4", "Local", 4, "<B>────"},
		{"narrowest box shows one character", "Local", 5, "<B>▌ |07L<B> ▐"},
		{"centred with even flanks", "ab", 10, "<B>──▌ |07a|15b<B> ▐──"},
		{"odd slack goes to the right flank", "ab", 9, "<B>─▌ |07a|15b<B> ▐──"},
		{"exact fit has no flanks", "ab", 6, "<B>▌ |07a|15b<B> ▐"},
		{"long name is cut to fit", "abcdefgh", 8, "<B>▌ |07a|15bcd<B> ▐"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildCenteredSection(tc.confArea, "<B>", tc.width); got != tc.want {
				t.Errorf("buildCenteredSection(%q, %d) = %q, want %q", tc.confArea, tc.width, got, tc.want)
			}
		})
	}

	// Whatever the width, the section fills exactly that many columns.
	colourCodes := regexp.MustCompile(`<B>|\|\d\d`)
	for width := 0; width <= 40; width++ {
		for _, area := range []string{"", "a", "Local > General"} {
			got := colourCodes.ReplaceAllString(buildCenteredSection(area, "<B>", width), "")
			if n := len([]rune(got)); n != width {
				t.Errorf("buildCenteredSection(%q, %d) is %d columns wide: %q", area, width, n, got)
			}
		}
	}
}

func TestColorizeConfAreaText(t *testing.T) {
	for in, want := range map[string]string{
		"":          "",
		"a":         "|07a",
		"Local":     "|07L|15ocal",
		"fsx Gen":   "|07f|15sx |07G|15en",
		"A > B":     "|07A |09> |07B",
		"v3.net":    "|07v|153|09.|07n|15et",
		"C++ Users": "|07C|09+|09+ |07U|15sers",
	} {
		if got := colorizeConfAreaText(in); got != want {
			t.Errorf("colorizeConfAreaText(%q) = %q, want %q", in, got, want)
		}
	}
}

// Templates written before the @CODE@ placeholders use |X codes instead.
func TestLoadHeaderTemplateLegacyFormat(t *testing.T) {
	menuSet := writeMenuSet(t, map[string]string{
		"FSEDITOR.ANS": "To: |S\r\nSubject: |E\r\nAnon: |A Mode: [|I]\r\n|#4|=15;",
	})

	for _, tc := range []struct {
		name     string
		isAnon   bool
		wantAnon string
	}{
		{"named", false, "Anon: No  Mode: [   ]"},
		{"anonymous", true, "Anon: Yes Mode: [   ]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tt := testterm.New(80, 24)
			s := NewScreen(tt, ansi.OutputModeUTF8, 80, 24)
			if err := s.LoadHeaderTemplate(menuSet, "Hello", "bob", "alice", tc.isAnon); err != nil {
				t.Fatalf("LoadHeaderTemplate: %v", err)
			}
			s.DisplayHeader()

			if got := tt.Row(1); got != "To: bob" {
				t.Errorf("Row(1) = %q, want %q", got, "To: bob")
			}
			if got := tt.Row(2); got != "Subject: Hello" {
				t.Errorf("Row(2) = %q, want %q", got, "Subject: Hello")
			}
			if got := tt.Row(3); got != tc.wantAnon {
				t.Errorf("Row(3) = %q, want %q", got, tc.wantAnon)
			}
			if got := tt.Row(4); got != "" {
				t.Errorf("Row(4) = %q, want the |#4 and |=15; markers stripped", got)
			}
			if got := s.GetEditingStartY(); got != 4 {
				t.Errorf("GetEditingStartY() = %d, want 4 from the |#4 marker", got)
			}

			// A legacy template has no tracked indicator position, so a status
			// update must not write anywhere.
			before := tt.Snapshot()
			s.DisplayStatusLine(false, 1, 1)
			if got := tt.Snapshot(); got != before {
				t.Errorf("status update drew on a legacy template:\n%s", got)
			}
		})
	}
}

// A |#N marker that would leave fewer than five editing rows is ignored
// rather than producing an unusable editor.
func TestGeometryMarkerIgnoredWhenItLeavesTooFewRows(t *testing.T) {
	for marker, wantStart := range map[string]int{
		"|#12": 12, // leaves 11 rows
		"|#18": 18, // leaves exactly 5
		"|#19": 7,  // would leave 4
		"|#24": 7,  // not above the last row
		"|#0":  7,
		"|#x":  7,
	} {
		menuSet := writeMenuSet(t, map[string]string{"FSEDITOR.ANS": "Subj: @E@\r\n" + marker})
		s := NewScreen(testterm.New(80, 24), ansi.OutputModeUTF8, 80, 24)
		if err := s.LoadHeaderTemplate(menuSet, "s", "r", "f", false); err != nil {
			t.Fatalf("LoadHeaderTemplate: %v", err)
		}
		if got := s.GetEditingStartY(); got != wantStart {
			t.Errorf("marker %q: GetEditingStartY() = %d, want %d", marker, got, wantStart)
		}
	}
}

// With no FSEDITOR.ANS in the menu set the editor draws a plain header of its
// own, naming the recipient and the subject. Its colour codes are expanded,
// not printed (#515), and the rule under it fits the row without wrapping.
func TestLoadHeaderTemplateFallsBackToMinimalHeader(t *testing.T) {
	for _, width := range []int{80, 100} {
		t.Run(fmt.Sprintf("%d columns", width), func(t *testing.T) {
			tt := testterm.New(width, 24)
			s := NewScreen(tt, ansi.OutputModeUTF8, width, 24)
			if err := s.LoadHeaderTemplate(t.TempDir(), "Hello", "bob", "alice", false); err != nil {
				t.Fatalf("LoadHeaderTemplate: %v", err)
			}
			s.GoXY(1, 12)
			s.WriteDirect("stale text")
			s.DisplayHeader()

			for i, want := range []string{
				"Full Screen Message Editor",
				"To: bob",
				"Subject: Hello",
				strings.Repeat("-", 79), // header art is 80 columns wide on any terminal
				"",                      // the rule did not wrap onto this row
			} {
				if got := tt.Row(i + 1); got != want {
					t.Errorf("Row(%d) = %q, want %q", i+1, got, want)
				}
			}
			// |15 on the title, |11 on the recipient.
			if c := tt.Cell(1, 1); c.Fg != 37 || !c.Bold {
				t.Errorf("title cell = %+v, want bright white", c)
			}
			if c := tt.Cell(2, 5); c.Rune != 'b' || c.Fg != 36 || !c.Bold {
				t.Errorf("recipient cell = %+v, want 'b' in bright cyan", c)
			}
			// The minimal header clears the screen itself.
			if got := tt.Row(12); got != "" {
				t.Errorf("Row(12) = %q, want the screen cleared", got)
			}
			if got := tt.Unhandled(); len(got) != 0 {
				t.Errorf("Unhandled() = %q, want empty", got)
			}
		})
	}
}

func TestFooterRendersBoardNameAndTagline(t *testing.T) {
	menuSet := writeFooterTemplate(t, "   ViSiON/3 Edit")
	tt := testterm.New(80, 24)
	s := NewScreen(tt, ansi.OutputModeUTF8, 80, 24)
	s.boardName = "My BBS"

	if s.HasFooter() || s.PromptRow() != 24 || s.GetScreenLines() != 17 {
		t.Fatalf("before the footer loads: HasFooter=%v PromptRow=%d ScreenLines=%d, want false/24/17",
			s.HasFooter(), s.PromptRow(), s.GetScreenLines())
	}
	if err := s.LoadFooterTemplate(menuSet); err != nil {
		t.Fatalf("LoadFooterTemplate: %v", err)
	}
	s.DisplayFooter()

	// The rule between the name and the key legend stretches so the row ends
	// one column short of the right margin.
	row := tt.Row(23)
	want := " └─▌My BBS▐" + strings.Repeat("─", 34) + "▌CTRL (A)Abort (Z)Save (Q)Quote▐─┘"
	if row != want {
		t.Errorf("Row(23) = %q, want %q", row, want)
	}
	if got := len([]rune(row)); got != 79 {
		t.Errorf("footer row is %d columns, want 79", got)
	}
	if got := tt.Row(24); got != "   ViSiON/3 Edit" {
		t.Errorf("Row(24) = %q, want the template's tagline row", got)
	}
	// The footer takes two rows, so the editing area gives one up.
	if !s.HasFooter() || s.PromptRow() != 24 || s.GetScreenLines() != 15 {
		t.Errorf("after the footer loads: HasFooter=%v PromptRow=%d ScreenLines=%d, want true/24/15",
			s.HasFooter(), s.PromptRow(), s.GetScreenLines())
	}
	if got := tt.Unhandled(); len(got) != 0 {
		t.Errorf("Unhandled() = %q, want empty", got)
	}
}

func TestFooterBoardNameFitsTheRow(t *testing.T) {
	menuSet := writeFooterTemplate(t, "tagline")

	for _, tc := range []struct {
		name, boardName, wantName string
	}{
		{"unset name falls back", "", "BBS"},
		{"name that exactly fills the row", strings.Repeat("n", 36), strings.Repeat("n", 36)},
		{"over-long name is cut", strings.Repeat("n", 36) + "overflow", strings.Repeat("n", 36)},
		{"multibyte name is cut on a rune", strings.Repeat("é", 40), strings.Repeat("é", 36)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tt := testterm.New(80, 24)
			s := NewScreen(tt, ansi.OutputModeUTF8, 80, 24)
			s.boardName = tc.boardName
			if err := s.LoadFooterTemplate(menuSet); err != nil {
				t.Fatalf("LoadFooterTemplate: %v", err)
			}
			s.DisplayFooter()

			row := tt.Row(23)
			if !strings.HasPrefix(row, " └─▌"+tc.wantName+"▐") {
				t.Errorf("Row(23) = %q, want the name %q", row, tc.wantName)
			}
			if !strings.HasSuffix(row, "────▌CTRL (A)Abort (Z)Save (Q)Quote▐─┘") {
				t.Errorf("Row(23) = %q, want the key legend intact", row)
			}
			if got := len([]rune(row)); got != 79 {
				t.Errorf("footer row is %d columns, want 79", got)
			}
		})
	}
}

// Restoring the footer after a prompt must not leave the tail of the prompt
// beside a tagline that is shorter than it (#516).
func TestDisplayFooterClearsItsRows(t *testing.T) {
	menuSet := writeFooterTemplate(t, "short")
	tt := testterm.New(80, 24)
	s := NewScreen(tt, ansi.OutputModeUTF8, 80, 24)
	if err := s.LoadFooterTemplate(menuSet); err != nil {
		t.Fatalf("LoadFooterTemplate: %v", err)
	}
	s.DisplayFooter()

	// A prompt in lightbar colours over the tagline row, as the Escape menu
	// and the abort question draw.
	s.GoXY(1, 24)
	s.WriteDirect(lbSelected + " Select an Option: Save Abort Edit Help Quote ")
	s.DisplayFooter()

	if got := tt.Row(24); got != "short" {
		t.Errorf("Row(24) = %q, want only the tagline", got)
	}
	if c := tt.Cell(24, 20); c.Bg == lightbarBg {
		t.Errorf("Cell(24,20) = %+v, cleared in the prompt's background", c)
	}
	if got := tt.Row(22); got != "" {
		t.Errorf("Row(22) = %q, want the row above the footer untouched", got)
	}
}

// Resize keeps the first editing row a |#N header marker chose; without that
// the text would jump down to the default row and leave a gap under the header.
func TestResizeKeepsHeaderMarkerStartRow(t *testing.T) {
	menuSet := writeMenuSet(t, map[string]string{"FSEDITOR.ANS": infoBarTemplate})
	s := NewScreen(testterm.New(100, 30), ansi.OutputModeUTF8, 80, 24)
	if err := s.LoadHeaderTemplate(menuSet, "s", "r", "f", false); err != nil {
		t.Fatalf("LoadHeaderTemplate: %v", err)
	}
	s.Resize(100, 30)
	if got := s.GetEditingStartY(); got != 5 {
		t.Errorf("GetEditingStartY() = %d, want 5 from the |#5 marker", got)
	}
	if got := s.GetScreenLines(); got != 24 {
		t.Errorf("GetScreenLines() = %d, want 24 (rows 5-28)", got)
	}
}

// The footer is optional: a menu set without one leaves the screen alone.
func TestMissingFooterTemplateIsNotAnError(t *testing.T) {
	tt := testterm.New(80, 24)
	s := NewScreen(tt, ansi.OutputModeUTF8, 80, 24)
	if err := s.LoadFooterTemplate(t.TempDir()); err != nil {
		t.Fatalf("LoadFooterTemplate: %v", err)
	}
	s.DisplayFooter()

	if s.HasFooter() || s.GetScreenLines() != 17 {
		t.Errorf("HasFooter=%v ScreenLines=%d, want false/17", s.HasFooter(), s.GetScreenLines())
	}
	if got := tt.Snapshot(); got != "" {
		t.Errorf("DisplayFooter drew %q with no footer loaded", got)
	}
}

func TestResizeRecomputesGeometry(t *testing.T) {
	t.Run("without a footer", func(t *testing.T) {
		s := NewScreen(testterm.New(132, 50), ansi.OutputModeUTF8, 80, 24)
		s.Resize(132, 50)
		if s.GetScreenLines() != 43 || s.PromptRow() != 50 {
			t.Errorf("ScreenLines=%d PromptRow=%d, want 43/50", s.GetScreenLines(), s.PromptRow())
		}
	})
	t.Run("with a footer", func(t *testing.T) {
		s := NewScreen(testterm.New(132, 50), ansi.OutputModeUTF8, 80, 24)
		if err := s.LoadFooterTemplate(writeFooterTemplate(t, "tagline")); err != nil {
			t.Fatalf("LoadFooterTemplate: %v", err)
		}
		s.Resize(132, 50)
		if s.GetScreenLines() != 41 || s.PromptRow() != 50 {
			t.Errorf("ScreenLines=%d PromptRow=%d, want 41/50", s.GetScreenLines(), s.PromptRow())
		}
	})
	t.Run("a tiny window still gets five editing rows", func(t *testing.T) {
		s := NewScreen(testterm.New(80, 24), ansi.OutputModeUTF8, 80, 24)
		s.Resize(40, 8)
		if s.GetScreenLines() != 5 || s.PromptRow() != 12 {
			t.Errorf("ScreenLines=%d PromptRow=%d, want 5/12", s.GetScreenLines(), s.PromptRow())
		}
		if err := s.LoadFooterTemplate(writeFooterTemplate(t, "tagline")); err != nil {
			t.Fatalf("LoadFooterTemplate: %v", err)
		}
		s.Resize(40, 8)
		if s.GetScreenLines() != 5 {
			t.Errorf("ScreenLines with a footer = %d, want 5", s.GetScreenLines())
		}
	})
}

// RefreshLine skips a row whose text has not changed; a resize drops that
// cache, because the terminal no longer shows what was last drawn.
func TestRefreshLineCacheIsDroppedOnResize(t *testing.T) {
	tt := testterm.New(80, 24)
	s := NewScreen(tt, ansi.OutputModeUTF8, 80, 24)
	row := s.GetEditingStartY()

	s.RefreshLine(1, "hello", 1)
	if got := tt.Row(row); got != "hello" {
		t.Fatalf("Row(%d) = %q, want %q", row, got, "hello")
	}

	// Scribble over the row behind Screen's back.
	s.GoXY(1, row)
	s.WriteDirect("XXXXXXXX")

	s.RefreshLine(1, "hello", 1)
	if got := tt.Row(row); got != "XXXXXXXX" {
		t.Errorf("Row(%d) = %q — an unchanged line was redrawn", row, got)
	}

	s.Resize(80, 24)
	s.RefreshLine(1, "hello", 1)
	if got := tt.Row(row); got != "hello" {
		t.Errorf("Row(%d) after resize = %q, want %q redrawn", row, got, "hello")
	}
}

// Lines scrolled out of the editing area are neither drawn nor given the cursor.
func TestOffscreenLinesAreNotDrawn(t *testing.T) {
	tt := testterm.New(80, 24)
	s := NewScreen(tt, ansi.OutputModeUTF8, 80, 24)
	s.GoXY(4, 2)

	s.RefreshLine(3, "above the window", 10)
	s.RefreshLine(10+s.GetScreenLines(), "below the window", 10)
	s.Reposition(3, 1, 10)
	s.Reposition(10+s.GetScreenLines(), 1, 10)

	if got := tt.Snapshot(); got != "" {
		t.Errorf("off-screen lines were drawn:\n%s", got)
	}
	if row, col := tt.Cursor(); row != 2 || col != 4 {
		t.Errorf("Cursor() = (%d,%d), want it left at (2,4)", row, col)
	}
}
