package syncjs

import (
	"strings"
	"time"

	"github.com/dop251/goja"
)

// registerSystem creates the Synchronet-compatible system object on the JS runtime.
func registerSystem(vm *goja.Runtime, eng *Engine) {
	obj := vm.NewObject()

	obj.Set("name", eng.session.BoardName)
	obj.Set("operator", eng.session.SysOpName)
	obj.Set("qwk_id", makeQWKID(eng.session.BoardName))

	// system.timer — current time in milliseconds (used for timing)
	obj.DefineAccessorProperty("timer", vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(time.Now().UnixMilli())
	}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

	// Directory paths
	obj.Set("exec_dir", eng.cfg.ExecDir)
	obj.Set("data_dir", eng.cfg.DataDir)
	obj.Set("node_dir", eng.cfg.NodeDir)
	obj.Set("ctrl_dir", eng.cfg.ExecDir)
	obj.Set("text_dir", eng.cfg.DataDir)

	obj.Set("nodes", 4)

	vm.Set("system", obj)
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
