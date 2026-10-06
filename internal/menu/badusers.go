package menu

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"log/slog"
)

// badUserNameRule reads the selected list and returns the matching rule.
// Read or validation failures log a warning and return no match.
func (e *MenuExecutor) badUserNameRule(handle string) string {
	path := e.GetServerConfig().BadUsersFile(e.RootConfigPath)
	rule, err := config.MatchBadUserName(path, handle)
	if err != nil {
		slog.Warn("cannot check bad user names; preserving signup checks", "path", path, "error", err)
	}
	return rule
}

// validateSignupHandle applies existing handle checks and the configured bad-name
// rules, logging rejected handles and their matching rules at INFO.
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
