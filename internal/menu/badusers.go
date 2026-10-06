package menu

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"log/slog"
)

func (e *MenuExecutor) badUserNameRule(handle string) string {
	path := e.GetServerConfig().BadUsersFile(e.RootConfigPath)
	rule, err := config.MatchBadUserName(path, handle)
	if err != nil {
		slog.Warn("cannot check bad user names; preserving signup checks", "path", path, "error", err)
	}
	return rule
}

func (e *MenuExecutor) validateSignupHandle(handle string) bool {
	if !validateHandle(handle) {
		return false
	}
	if rule := e.badUserNameRule(handle); rule != "" {
		slog.Info("new user handle rejected: matches badusers rule", "handle", handle, "rule", rule)
		return false
	}
	return true
}
