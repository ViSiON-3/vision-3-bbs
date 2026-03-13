package syncjs

import (
	"time"

	"github.com/dop251/goja"
)

// registerUser creates the Synchronet-compatible user object on the JS runtime.
func registerUser(vm *goja.Runtime, eng *Engine) {
	obj := vm.NewObject()

	obj.Set("alias", eng.session.UserHandle)
	obj.Set("name", eng.session.UserRealName)
	obj.Set("number", eng.session.UserID)

	// user.security — nested object with level, etc.
	security := vm.NewObject()
	security.Set("level", eng.session.AccessLevel)
	security.Set("password", "") // never expose real password
	obj.Set("security", security)

	// Convenience aliases used by some games
	obj.Set("level", eng.session.AccessLevel)
	obj.Set("full_name", eng.session.UserRealName)
	obj.Set("location", eng.session.Location)
	obj.Set("handle", eng.session.UserHandle)

	// user.settings — user preference flags (USER_EXPERT=2)
	obj.Set("settings", 2)

	// Date properties used by sbbs_console.js (Unix timestamps)
	now := time.Now().Unix()
	obj.Set("laston_date", now)
	obj.Set("expiration_date", 0)
	obj.Set("new_file_time", now)
	obj.Set("birthdate", "01/01/90")
	obj.Set("phone", "")
	obj.Set("comment", "")
	obj.Set("download_protocol", "")

	// user.stats — usage statistics
	stats := vm.NewObject()
	stats.Set("total_logons", eng.session.TimesCalled)
	stats.Set("total_posts", 0)
	stats.Set("total_emails", 0)
	stats.Set("files_uploaded", 0)
	stats.Set("files_downloaded", 0)
	stats.Set("bytes_uploaded", 0)
	stats.Set("bytes_downloaded", 0)
	obj.Set("stats", stats)

	vm.Set("user", obj)
}
