package scripting

import (
	"github.com/dop251/goja"
)

// registerUser creates the v3.user object with current user bindings.
// The user object provides read access to session user fields and
// write access via set()/save() for the current user's own record.
func registerUser(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	obj := vm.NewObject()
	u := eng.providers.CurrentUser

	// Read-only properties — reflect live user state.
	obj.DefineAccessorProperty("id", vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(eng.providers.CurrentUser.ID)
	}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

	obj.DefineAccessorProperty("handle", vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(eng.providers.CurrentUser.Handle)
	}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

	obj.DefineAccessorProperty("realName", vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(eng.providers.CurrentUser.RealName)
	}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

	obj.DefineAccessorProperty("accessLevel", vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(eng.providers.CurrentUser.AccessLevel)
	}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

	obj.DefineAccessorProperty("timesCalled", vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(eng.providers.CurrentUser.TimesCalled)
	}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

	obj.DefineAccessorProperty("location", vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(eng.providers.CurrentUser.GroupLocation)
	}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

	obj.DefineAccessorProperty("messagesPosted", vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(eng.providers.CurrentUser.MessagesPosted)
	}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

	obj.DefineAccessorProperty("uploads", vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(eng.providers.CurrentUser.NumUploads)
	}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

	obj.DefineAccessorProperty("downloads", vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(eng.providers.CurrentUser.NumDownloads)
	}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

	obj.DefineAccessorProperty("filePoints", vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(eng.providers.CurrentUser.FilePoints)
	}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

	obj.DefineAccessorProperty("validated", vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(eng.providers.CurrentUser.Validated)
	}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

	// set(field, value) — update a writable field on the current user.
	// Changes are held in memory until save() is called.
	obj.Set("set", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			return goja.Undefined()
		}
		field := call.Arguments[0].String()
		switch field {
		case "realName":
			u.RealName = call.Arguments[1].String()
		case "location":
			u.GroupLocation = call.Arguments[1].String()
		case "screenWidth":
			u.ScreenWidth = int(call.Arguments[1].ToInteger())
		case "screenHeight":
			u.ScreenHeight = int(call.Arguments[1].ToInteger())
		}
		return goja.Undefined()
	})

	// save() — persist current user changes to disk via UserMgr.
	obj.Set("save", func(call goja.FunctionCall) goja.Value {
		if err := eng.providers.UserMgr.UpdateUserByID(u); err != nil {
			panic(vm.NewGoError(err))
		}
		return goja.Undefined()
	})

	v3.Set("user", obj)
}
