package scripting

import (
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/dop251/goja"
)

// MenuView is the menu a script draws in place of its screen.
type MenuView struct {
	// Name is the menu's file name without the extension, and Title its
	// TITLE.
	Name  string
	Title string
	// Items are the commands to show, in menu order: those the caller's
	// access allows that are not hidden.
	Items []MenuItemView
	// Shortcuts is every key the caller may use on this menu, hidden
	// commands and alternative keys included.
	Shortcuts []string
	// The rest is set by a built-in screen that hands its drawing to a
	// script (User Konfig, the Twit List); a menu leaves it empty. Body is
	// the screen's rows of plain text above its items, Selected the key of
	// the item to highlight first, Notice the result of the caller's last
	// action, and Hint a standing line of help.
	Body     []string
	Selected string
	Notice   string
	Hint     string

	// Fields is where a built-in screen's script shows each item's field,
	// by item key, set with v3.menu.field(key, x, y, width). The screen
	// draws that item's editor there. Nil until the script sets one.
	Fields map[string]FieldPos
	// PromptRow is the row the script keeps for the screen's prompts and
	// messages, set with v3.menu.promptRow(y); 0 when it named none.
	PromptRow int

	// Expand fills in the board's placeholder codes (|UH, |CC, |CAN and
	// the rest of a menu prompt's) in a script's text, read as
	// v3.menu.expand(text). Colour codes are left for the script to print.
	// Nil expands nothing.
	Expand func(string) string
}

// MenuItemView is one command of a menu, as a script sees it.
type MenuItemView struct {
	Key      string // the first of the command's keys
	Label    string // the command's LABEL, or its node activity without one
	Activity string // the command's node activity, or a setting's help
	Value    string // a setting's current value as plain text; "" for a command
}

// FieldPos is a field's place on the screen: its first column X and its
// row Y, counted from 1 as v3.console.gotoxy counts them, and its width in
// columns, 0 when the script left it to the screen.
type FieldPos struct {
	X, Y, Width int
}

// registerMenu creates the v3.menu object for a script that draws a menu:
// name, title, items and shortcuts, and for a built-in screen its body,
// selected key, notice and hint. The script hands the caller's choice
// back with v3.prompt.answer, as a menu prompt script does (PromptMenu).
func registerMenu(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	m := eng.providers.Menu
	obj := vm.NewObject()
	jsutil.Set(obj, "name", m.Name)
	jsutil.Set(obj, "title", m.Title)

	items := make([]any, len(m.Items))
	for i, it := range m.Items {
		items[i] = map[string]any{"key": it.Key, "label": it.Label, "activity": it.Activity, "value": it.Value}
	}
	jsutil.Set(obj, "items", vm.ToValue(items))

	shortcuts := make([]any, len(m.Shortcuts))
	for i, k := range m.Shortcuts {
		shortcuts[i] = k
	}
	jsutil.Set(obj, "shortcuts", vm.ToValue(shortcuts))

	body := make([]any, len(m.Body))
	for i, row := range m.Body {
		body[i] = row
	}
	jsutil.Set(obj, "body", vm.ToValue(body))
	jsutil.Set(obj, "selected", m.Selected)
	jsutil.Set(obj, "notice", m.Notice)
	jsutil.Set(obj, "hint", m.Hint)
	jsutil.Set(obj, "expand", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue("")
		}
		text := call.Arguments[0].String()
		if m.Expand != nil {
			text = m.Expand(text)
		}
		return vm.ToValue(text)
	})

	// field(key, x, y, width) — where the script shows the field of the
	// item with key key. A call without a key or a position on the screen
	// is ignored; width is optional.
	jsutil.Set(obj, "field", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 3 {
			return goja.Undefined()
		}
		key := strings.ToUpper(strings.TrimSpace(call.Arguments[0].String()))
		x, y := int(call.Arguments[1].ToInteger()), int(call.Arguments[2].ToInteger())
		if key == "" || x < 1 || y < 1 {
			return goja.Undefined()
		}
		width := 0
		if len(call.Arguments) > 3 {
			width = max(int(call.Arguments[3].ToInteger()), 0)
		}
		if m.Fields == nil {
			m.Fields = map[string]FieldPos{}
		}
		m.Fields[key] = FieldPos{X: x, Y: y, Width: width}
		return goja.Undefined()
	})

	// promptRow(y) — the row the screen may use for its prompts and
	// messages. A row below 1 is ignored.
	jsutil.Set(obj, "promptRow", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) > 0 {
			if y := int(call.Arguments[0].ToInteger()); y >= 1 {
				m.PromptRow = y
			}
		}
		return goja.Undefined()
	})

	jsutil.Set(v3, "menu", obj)
}
