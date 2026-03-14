package scripting

import (
	"fmt"
	"time"

	"github.com/dop251/goja"
)

// registerSession creates the v3.session object with session context bindings.
func registerSession(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	obj := vm.NewObject()

	obj.Set("node", eng.session.NodeNumber)
	obj.Set("startTime", eng.session.SessionStartTime.Unix())

	// timeLeft — seconds remaining in session (dynamic)
	obj.DefineAccessorProperty("timeLeft", vm.ToValue(func(call goja.FunctionCall) goja.Value {
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
	}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

	// online — true if user is still connected (dynamic)
	obj.DefineAccessorProperty("online", vm.ToValue(func(call goja.FunctionCall) goja.Value {
		select {
		case <-eng.ctx.Done():
			return vm.ToValue(false)
		default:
			return vm.ToValue(true)
		}
	}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

	// v3.session.bbs — BBS info sub-object
	bbs := vm.NewObject()
	bbs.Set("name", eng.session.BoardName)
	bbs.Set("sysop", eng.session.SysOpName)
	bbs.Set("version", eng.session.BBSVersion)
	obj.Set("bbs", bbs)

	// v3.session.user — current user info (read-only snapshot)
	usr := vm.NewObject()
	usr.Set("id", eng.session.UserID)
	usr.Set("handle", eng.session.UserHandle)
	usr.Set("realName", eng.session.UserRealName)
	usr.Set("accessLevel", eng.session.AccessLevel)
	usr.Set("timesCalled", eng.session.TimesCalled)
	usr.Set("location", eng.session.Location)
	usr.Set("screenWidth", eng.session.ScreenWidth)
	usr.Set("screenHeight", eng.session.ScreenHeight)
	obj.Set("user", usr)

	// v3.args — script arguments
	args := vm.NewArray()
	for i, arg := range eng.cfg.Args {
		args.Set(fmt.Sprintf("%d", i), arg)
	}
	v3.Set("args", args)

	v3.Set("session", obj)
}
