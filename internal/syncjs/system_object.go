package syncjs

import (
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/dop251/goja"
)

// registerSystem creates the Synchronet-compatible system object on the JS runtime.
func registerSystem(vm *goja.Runtime, eng *Engine) {
	obj := vm.NewObject()

	jsutil.Set(obj, "name", eng.session.BoardName)
	jsutil.Set(obj, "operator", eng.session.SysOpName)
	jsutil.Set(obj, "qwk_id", makeQWKID(eng.session.BoardName))

	// system.timer — current time in milliseconds (used for timing)
	jsutil.DefineAccessor(obj, "timer", vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(time.Now().UnixMilli())
	}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

	// Directory paths
	jsutil.Set(obj, "exec_dir", eng.cfg.ExecDir)
	jsutil.Set(obj, "data_dir", eng.cfg.DataDir)
	jsutil.Set(obj, "node_dir", eng.cfg.NodeDir)
	jsutil.Set(obj, "ctrl_dir", eng.cfg.ExecDir)
	jsutil.Set(obj, "text_dir", eng.cfg.DataDir)

	jsutil.Set(obj, "nodes", 4)

	jsutil.Set(vm, "system", obj)
}

// makeQWKID derives a QWK ID from the BBS name.
func makeQWKID(name string) string {
	id := strings.ToUpper(name)
	id = strings.ReplaceAll(id, " ", "")
	if len(id) > 8 {
		id = id[:8]
	}
	return id
}
