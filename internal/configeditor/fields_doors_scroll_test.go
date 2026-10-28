package configeditor

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/charmbracelet/bubbles/textinput"
)

// Door names, paths and command lines often outgrow the record box, so their
// fields take more text than they show and scroll while editing.
func TestDoorTextFieldsScrollPastWidth(t *testing.T) {
	longName := "Legend of the Red Dragon - The Extended Director's Cut Edition"
	longCmd := "/opt/doors/lord/start.sh {NODE} {DROPFILE} --with-a-long-flag --and-another-one-too"
	m := newDoorModel(map[string]config.DoorConfig{
		"LORD": {Code: "LORD", Name: longName, Commands: []string{longCmd}},
	})
	m.textInput = textinput.New()
	m.mode = modeRecordEdit
	m.recordEditIdx = 0
	m.recordFields = m.buildRecordFields()

	for _, label := range []string{"Name", "Commands"} {
		t.Run(label, func(t *testing.T) {
			idx := -1
			for i, f := range m.recordFields {
				if f.Label == label {
					idx = i
				}
			}
			if idx < 0 {
				t.Fatalf("no %s field", label)
			}
			f := m.recordFields[idx]
			want := f.Get()
			if len(want) <= f.Width {
				t.Fatalf("test value %q fits width %d; make it longer", want, f.Width)
			}

			m.editField = idx
			// Not editing: the value is cut with an ellipsis to mark the rest.
			if row := m.renderRecordEditRow(f.Row, 70); !strings.Contains(row, "…") || screenCells(row) != 70 {
				t.Errorf("display row = %q (%d cells), want 70 cells with an ellipsis", row, screenCells(row))
			}

			edit, _ := m.startRecordFieldEdit()
			if got := edit.textInput.Value(); got != want {
				t.Errorf("editing value = %q, want the full %q", got, want)
			}
			// The scrolled edit view must stay inside the 70-column box, cursor
			// cell included, at both ends of the text.
			for _, pos := range []string{"end", "start"} {
				if pos == "start" {
					edit.textInput.CursorStart()
				}
				if _, w := edit.renderRecordField(idx, f); w > 68 {
					t.Errorf("field width %d overflows the box", w)
				}
				row := edit.renderRecordEditRow(f.Row, 70)
				if screenCells(row) != 70 || strings.Contains(row, "…") {
					t.Errorf("cursor at %s: edit row = %q (%d cells), want exactly 70 untruncated", pos, row, screenCells(row))
				}
			}
		})
	}
}
