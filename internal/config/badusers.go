package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// BadUsersFile resolves the signup name list. The default follows the selected
// config directory; custom relative paths follow the process working directory,
// like ipBlocklistPath. Blank selects the default rather than disabling checks.
func (c ServerConfig) BadUsersFile(configDir string) string {
	if c.BadUsersPath != "" && c.BadUsersPath != "configs/badusers.txt" {
		return c.BadUsersPath
	}
	if configDir == "" {
		configDir = "configs"
	}
	return filepath.Join(configDir, "badusers.txt")
}

// MatchBadUserName re-reads path and returns the first matching rule, or an empty
// string when none matches. Only * is special; all other characters are literal.
// Rules and handles are trimmed and compared case-insensitively, without other
// normalization. Invalid UTF-8 or control characters invalidate the entire list;
// callers must warn and preserve signup when an error is returned.
func MatchBadUserName(path, handle string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("bad user names file contains invalid UTF-8")
	}
	var rules []string
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		for _, ch := range line {
			if unicode.IsControl(ch) {
				return "", fmt.Errorf("bad user names rule on line %d contains a control character", i+1)
			}
		}
		rules = append(rules, line)
	}
	handle = strings.TrimSpace(handle)
	for _, rule := range rules {
		pattern := "(?is)\\A" + strings.ReplaceAll(regexp.QuoteMeta(rule), "\\*", ".*") + "\\z"
		matched, err := regexp.MatchString(pattern, handle)
		if err != nil {
			return "", fmt.Errorf("invalid bad user names rule: %w", err)
		}
		if matched {
			return rule, nil
		}
	}
	return "", nil
}
