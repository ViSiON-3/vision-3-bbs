package scripting

import (
	"github.com/dop251/goja"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// registerUsers creates the v3.users object for read-only user database access.
func registerUsers(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	obj := vm.NewObject()

	// get(handle) — look up a user by handle, returns object or null.
	obj.Set("get", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return goja.Null()
		}
		handle := call.Arguments[0].String()
		u, found := eng.providers.UserMgr.GetUser(handle)
		if !found {
			return goja.Null()
		}
		return userToJS(vm, u)
	})

	// getByID(id) — look up a user by ID, returns object or null.
	obj.Set("getByID", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return goja.Null()
		}
		id := int(call.Arguments[0].ToInteger())
		u, found := eng.providers.UserMgr.GetUserByID(id)
		if !found {
			return goja.Null()
		}
		return userToJS(vm, u)
	})

	// count() — total registered user count.
	obj.Set("count", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(eng.providers.UserMgr.GetUserCount())
	})

	// list() — returns array of all users (safe fields only).
	obj.Set("list", func(call goja.FunctionCall) goja.Value {
		all := eng.providers.UserMgr.GetAllUsers()
		arr := vm.NewArray()
		for i, u := range all {
			arr.Set(intToStr(i), userToJS(vm, u))
		}
		return arr
	})

	v3.Set("users", obj)
}

// userToJS converts a *user.User to a JS object with safe fields only.
// Sensitive fields (password hash, private notes, flags) are excluded.
func userToJS(vm *goja.Runtime, u *user.User) goja.Value {
	obj := vm.NewObject()
	obj.Set("id", u.ID)
	obj.Set("handle", u.Handle)
	obj.Set("realName", u.RealName)
	obj.Set("accessLevel", u.AccessLevel)
	obj.Set("timesCalled", u.TimesCalled)
	obj.Set("location", u.GroupLocation)
	obj.Set("messagesPosted", u.MessagesPosted)
	obj.Set("uploads", u.NumUploads)
	obj.Set("downloads", u.NumDownloads)
	obj.Set("filePoints", u.FilePoints)
	obj.Set("validated", u.Validated)
	obj.Set("lastLogin", u.LastLogin.Unix())
	obj.Set("createdAt", u.CreatedAt.Unix())
	return obj
}

func intToStr(i int) string {
	return itoa(i)
}
