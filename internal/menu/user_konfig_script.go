package menu

import (
	"log/slog"

	"github.com/ViSiON-3/vision-3-bbs/internal/scripting"
)

// scriptedMenu is KONFIG.MNU when the menu set has one that names a SCRIPT:
// the set then draws User Konfig's list of settings with that script. Without
// it the built-in form is used.
func (st *konfigState) scriptedMenu() *MenuRecord {
	if ok, _ := st.c.e.Menus().Exists("mnu", "KONFIG.MNU"); !ok {
		return nil
	}
	rec, err := LoadMenu("KONFIG", st.c.e.Menus())
	if err != nil || rec.Script == "" {
		return nil
	}
	return rec
}

// scriptView is the settings as a script sees them, in form order: each
// item's key, label, current value and help. The values are those the form
// shows, so a password is masked.
func (st *konfigState) scriptView(rec *MenuRecord) *scripting.MenuView {
	view := &scripting.MenuView{
		Title:     rec.Title,
		Shortcuts: []string{"Q"},
		Selected:  string(st.items[st.sel].key),
		Notice:    plainSetting(st.status),
	}
	for _, it := range st.items {
		view.Items = append(view.Items, scripting.MenuItemView{
			Key: string(it.key), Label: plainSetting(it.label),
			Value: plainSetting(it.value(st).text), Activity: plainSetting(it.help),
		})
		view.Shortcuts = append(view.Shortcuts, string(it.key))
	}
	return view
}

// runScripted has the script pick a setting, runs that setting's own editor,
// and repeats until the script answers Q or ESC. handled is false when the
// script could not run or gave no usable answer; the built-in form then
// takes over.
func (st *konfigState) runScripted(rec *MenuRecord) (handled bool, err error) {
	defer func() { st.place = nil }()
	for {
		st.place = nil
		view := st.scriptView(rec)
		choice, ok, err := st.c.scriptedChoice("KONFIG", rec, view)
		// The script ran with its own input handler; the editors need the
		// session's, which also holds any keys the script read ahead.
		st.ih = getSessionIH(st.c.s)
		if err != nil {
			return true, err
		}
		if !ok {
			return false, nil
		}
		if choice == "Q" || choice == "ESC" {
			return true, nil
		}
		i := -1
		if len(choice) == 1 {
			i = st.itemForKey(int(choice[0]))
		}
		if i < 0 {
			slog.Warn("User Konfig script answered with a key that is not a setting; using the built-in form", "answer", choice)
			return false, nil
		}
		st.sel, st.status, st.redraw = i, "", false
		e := st.c.e
		if err := st.placeEditor(view.Fields[choice], view.PromptRow); err != nil {
			return true, err
		}
		if err := st.items[i].edit(st); err != nil {
			return true, err
		}
		if st.c.e != e {
			// The caller picked another theme: Konfig carries on as that
			// theme draws it, by its own KONFIG.MNU script or else the
			// built-in form.
			if rec = st.scriptedMenu(); rec == nil {
				return false, nil
			}
		}
	}
}

// konfigPlace is where a setting's editor draws on a screen a script drew.
type konfigPlace struct {
	// field is where the script shows the setting's field; Y is 0 when it
	// gave none.
	field scripting.FieldPos
	// prompt is the row for the editor's label, prompts and messages and
	// help the row for the setting's help; 0 when there is none. help is
	// set only on the built-in form's rows after a clear.
	prompt, help int
	// labelFrom is the first column of the widest label drawn beside the
	// field so far, or 0.
	labelFrom int
}

// scriptField is the field the script gave for the setting being edited,
// or nil.
func (st *konfigState) scriptField() *scripting.FieldPos {
	if st.place == nil || st.place.field.Y == 0 {
		return nil
	}
	return &st.place.field
}

// placeEditor sets st.place for the setting the script picked, whose field
// the script put at field. Prompts go on promptRow when the script named
// one. With neither a field nor a prompt row, an editor that prompts clears
// the screen first (ensurePromptRow), so its prompt is on a row with nothing
// of the script's on it, and uses the built-in form's rows.
func (st *konfigState) placeEditor(field scripting.FieldPos, promptRow int) error {
	p := &konfigPlace{}
	if field.Y >= 1 && field.Y <= st.height() && field.X >= 1 && field.X <= st.width() {
		p.field = field
	}
	if promptRow >= 1 && promptRow <= st.height() && promptRow != p.field.Y {
		p.prompt = promptRow
	}
	st.place = p
	return nil
}

// ensurePromptRow clears the screen for an editor that is about to draw a
// prompt when the script gave the setting neither a field nor a prompt row.
// A setting that draws nothing, such as a
// switch, leaves the script's screen alone.
func (st *konfigState) ensurePromptRow() error {
	if p := st.place; p != nil && p.field.Y == 0 && p.prompt == 0 {
		return st.promptOnClearScreen()
	}
	return nil
}

// promptOnClearScreen clears the screen a script drew and has the editor
// prompt on the built-in form's rows, for an editor with nowhere else to
// show its prompt.
func (st *konfigState) promptOnClearScreen() error {
	st.place = &konfigPlace{prompt: st.formEditRow(), help: st.formHelpRow()}
	return st.raw("\x1b[0m\x1b[2J\x1b[H")
}
