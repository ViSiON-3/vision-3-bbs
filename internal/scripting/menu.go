package scripting

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/dop251/goja"
)

// MenuItem is one command of a menu a script draws (ScriptConfig.Menu), as
// the script reads it from v3.menu.items.
type MenuItem struct {
	Key      string // the first of the command's keys, as typed to choose it
	Label    string // the command's LABEL, or its node activity without one
	Activity string // the command's node activity; may be ""
}

// registerMenu creates v3.menu for a script that draws a menu: items, an
// array of {key, label, activity} in menu order. It is not registered for
// any other script.
func registerMenu(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	items := make([]any, len(eng.cfg.Menu))
	for i, it := range eng.cfg.Menu {
		o := vm.NewObject()
		jsutil.Set(o, "key", it.Key)
		jsutil.Set(o, "label", it.Label)
		jsutil.Set(o, "activity", it.Activity)
		items[i] = o
	}
	obj := vm.NewObject()
	jsutil.Set(obj, "items", vm.NewArray(items...))
	jsutil.Set(v3, "menu", obj)
}
