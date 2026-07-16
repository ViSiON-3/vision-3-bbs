package scripting

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/dop251/goja"
)

// registerUsers creates the v3.users object for read-only user database access.
func registerUsers(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	obj := vm.NewObject()

	// get(handle) — look up a user by handle, returns object or null.
	jsutil.Set(obj, "get", func(call goja.FunctionCall) goja.Value {
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
	jsutil.Set(obj, "getByID", func(call goja.FunctionCall) goja.Value {
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
	jsutil.Set(obj, "count", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(eng.providers.UserMgr.GetUserCount())
	})

	// list() — returns array of all users (safe fields only).
	jsutil.Set(obj, "list", func(call goja.FunctionCall) goja.Value {
		all := eng.providers.UserMgr.GetAllUsers()
		arr := vm.NewArray()
		for i, u := range all {
			jsutil.Set(arr, intToStr(i), userToJS(vm, u))
		}
		return arr
	})

	jsutil.Set(v3, "users", obj)
}

// userToJS converts a *user.User to a JS object with safe fields only.
// Sensitive fields (password hash, private notes, flags) are excluded.
func userToJS(vm *goja.Runtime, u *user.User) goja.Value {
	obj := vm.NewObject()
	jsutil.Set(obj, "id", u.ID)
	jsutil.Set(obj, "handle", u.Handle)
	jsutil.Set(obj, "realName", u.RealName)
	jsutil.Set(obj, "accessLevel", u.AccessLevel)
	jsutil.Set(obj, "timesCalled", u.TimesCalled)
	jsutil.Set(obj, "location", u.GroupLocation)
	jsutil.Set(obj, "messagesPosted", u.MessagesPosted)
	jsutil.Set(obj, "uploads", u.NumUploads)
	jsutil.Set(obj, "downloads", u.NumDownloads)
	jsutil.Set(obj, "filePoints", u.FilePoints)
	jsutil.Set(obj, "validated", u.Validated)
	jsutil.Set(obj, "lastLogin", u.LastLogin.Unix())
	jsutil.Set(obj, "createdAt", u.CreatedAt.Unix())
	return obj
}

func intToStr(i int) string {
	return itoa(i)
}
