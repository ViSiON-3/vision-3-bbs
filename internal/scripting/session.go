package scripting

import (
	"fmt"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/dop251/goja"
)

// registerSession creates the v3.session object with session context bindings.
func registerSession(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	obj := vm.NewObject()

	jsutil.Set(obj, "node", eng.session.NodeNumber)
	jsutil.Set(obj, "startTime", eng.session.SessionStartTime.Unix())

	// timeLeft — seconds remaining in session (dynamic)
	jsutil.DefineAccessor(obj, "timeLeft", vm.ToValue(func(call goja.FunctionCall) goja.Value {
		elapsed := time.Since(eng.session.SessionStartTime)
		limit := time.Duration(eng.session.TimeLimit) * time.Minute
		if limit <= 0 {
			limit = time.Hour // default 1 hour if no limit configured
		}
		remaining := limit - elapsed
		if remaining < 0 {
			remaining = 0
		}
		return vm.ToValue(int(remaining.Seconds()))
	}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

	// online — true if user is still connected (dynamic)
	jsutil.DefineAccessor(obj, "online", vm.ToValue(func(call goja.FunctionCall) goja.Value {
		select {
		case <-eng.ctx.Done():
			return vm.ToValue(false)
		default:
			return vm.ToValue(true)
		}
	}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

	// v3.session.bbs — BBS info sub-object
	bbs := vm.NewObject()
	jsutil.Set(bbs, "name", eng.session.BoardName)
	jsutil.Set(bbs, "sysop", eng.session.SysOpName)
	jsutil.Set(bbs, "version", eng.session.BBSVersion)
	jsutil.Set(obj, "bbs", bbs)

	// v3.session.user — current user info (read-only snapshot)
	usr := vm.NewObject()
	jsutil.Set(usr, "id", eng.session.UserID)
	jsutil.Set(usr, "handle", eng.session.UserHandle)
	jsutil.Set(usr, "realName", eng.session.UserRealName)
	jsutil.Set(usr, "accessLevel", eng.session.AccessLevel)
	jsutil.Set(usr, "timesCalled", eng.session.TimesCalled)
	jsutil.Set(usr, "location", eng.session.Location)
	jsutil.Set(usr, "screenWidth", eng.session.ScreenWidth)
	jsutil.Set(usr, "screenHeight", eng.session.ScreenHeight)
	jsutil.Set(obj, "user", usr)

	// v3.args — script arguments
	args := vm.NewArray()
	for i, arg := range eng.cfg.Args {
		jsutil.Set(args, fmt.Sprintf("%d", i), arg)
	}
	jsutil.Set(v3, "args", args)

	jsutil.Set(v3, "session", obj)
}
