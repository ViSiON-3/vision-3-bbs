package syncjs

import (
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/dop251/goja"
)

// registerUser creates the Synchronet-compatible user object on the JS runtime.
func registerUser(vm *goja.Runtime, eng *Engine) {
	obj := vm.NewObject()

	jsutil.Set(obj, "alias", eng.session.UserHandle)
	jsutil.Set(obj, "name", eng.session.UserRealName)
	jsutil.Set(obj, "number", eng.session.UserID)

	// user.security — nested object with level, etc.
	security := vm.NewObject()
	jsutil.Set(security, "level", eng.session.AccessLevel)
	jsutil.Set(security, "password", "") // never expose real password
	jsutil.Set(obj, "security", security)

	// Convenience aliases used by some games
	jsutil.Set(obj, "level", eng.session.AccessLevel)
	jsutil.Set(obj, "full_name", eng.session.UserRealName)
	jsutil.Set(obj, "location", eng.session.Location)
	jsutil.Set(obj, "handle", eng.session.UserHandle)

	// user.settings — user preference flags (USER_EXPERT=2)
	jsutil.Set(obj, "settings", 2)

	// Date properties used by sbbs_console.js (Unix timestamps)
	now := time.Now().Unix()
	jsutil.Set(obj, "laston_date", now)
	jsutil.Set(obj, "expiration_date", 0)
	jsutil.Set(obj, "new_file_time", now)
	jsutil.Set(obj, "birthdate", "01/01/90")
	jsutil.Set(obj, "phone", "")
	jsutil.Set(obj, "comment", "")
	jsutil.Set(obj, "download_protocol", "")

	// user.stats — usage statistics
	stats := vm.NewObject()
	jsutil.Set(stats, "total_logons", eng.session.TimesCalled)
	jsutil.Set(stats, "total_posts", 0)
	jsutil.Set(stats, "total_emails", 0)
	jsutil.Set(stats, "files_uploaded", 0)
	jsutil.Set(stats, "files_downloaded", 0)
	jsutil.Set(stats, "bytes_uploaded", 0)
	jsutil.Set(stats, "bytes_downloaded", 0)
	jsutil.Set(obj, "stats", stats)

	jsutil.Set(vm, "user", obj)
}
