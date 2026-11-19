package scripting

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/dop251/goja"
)

// registerOneliners creates the v3.oneliners object: read recent oneliners
// and append one as the current caller.
func registerOneliners(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	obj := vm.NewObject()

	// list(n) — the n most recent oneliners, newest first. n defaults to
	// 10; 0 or less returns every one.
	jsutil.Set(obj, "list", func(call goja.FunctionCall) goja.Value {
		limit := 10
		if len(call.Arguments) > 0 {
			limit = int(call.Arguments[0].ToInteger())
		}
		arr := vm.NewArray()
		for i, r := range eng.providers.Oneliners.Recent(limit) {
			o := vm.NewObject()
			jsutil.Set(o, "text", r.Text)
			jsutil.Set(o, "handle", r.Handle)
			jsutil.Set(o, "anonymous", r.Anonymous)
			if r.PostedAt.IsZero() {
				jsutil.Set(o, "postedAt", int64(0))
			} else {
				jsutil.Set(o, "postedAt", r.PostedAt.Unix())
			}
			jsutil.Set(arr, intToStr(i), o)
		}
		return arr
	})

	// add(text [, anonymous]) — append a oneliner posted as the current
	// caller. Returns true on success, false on an empty line or a write
	// failure.
	jsutil.Set(obj, "add", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue(false)
		}
		text := call.Arguments[0].String()
		anon := false
		if len(call.Arguments) > 1 {
			anon = call.Arguments[1].ToBoolean()
		}
		handle, username := "", ""
		if eng.providers.CurrentUser != nil {
			handle = eng.providers.CurrentUser.Handle
			username = eng.providers.CurrentUser.Handle
		}
		if err := eng.providers.Oneliners.Add(handle, username, text, anon); err != nil {
			return vm.ToValue(false)
		}
		return vm.ToValue(true)
	})

	jsutil.Set(v3, "oneliners", obj)
}
