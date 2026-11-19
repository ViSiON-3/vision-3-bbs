//go:build !windows

package menu

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
	"github.com/mattn/go-runewidth"
	"golang.org/x/crypto/bcrypt"
)

// konfigFirstRunScreen is the screen as the caller saw it before the
// script's second run, which starts by writing @@.
func konfigFirstRunScreen(t *testing.T, out string) *testterm.Term {
	t.Helper()
	first := strings.Index(out, "@@")
	second := -1
	if first >= 0 {
		second = strings.Index(out[first+2:], "@@")
	}
	if first < 0 || second < 0 {
		t.Fatalf("the script did not run twice: %q", out)
	}
	screen := testterm.New(80, 24)
	_, _ = screen.Write([]byte(out[:first+2+second]))
	return screen
}

// The script marks Location's field at column 40 of row 9; the editor's
// field is drawn there, not on the built-in form's edit row.
func TestKonfigScriptFieldPositionPlacesTheEditor(t *testing.T) {
	script := `v3.console.write("@@");
v3.console.clear();
v3.menu.items.forEach(function (it, i) { v3.console.gotoxy(1, 2 + i); v3.console.write(it.key + " " + it.label); });
v3.menu.field("H", 40, 9, 20);
v3.prompt.answer(v3.console.getkey());`
	c, ds, um := scriptedScreenCtx(t, "KONFIG", script, "H\x15North\rQ")
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	if got := reloadUser(t, um).GroupLocation; got != "North" {
		t.Fatalf("location = %q", got)
	}
	screen := konfigFirstRunScreen(t, ds.output())
	if row := screen.Row(9); len(row) < 44 || row[39:44] != "North" || row[38] != ' ' {
		t.Errorf("row 9 = %q, want the field at column 40", row)
	}
	label := c.e.Strings().KonfigLocationLabel
	for r := 10; r <= 24; r++ {
		if row := screen.Row(r); strings.Contains(row, "North") || strings.Contains(row, label+":") {
			t.Errorf("row %d = %q: the editor drew outside the field", r, row)
		}
	}
}

// konfigEveryRowBut draws on every row of the screen except skip.
const konfigEveryRowBut = `v3.console.write("@@");
v3.console.clear();
for (var r = 1; r <= 24; r++) {
  if (r === SKIP) continue;
  v3.console.gotoxy(1, r);
  v3.console.write("x" + r);
}
v3.prompt.answer(v3.console.getkey());`

// A script that gives a setting no field and names no prompt row has the
// screen cleared for the editor's prompt, so the prompt is on a row with
// nothing of the script's on it, whether or not the script left rows blank.
func TestKonfigScriptWithoutFieldClearsForThePrompt(t *testing.T) {
	for _, tc := range []struct {
		name string
		skip string
	}{
		{"every row drawn", "0"},
		{"one row left blank", "7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := strings.Replace(konfigEveryRowBut, "SKIP", tc.skip, 1)
			c, ds, um := scriptedScreenCtx(t, "KONFIG", script, "H\x15North\rQ")
			if _, _, err := runUserKonfig(c, ""); err != nil {
				t.Fatal(err)
			}
			if got := reloadUser(t, um).GroupLocation; got != "North" {
				t.Fatalf("location = %q", got)
			}
			screen := konfigFirstRunScreen(t, ds.output())
			label := c.e.Strings().KonfigLocationLabel
			found := false
			for r := 1; r <= 24; r++ {
				row := screen.Row(r)
				if strings.HasPrefix(row, "x") {
					t.Errorf("row %d = %q: the script's screen was not cleared", r, row)
				}
				found = found || strings.Contains(row, label+": North")
			}
			if !found {
				t.Errorf("no prompt on the screen:\n%s", screen.Snapshot())
			}
		})
	}
}

// A script that names a prompt row gets the field's label there, with no
// record of the screen to find a blank row in.
func TestKonfigScriptPromptRowTakesTheLabel(t *testing.T) {
	script := strings.Replace(konfigEveryRowBut, "v3.prompt.answer", `v3.menu.field("H", 40, 9, 20);
v3.menu.promptRow(20);
v3.prompt.answer`, 1)
	script = strings.Replace(script, "SKIP", "0", 1)
	c, ds, um := scriptedScreenCtx(t, "KONFIG", script, "H\x15North\rQ")
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	if got := reloadUser(t, um).GroupLocation; got != "North" {
		t.Fatalf("location = %q", got)
	}
	screen := konfigFirstRunScreen(t, ds.output())
	label := c.e.Strings().KonfigLocationLabel
	if row := screen.Row(20); strings.TrimSpace(row) != label+":" {
		t.Errorf("row 20 = %q, want the label", row)
	}
	if row := screen.Row(9); !strings.HasPrefix(row, "x9") || len(row) < 44 || row[39:44] != "North" {
		t.Errorf("row 9 = %q, want the field at column 40", row)
	}
	for r := 1; r <= 24; r++ {
		if r != 20 && !strings.HasPrefix(screen.Row(r), "x") {
			t.Errorf("row %d = %q: the script's screen was cleared", r, screen.Row(r))
		}
	}
}

// A field given with no prompt row has the editor's label just left of it;
// a field with no room for the label there gets a cleared screen.
func TestKonfigScriptFieldWithoutPromptRow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		col     int
		cleared bool
	}{
		{"label beside the field", 40, false},
		{"no room for the label", 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := strings.Replace(konfigEveryRowBut, "v3.prompt.answer",
				fmt.Sprintf("v3.menu.field(\"H\", %d, 9, 20);\nv3.prompt.answer", tc.col), 1)
			script = strings.Replace(script, "SKIP", "0", 1)
			c, ds, um := scriptedScreenCtx(t, "KONFIG", script, "H\x15North\rQ")
			if _, _, err := runUserKonfig(c, ""); err != nil {
				t.Fatal(err)
			}
			if got := reloadUser(t, um).GroupLocation; got != "North" {
				t.Fatalf("location = %q", got)
			}
			screen := konfigFirstRunScreen(t, ds.output())
			label := c.e.Strings().KonfigLocationLabel
			if tc.cleared {
				if row := screen.Row(1); strings.HasPrefix(row, "x") {
					t.Errorf("row 1 = %q: the screen was not cleared", row)
				}
				if !strings.Contains(screen.Snapshot(), label+": North") {
					t.Errorf("no prompt:\n%s", screen.Snapshot())
				}
				return
			}
			want := label + ": North"
			if row := screen.Row(9); !strings.HasPrefix(row, "x9") || !strings.HasSuffix(row, want) ||
				len(row) != tc.col-1+len("North") {
				t.Errorf("row 9 = %q, want %q ending at column %d", row, want, tc.col+4)
			}
		})
	}
}

// A field holds its text within its width in terminal cells, so a
// double-width character does not spill past it. testterm is unsure of a
// wide character's width, so the drawn field is measured in the output.
func TestKonfigScriptFieldKeepsWideTextInside(t *testing.T) {
	script := `v3.menu.field("H", 40, 9, 5);
v3.menu.promptRow(20);
v3.prompt.answer(v3.console.getkey());`
	c, ds, _ := scriptedScreenCtx(t, "KONFIG", script, "H\rQ")
	c.currentUser.GroupLocation = "界界界"
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	out := ds.output()
	start := "\x1b[9;40H" + konfigFieldStyle
	i := strings.Index(out, start)
	if i < 0 {
		t.Fatalf("no field drawn at row 9, column 40: %q", out)
	}
	field, _, _ := strings.Cut(out[i+len(start):], konfigReset)
	if w := runewidth.StringWidth(field); w != 5 || !strings.Contains(field, "界") {
		t.Errorf("field %q is %d cells, want 5 holding 界", field, w)
	}
}

// The Auto-Signature choice is shown in a field only when it fits there.
func TestKonfigScriptChoiceTooWideForFieldClears(t *testing.T) {
	script := strings.Replace(konfigEveryRowBut, "v3.prompt.answer", `v3.menu.field("F", 40, 9, 1);
v3.prompt.answer`, 1)
	script = strings.Replace(script, "SKIP", "0", 1)
	c, ds, _ := scriptedScreenCtx(t, "KONFIG", script, "F\x1bQ")
	c.currentUser.AutoSignature = "-- me"
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	screen := konfigFirstRunScreen(t, ds.output())
	choice := stripEscapes(string(ansi.ReplacePipeCodes([]byte(c.e.Strings().KonfigAutoSigChoice))))
	if !strings.Contains(screen.Snapshot(), strings.TrimSpace(choice)) {
		t.Errorf("choice prompt %q not shown:\n%s", choice, screen.Snapshot())
	}
	if row := screen.Row(1); strings.HasPrefix(row, "x") {
		t.Errorf("row 1 = %q: the screen was not cleared", row)
	}
}

// A password field beside which "Current password" was drawn shows only
// "New password" at the next step, not the old label's first letters.
func TestKonfigScriptFieldShorterLabelBlanksTheLonger(t *testing.T) {
	script := strings.Replace(konfigEveryRowBut, "v3.prompt.answer", `v3.menu.field("J", 40, 9, 20);
v3.prompt.answer`, 1)
	script = strings.Replace(script, "SKIP", "0", 1)
	c, ds, _ := scriptedScreenCtx(t, "KONFIG", script, "Jpw\r\x1bQ")
	hash, err := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	c.currentUser.PasswordHash = string(hash)
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	str := c.e.Strings()
	if len(str.KonfigPwNew) >= len(str.KonfigPwCurrent) {
		t.Skip("the new-password label is not the shorter one")
	}
	row := konfigFirstRunScreen(t, ds.output()).Row(9)
	if len(row) < 3 || strings.TrimSpace(row[2:min(39, len(row))]) != str.KonfigPwNew+":" {
		t.Errorf("row 9 = %q, want only %q left of the field", row, str.KonfigPwNew+":")
	}
}

// A field's hint is cut at the right edge rather than wrapping onto the
// script's next row.
func TestKonfigScriptHintStaysOnThePromptRow(t *testing.T) {
	script := `v3.console.write("@@");
v3.console.clear();
v3.console.gotoxy(1, 8);
v3.console.write("next row");
v3.menu.field("A", 40, 3, 5);
v3.menu.promptRow(7);
v3.prompt.answer(v3.console.getkey());`
	c, ds, _ := scriptedScreenCtx(t, "KONFIG", script, "A\x1bQ")
	c.e.Strings().KonfigRangeHint = strings.Repeat("long hint ", 12) + "%d-%d"
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	out := ds.output()
	if !strings.Contains(out, "\x1b[3;40H"+konfigFieldStyle) {
		t.Fatalf("the editor did not use the field: %q", out)
	}
	screen := konfigFirstRunScreen(t, out)
	if row := screen.Row(8); row != "next row" {
		t.Errorf("row 8 = %q: the hint wrapped onto it", row)
	}
	if row := screen.Row(7); !strings.Contains(row, "long hint") {
		t.Errorf("row 7 = %q, want the hint", row)
	}
}

func TestKonfigFieldViewKeepsClustersWhole(t *testing.T) {
	st := &konfigState{c: &cmdCtx{outputMode: ansi.OutputModeUTF8}}
	for _, tc := range []struct {
		text     string
		width    int
		mask     bool
		shown    string
		used, at int
		cur      int // 0 for the end of the text
	}{
		{text: "👩‍💻", width: 3, shown: "👩‍💻", used: 2, at: 2},
		{text: "ab👩‍💻", width: 3, shown: "👩‍💻", used: 2, at: 2},
		{text: "界界", width: 3, shown: "界", used: 2, at: 2},
		{text: "abcdef", width: 4, shown: "def", used: 3, at: 3},
		{text: "界界", width: 3, mask: true, shown: "**", used: 2, at: 2},
		// The cursor on a wide character keeps the whole character in view.
		{text: "ab界", cur: 2, width: 3, shown: "b界", used: 3, at: 1},
	} {
		text := []rune(tc.text)
		cur := len(text)
		if tc.cur > 0 {
			cur = tc.cur
		}
		_, shown, used, at := st.fieldView(text, cur, 0, tc.width, tc.mask)
		if shown != tc.shown || used != tc.used || at != tc.at {
			t.Errorf("fieldView(%q, width %d, mask %v) = %q, %d cells, cursor %d; want %q, %d, %d",
				tc.text, tc.width, tc.mask, shown, used, at, tc.shown, tc.used, tc.at)
		}
	}
}

// In a field, Right steps over a whole character, so typing after it goes
// where the cursor shows, not between a letter and its accent.
func TestKonfigFieldMovesByWholeCharacters(t *testing.T) {
	script := `v3.menu.field("H", 40, 9, 20);
v3.menu.promptRow(20);
v3.prompt.answer(v3.console.getkey());`
	c, _, um := scriptedScreenCtx(t, "KONFIG", script, "H\x1b[H\x1b[Ca\x1b[C\x7f\rQ")
	c.currentUser.GroupLocation = "éxy"
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	// Home, Right past é, a, Right past x, Backspace removes x.
	if got := reloadUser(t, um).GroupLocation; got != "éay" {
		t.Errorf("location = %q, want %q", got, "éay")
	}
}

// With no prompt row, a numeric field shows its range right of the box; if
// the range does not fit there, the screen is cleared for the prompt.
func TestKonfigScriptFieldHintBesideTheField(t *testing.T) {
	for _, tc := range []struct {
		name    string
		col     int
		cleared bool
	}{
		{"hint fits", 40, false},
		{"no room for the hint", 74, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := strings.Replace(konfigEveryRowBut, "v3.prompt.answer",
				fmt.Sprintf("v3.menu.field(\"A\", %d, 9, 5);\nv3.prompt.answer", tc.col), 1)
			script = strings.Replace(script, "SKIP", "0", 1)
			c, ds, _ := scriptedScreenCtx(t, "KONFIG", script, "A\x1bQ")
			if _, _, err := runUserKonfig(c, ""); err != nil {
				t.Fatal(err)
			}
			screen := konfigFirstRunScreen(t, ds.output())
			hint := stripEscapes(string(ansi.ReplacePipeCodes([]byte(fmt.Sprintf(c.e.Strings().KonfigRangeHint, konfigMinWidth, konfigMaxWidth)))))
			if tc.cleared {
				if row := screen.Row(1); strings.HasPrefix(row, "x") {
					t.Errorf("row 1 = %q: the screen was not cleared", row)
				}
				if !strings.Contains(screen.Snapshot(), strings.TrimSpace(hint)) {
					t.Errorf("no range hint:\n%s", screen.Snapshot())
				}
				return
			}
			if row := screen.Row(9); !strings.HasPrefix(row, "x9") || len(row) < tc.col+3 || !strings.Contains(row[tc.col+3:], strings.TrimSpace(hint)) {
				t.Errorf("row 9 = %q, want the range %q after the field", row, hint)
			}
		})
	}
}

// A field too narrow to show a character and the cursor is edited on the
// prompt row.
func TestKonfigScriptNarrowFieldEditsOnThePromptRow(t *testing.T) {
	script := `v3.console.write("@@");
v3.console.clear();
v3.menu.field("H", 40, 9, 1);
v3.menu.promptRow(20);
v3.prompt.answer(v3.console.getkey());`
	c, ds, um := scriptedScreenCtx(t, "KONFIG", script, "H\x15North\rQ")
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	if got := reloadUser(t, um).GroupLocation; got != "North" {
		t.Fatalf("location = %q", got)
	}
	screen := konfigFirstRunScreen(t, ds.output())
	if row := screen.Row(20); !strings.Contains(row, c.e.Strings().KonfigLocationLabel+": North") {
		t.Errorf("row 20 = %q, want the prompt with the text", row)
	}
	if row := screen.Row(9); strings.TrimSpace(row) != "" {
		t.Errorf("row 9 = %q, want the one-column field unused", row)
	}
}

// A switch draws no prompt, so the script's screen is not cleared for it
// even with no field, prompt row or record of the screen.
func TestKonfigScriptSwitchLeavesTheScreen(t *testing.T) {
	script := strings.Replace(konfigEveryRowBut, "SKIP", "0", 1)
	c, ds, um := scriptedScreenCtx(t, "KONFIG", script, "DQ")
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	if !reloadUser(t, um).HotKeys {
		t.Fatal("Hot Keys not switched")
	}
	if row := konfigFirstRunScreen(t, ds.output()).Row(1); !strings.HasPrefix(row, "x1") {
		t.Errorf("row 1 = %q: the screen was cleared for a switch", row)
	}
}

// A regional indicator typed before a flag joins the flag's first half;
// the cursor goes after the joined pair, so Delete removes the rest.
func TestKonfigFieldInsertKeepsTheCursorOnABoundary(t *testing.T) {
	script := `v3.menu.field("H", 40, 9, 20);
v3.menu.promptRow(20);
v3.prompt.answer(v3.console.getkey());`
	c, _, um := scriptedScreenCtx(t, "KONFIG", script, "H\x1b[H\U0001F1FD\x1b[3~\rQ")
	c.currentUser.GroupLocation = "\U0001F1FA\U0001F1F8"
	SetSessionOutputMode(c.s, ansi.OutputModeUTF8)
	t.Cleanup(func() { sessionOutputModes.Delete(c.s) })
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	if got, want := reloadUser(t, um).GroupLocation, "\U0001F1FD\U0001F1FA"; got != want {
		t.Errorf("location = %+q, want %+q", got, want)
	}
}

// On a UTF-8 terminal a field narrower than 5 columns cannot show the
// widest character and the cursor, so the editor uses the prompt row.
func TestKonfigScriptFieldTooNarrowForWideText(t *testing.T) {
	script := `v3.menu.field("H", 40, 9, 4);
v3.menu.promptRow(20);
v3.prompt.answer(v3.console.getkey());`
	c, ds, _ := scriptedScreenCtx(t, "KONFIG", script, "H\rQ")
	c.currentUser.GroupLocation = "界"
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	if out := ds.output(); strings.Contains(out, "\x1b[9;40H"+konfigFieldStyle) || !strings.Contains(out, "\x1b[20;") {
		t.Errorf("the editor used the 2-column field: %q", out)
	}
}

// The File Columns box under a script with a prompt row: a key press does
// not clear the prompt row, which can lie inside the box, and the box's
// refusal to turn off the last column is shown in the box.
func TestKonfigScriptFileColumnsBox(t *testing.T) {
	script := `v3.console.write("@@");
v3.menu.promptRow(12);
v3.prompt.answer(v3.console.getkey());`
	c, ds, _ := scriptedScreenCtx(t, "KONFIG", script, "Ln\x1bQ")
	c.currentUser.FileListColumns.Name = true
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	out := ds.output()
	first := strings.Index(out, "@@")
	second := -1
	if first >= 0 {
		second = strings.Index(out[first+2:], "@@")
	}
	if first < 0 || second < 0 {
		t.Fatalf("the script did not run twice: %q", out)
	}
	box := out[first+2 : first+2+second]
	str := c.e.Strings()
	if !strings.Contains(box, str.KonfigColumnsTitle) {
		t.Fatalf("the File Columns box was not opened: %q", box)
	}
	if strings.Contains(box, "\x1b[12;1H\x1b[2K") {
		t.Errorf("row 12 was cleared while the box was open: %q", box)
	}
	refusal := stripEscapes(string(ansi.ReplacePipeCodes([]byte(str.KonfigColumnsKeepOne))))
	// The box clips it to its width.
	if !strings.Contains(stripEscapes(box), refusal[:20]) {
		t.Errorf("the refusal %q is not shown in the box: %q", refusal, box)
	}
}

// Deleting the letter between two regional indicators joins them into a
// flag; the cursor goes to the flag's start, where the field shows it.
func TestKonfigFieldDeleteKeepsTheCursorOnABoundary(t *testing.T) {
	script := `v3.menu.field("H", 40, 9, 20);
v3.menu.promptRow(20);
v3.prompt.answer(v3.console.getkey());`
	c, _, um := scriptedScreenCtx(t, "KONFIG", script, "H\x1b[H\x1b[C\x1b[3~Z\rQ")
	c.currentUser.GroupLocation = "\U0001F1FAX\U0001F1F8"
	SetSessionOutputMode(c.s, ansi.OutputModeUTF8)
	t.Cleanup(func() { sessionOutputModes.Delete(c.s) })
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	if got, want := reloadUser(t, um).GroupLocation, "Z\U0001F1FA\U0001F1F8"; got != want {
		t.Errorf("location = %+q, want %+q", got, want)
	}
}

// A label beside the field is measured as the field measures text, so an
// emoji-presentation label ends just before the field.
func TestKonfigScriptInlineLabelMeasuredByClusters(t *testing.T) {
	script := strings.Replace(konfigEveryRowBut, "v3.prompt.answer", `v3.menu.field("H", 40, 9, 20);
v3.prompt.answer`, 1)
	script = strings.Replace(script, "SKIP", "0", 1)
	c, ds, _ := scriptedScreenCtx(t, "KONFIG", script, "H\x1bQ")
	c.e.Strings().KonfigLocationLabel = "©️©️©️©️"
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	// Four two-cell clusters and ": " end at column 39.
	if out := ds.output(); !strings.Contains(out, "\x1b[9;30H") {
		t.Errorf("label not drawn from column 30: %q", out)
	}
}

// Labels, hints and the Auto-Signature choice are cut between whole
// characters, measured as the terminal draws them.
func TestKonfigClipsByGraphemeClusters(t *testing.T) {
	st := &konfigState{c: &cmdCtx{outputMode: ansi.OutputModeUTF8}}
	heart := "\u2764\ufe0f"
	if got := st.cells(heart + "a"); got != 3 {
		t.Errorf("cells(❤️a) = %d, want 3", got)
	}
	if got, want := st.clip(heart+heart+heart, 4, ".."), heart+".."; got != want {
		t.Errorf("clip = %+q, want %+q", got, want)
	}
	if got, want := st.clipCells("\x1b[1m"+heart+heart, 3), "\x1b[1m"+heart; got != want {
		t.Errorf("clipCells = %+q, want %+q", got, want)
	}
	cp := &konfigState{c: &cmdCtx{outputMode: ansi.OutputModeCP437}}
	if got := cp.cells(heart + "a"); got != 3 {
		t.Errorf("CP437 cells(❤️a) = %d, want 3 (one per character)", got)
	}
}

// The Menu Prompt editor's live preview needs a row a script cannot name,
// so the screen is cleared for it and the preview shows on the form's help
// row.
func TestKonfigScriptCustomPromptClearsForThePreview(t *testing.T) {
	script := strings.Replace(konfigEveryRowBut, "v3.prompt.answer", `v3.menu.field("T", 40, 9, 20);
v3.menu.promptRow(20);
v3.prompt.answer`, 1)
	script = strings.Replace(script, "SKIP", "0", 1)
	c, ds, _ := scriptedScreenCtx(t, "KONFIG", script, "T\x15Hi there\rQ")
	if !c.e.customPromptAllowed(c.currentUser, c.s, c.sessionStartTime) {
		t.Skip("the test user may not set a menu prompt")
	}
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	screen := konfigFirstRunScreen(t, ds.output())
	if row := screen.Row(1); strings.HasPrefix(row, "x") {
		t.Errorf("row 1 = %q: the screen was not cleared", row)
	}
	label := c.e.Strings().KonfigCustomPromptLabel
	if !strings.Contains(screen.Snapshot(), label+": Hi there") {
		t.Errorf("no prompt:\n%s", screen.Snapshot())
	}
}

// The Auto-Signature choice drawn in a field blanks the rest of the field,
// so none of the value the script drew there shows after it.
func TestKonfigScriptChoiceInFieldBlanksTheRest(t *testing.T) {
	script := `v3.console.write("@@");
v3.console.clear();
v3.console.gotoxy(5, 9);
v3.console.write("` + strings.Repeat("Z", 75) + `");
v3.menu.field("F", 5, 9, 70);
v3.prompt.answer(v3.console.getkey());`
	c, ds, _ := scriptedScreenCtx(t, "KONFIG", script, "F\x1bQ")
	c.currentUser.AutoSignature = "-- me"
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	row := konfigFirstRunScreen(t, ds.output()).Row(9)
	choice := stripEscapes(string(ansi.ReplacePipeCodes([]byte(c.e.Strings().KonfigAutoSigChoice))))
	if !strings.HasPrefix(row, "    "+choice) {
		t.Fatalf("row 9 = %q, want the choice in the field", row)
	}
	// Only the five Zs past the field's 70 columns remain.
	if n := strings.Count(row, "Z"); n != 5 {
		t.Errorf("row 9 = %q: %d of the script's Zs show, want the 5 outside the field", row, n)
	}
}
