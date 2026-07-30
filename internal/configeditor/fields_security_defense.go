package configeditor

import (
	"strconv"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/uitext"
)

// Field definitions for the Access & Security sub-screens that defend against
// abuse: automated-connection defense and new-user vetting.

// sysFieldsBotDefense returns fields for the Bot Defense sub-screen
// (Challenge Gate + connection-rate limiter).
func sysFieldsBotDefense(cfg *config.ServerConfig) []fieldDef {
	return []fieldDef{
		{
			Label: "Enable Gate", Help: "Require a key-press challenge before login", Type: ftYesNo, Col: 3, Row: 1, Width: 1,
			Get: func() string { return uitext.BoolToYN(cfg.EnableChallengeGate) },
			Set: func(val string) error { cfg.EnableChallengeGate = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Gate Art File", Help: "ANSI/ASCII file in the menu ansi dir", Type: ftString, Col: 3, Row: 2, Width: 20,
			Get: func() string { return cfg.ChallengeGateFile },
			Set: func(val string) error { cfg.ChallengeGateFile = val; return nil },
		},
		{
			Label: "Challenge Key", Help: "ESC or a single character to press", Type: ftString, Col: 3, Row: 3, Width: 6,
			Get: func() string { return cfg.ChallengeGateKey },
			Set: func(val string) error { cfg.ChallengeGateKey = val; return nil },
		},
		{
			Label: "Timeout Secs", Help: "Seconds allowed to complete the challenge", Type: ftInteger, Col: 3, Row: 4, Width: 4, Min: 1, Max: 999,
			Get: func() string { return strconv.Itoa(cfg.ChallengeGateTimeoutSeconds) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.ChallengeGateTimeoutSeconds = n
				return nil
			},
		},
		{
			Label: "Req Presses", Help: "Key presses required to pass", Type: ftInteger, Col: 3, Row: 5, Width: 3, Min: 1, Max: 99,
			Get: func() string { return strconv.Itoa(cfg.ChallengeGateRequiredPresses) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.ChallengeGateRequiredPresses = n
				return nil
			},
		},
		{
			Label: "Live Countdown", Help: "Animate the ## countdown (off = static)", Type: ftYesNo, Col: 3, Row: 6, Width: 1,
			Get: func() string { return uitext.BoolToYN(cfg.ChallengeGateLiveCountdown) },
			Set: func(val string) error { cfg.ChallengeGateLiveCountdown = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Rate Limit", Help: "Temp-ban IPs that reconnect too fast", Type: ftYesNo, Col: 3, Row: 7, Width: 1,
			Get: func() string { return uitext.BoolToYN(cfg.EnableConnRateLimit) },
			Set: func(val string) error { cfg.EnableConnRateLimit = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Rate Hits", Help: "Attempts within the window that trigger a ban", Type: ftInteger, Col: 3, Row: 8, Width: 4, Min: 0, Max: 9999,
			Get: func() string { return strconv.Itoa(cfg.ConnRateLimitHits) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.ConnRateLimitHits = n
				return nil
			},
		},
		{
			Label: "Rate Window", Help: "Sliding window in seconds", Type: ftInteger, Col: 3, Row: 9, Width: 4, Min: 1, Max: 9999,
			Get: func() string { return strconv.Itoa(cfg.ConnRateLimitWindowSeconds) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.ConnRateLimitWindowSeconds = n
				return nil
			},
		},
		{
			Label: "Ban Minutes", Help: "Temp-ban duration in minutes", Type: ftInteger, Col: 3, Row: 10, Width: 5, Min: 1, Max: 99999,
			Get: func() string { return strconv.Itoa(cfg.ConnRateLimitBanMinutes) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.ConnRateLimitBanMinutes = n
				return nil
			},
		},
	}
}

// sysFieldsNUV returns fields for the New User Voting sub-screen.
func sysFieldsNUV(cfg *config.ServerConfig) []fieldDef {
	return []fieldDef{
		{
			Label: "Use NUV", Help: "Enable New User Voting system", Type: ftYesNo, Col: 3, Row: 1, Width: 1,
			Get: func() string { return uitext.BoolToYN(cfg.UseNUV) },
			Set: func(val string) error { cfg.UseNUV = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Auto Add NUV", Help: "Automatically queue new registrations for voting", Type: ftYesNo, Col: 3, Row: 2, Width: 1,
			Get: func() string { return uitext.BoolToYN(cfg.AutoAddNUV) },
			Set: func(val string) error { cfg.AutoAddNUV = uitext.YNToBool(val); return nil },
		},
		{
			Label: "NUV Use Level", Help: "Minimum access level required to vote", Type: ftInteger, Col: 3, Row: 3, Width: 3, Min: 0, Max: 255,
			Get: func() string { return strconv.Itoa(cfg.NUVUseLevel) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.NUVUseLevel = n
				return nil
			},
		},
		{
			Label: "NUV Yes Votes", Help: "YES votes required to trigger yes threshold", Type: ftInteger, Col: 3, Row: 4, Width: 3, Min: 1, Max: 999,
			Get: func() string { return strconv.Itoa(cfg.NUVYesVotes) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.NUVYesVotes = n
				return nil
			},
		},
		{
			Label: "NUV No Votes", Help: "NO votes required to trigger no threshold", Type: ftInteger, Col: 3, Row: 5, Width: 3, Min: 1, Max: 999,
			Get: func() string { return strconv.Itoa(cfg.NUVNoVotes) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.NUVNoVotes = n
				return nil
			},
		},
		{
			Label: "NUV Validate", Help: "Auto-validate user when yes threshold reached", Type: ftYesNo, Col: 3, Row: 6, Width: 1,
			Get: func() string { return uitext.BoolToYN(cfg.NUVValidate) },
			Set: func(val string) error { cfg.NUVValidate = uitext.YNToBool(val); return nil },
		},
		{
			Label: "NUV Kill", Help: "Auto-delete user when no threshold reached", Type: ftYesNo, Col: 3, Row: 7, Width: 1,
			Get: func() string { return uitext.BoolToYN(cfg.NUVKill) },
			Set: func(val string) error { cfg.NUVKill = uitext.YNToBool(val); return nil },
		},
		{
			Label: "NUV Level", Help: "Access level assigned to auto-validated NUV users", Type: ftInteger, Col: 3, Row: 8, Width: 3, Min: 0, Max: 255,
			Get: func() string { return strconv.Itoa(cfg.NUVLevel) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.NUVLevel = n
				return nil
			},
		},
		{
			Label: "NUV Form", Help: "Infoform number (1-5) shown to voters; 0 = disabled", Type: ftInteger, Col: 3, Row: 9, Width: 1, Min: 0, Max: 5,
			Get: func() string { return strconv.Itoa(cfg.NUVForm) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				cfg.NUVForm = n
				return nil
			},
		},
	}
}
