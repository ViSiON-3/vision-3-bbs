package syncjs

import (
	"fmt"
	"time"

	"github.com/dop251/goja"
)

// registerBBS creates the Synchronet-compatible bbs object on the JS runtime.
func registerBBS(vm *goja.Runtime, eng *Engine) {
	obj := vm.NewObject()

	obj.Set("node_num", eng.session.NodeNumber)

	// bbs.sys_status — system status flags (read/write)
	var sysStatus int64
	obj.DefineAccessorProperty("sys_status", vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(sysStatus)
	}), vm.ToValue(func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) > 0 {
			sysStatus = call.Arguments[0].ToInteger()
		}
		return goja.Undefined()
	}), goja.FLAG_FALSE, goja.FLAG_TRUE)

	// bbs.online — true if user is still connected
	obj.DefineAccessorProperty("online", vm.ToValue(func(call goja.FunctionCall) goja.Value {
		select {
		case <-eng.ctx.Done():
			return vm.ToValue(false)
		default:
			return vm.ToValue(true)
		}
	}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

	// bbs.get_time_left() — seconds remaining in session
	obj.Set("get_time_left", func(call goja.FunctionCall) goja.Value {
		elapsed := time.Since(eng.session.SessionStartTime)
		limit := time.Duration(eng.session.TimeLimit) * time.Minute
		if limit <= 0 {
			return vm.ToValue(3600) // default 1 hour if no limit
		}
		remaining := limit - elapsed
		if remaining < 0 {
			remaining = 0
		}
		return vm.ToValue(int(remaining.Seconds()))
	})

	// bbs.atcode(code) — convert @-code to value string
	obj.Set("atcode", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue("")
		}
		return vm.ToValue(resolveAtCode(call.Arguments[0].String(), eng))
	})

	obj.Set("logon_time", eng.session.SessionStartTime.Unix())

	// bbs.mods — shared object for inter-module communication
	obj.Set("mods", vm.NewObject())

	vm.Set("bbs", obj)
}

// resolveAtCode converts Synchronet @-codes to their values.
func resolveAtCode(code string, eng *Engine) string {
	switch code {
	case "USER", "ALIAS":
		return eng.session.UserHandle
	case "NAME", "REALNAME":
		return eng.session.UserRealName
	case "NODE":
		return fmt.Sprintf("%d", eng.session.NodeNumber)
	case "SYSOP":
		return eng.session.SysOpName
	case "BBS":
		return eng.session.BoardName
	default:
		return ""
	}
}
