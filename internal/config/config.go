// Package config defines the BBS's configuration types and loads and saves
// the JSON files in the configs directory: config.json (LoadServerConfig,
// SaveServerConfig), ftn.json, qwknet.json, v3net.json, events.json,
// doors.json, login.json, strings.json, and a menu set's theme.json. Loaders
// fill in defaults for missing files and fields, and validators catch
// settings that would break a network or door at run time. The reload
// sentinel (configs/reload.now) lets config-writing tools tell a running BBS
// to re-read everything.
package config

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

// LoadTimezone returns a *time.Location for the given timezone string.
// It tries the value from config.json first, then the VISION3_TIMEZONE and TZ
// environment variables, falling back to time.Local if none resolve.
func LoadTimezone(configTZ string) *time.Location {
	// Try each source in order: config value, VISION3_TIMEZONE env, TZ env
	for _, tz := range []string{
		strings.TrimSpace(configTZ),
		strings.TrimSpace(os.Getenv("VISION3_TIMEZONE")),
		strings.TrimSpace(os.Getenv("TZ")),
	} {
		if tz == "" {
			continue
		}
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
		slog.Warn("invalid timezone, trying next source", "timezone", tz)
	}
	return time.Local
}

// NowIn returns the current time in the configured timezone.
func NowIn(configTZ string) time.Time {
	return time.Now().In(LoadTimezone(configTZ))
}
