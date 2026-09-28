package menueditor

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

var testSGR = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// stripStyle removes lipgloss colour escapes so the text can be measured.
func stripStyle(s string) string { return testSGR.ReplaceAllString(s, "") }

// List rows are cut and padded to the box width in characters. The odd box
// width puts a byte cut inside a two-byte "é" in the last column; the "é" in
// the command row's activity shifts its bytes by one so its cut does too.
func TestListRowsCutByRune(t *testing.T) {
	const boxW = 61
	m := newTestEditor(t)
	m.cmds = []CmdData{{NodeActivity: "Réading", Keys: "R", Command: strings.Repeat("é", 60)}}
	m.menus = []menuEntry{{Name: strings.Repeat("é", 30), Data: MenuData{Title: "Title"}}}

	for name, row := range map[string]string{
		"command row": m.renderCmdRow(0, boxW),
		"menu row":    m.renderMenuRow(0, boxW),
	} {
		got := stripStyle(row)
		if !utf8.ValidString(got) {
			t.Errorf("%s %q is not valid UTF-8", name, got)
		}
		if n := utf8.RuneCountInString(got); n != boxW {
			t.Errorf("%s is %d columns, want %d: %q", name, n, boxW, got)
		}
	}
}

// The highlighted (ready to edit) field shows the value cut to the field
// width in characters, with fill characters making up the rest.
func TestActiveFieldCutByRune(t *testing.T) {
	m := newTestEditor(t)
	m.mode = modeCommandEdit
	m.cmdEditFld = 0
	m.menuEditFld = 0

	// Node Activity is 40 wide; Menu Title is 20. A leading "A" makes the
	// byte cut land inside an "é".
	activity := "A" + strings.Repeat("é", 50)
	cmdRow := stripStyle(m.renderCmdField(0, cmdFields()[0], &CmdData{NodeActivity: activity}, 80))
	title := "A" + strings.Repeat("é", 30)
	menuRow := stripStyle(m.renderMenuField(0, menuFields()[0], &MenuData{Title: title}, 80))

	for _, tc := range []struct {
		name, row, want string
	}{
		{"command field", cmdRow, "A" + strings.Repeat("é", 39)},
		{"menu field", menuRow, "A" + strings.Repeat("é", 19)},
	} {
		if !utf8.ValidString(tc.row) {
			t.Errorf("%s %q is not valid UTF-8", tc.name, tc.row)
		}
		if !strings.Contains(tc.row, tc.want) || strings.Contains(tc.row, tc.want+"é") {
			t.Errorf("%s = %q, want the value cut to %d characters", tc.name, tc.row, utf8.RuneCountInString(tc.want))
		}
	}
}
