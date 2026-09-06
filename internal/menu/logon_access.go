package menu

import "github.com/ViSiON-3/vision-3-bbs/internal/config"

// canLogonAtLevel reports whether an account at this access level is allowed to
// log in under the given configuration.
//
// Login is decided by access level alone: executor_login_auth.go denies a
// caller whose level is below logonLevel, and nothing consults the validated
// flag. Anything telling a user whether they can get on must ask this rather
// than inspecting validation, or it will say the opposite of what happens.
//
// A logonLevel of zero or below disables the check, so everyone passes.
func canLogonAtLevel(cfg config.ServerConfig, accessLevel int) bool {
	if cfg.LogonLevel <= 0 {
		return true
	}
	return accessLevel >= cfg.LogonLevel
}
