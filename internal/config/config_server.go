package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// ServerConfig defines server-wide settings
type ServerConfig struct {
	BoardName           string `json:"boardName"`
	SysOpName           string `json:"sysOpName"`
	QWKID               string `json:"qwkID,omitempty"` // Explicit QWK packet ID; blank = derive from BoardName
	BBSLocation         string `json:"bbsLocation,omitempty"`
	Timezone            string `json:"timezone,omitempty"`
	SysOpLevel          int    `json:"sysOpLevel"`
	CoSysOpLevel        int    `json:"coSysOpLevel"`
	WFCEnabled          bool   `json:"wfcEnabled"`     // Allow remote WFC sysop console (wfc-admin subsystem)
	InvisibleLevel      int    `json:"invisibleLevel"` // Access level for invisible logon prompt; 0 = use coSysOpLevel
	NewUserLevel        int    `json:"newUserLevel"`   // Access level assigned to new signups
	RegularUserLevel    int    `json:"regularUserLevel"`
	LogonLevel          int    `json:"logonLevel"`
	AnonymousLevel      int    `json:"anonymousLevel"`
	SSHPort             int    `json:"sshPort"`
	SSHHost             string `json:"sshHost"`
	SSHEnabled          bool   `json:"sshEnabled"`
	TelnetPort          int    `json:"telnetPort"`
	TelnetHost          string `json:"telnetHost"`
	TelnetEnabled       bool   `json:"telnetEnabled"`
	MaxNodes            int    `json:"maxNodes"`
	MaxConnectionsPerIP int    `json:"maxConnectionsPerIP"`
	IPBlocklistPath     string `json:"ipBlocklistPath"`
	IPAllowlistPath     string `json:"ipAllowlistPath"`
	MaxFailedLogins     int    `json:"maxFailedLogins"`
	LockoutMinutes      int    `json:"lockoutMinutes"`
	FileListingMode     string `json:"fileListingMode"`
	LegacySSHAlgorithms bool   `json:"legacySSHAlgorithms"`
	AllowNewUsers       bool   `json:"allowNewUsers"`

	// Challenge Gate — optional pre-login bot challenge (botgate-style).
	EnableChallengeGate          bool   `json:"enableChallengeGate"`          // master on/off
	ChallengeGateFile            string `json:"challengeGateFile"`            // art file in menus ansi dir
	ChallengeGateKey             string `json:"challengeGateKey"`             // "ESC" or a single character
	ChallengeGateTimeoutSeconds  int    `json:"challengeGateTimeoutSeconds"`  // seconds to complete the challenge
	ChallengeGateRequiredPresses int    `json:"challengeGateRequiredPresses"` // presses of the key to pass
	ChallengeGateLiveCountdown   bool   `json:"challengeGateLiveCountdown"`   // live per-second countdown vs static

	// Connection-rate limiter — temp-ban IPs that reconnect too rapidly.
	EnableConnRateLimit        bool `json:"enableConnRateLimit"`
	ConnRateLimitHits          int  `json:"connRateLimitHits"`          // attempts within window that trigger a ban
	ConnRateLimitWindowSeconds int  `json:"connRateLimitWindowSeconds"` // sliding window size
	ConnRateLimitBanMinutes    int  `json:"connRateLimitBanMinutes"`    // temp-ban duration

	// Idle timeout (0 = disabled). Applied across the entire app; any input loop
	// that calls ReadKeyWithTimeout uses this value.
	SessionIdleTimeoutMinutes int `json:"sessionIdleTimeoutMinutes"`

	// Transfer timeout in minutes for file transfers (ZModem, etc.). 0 = no timeout.
	// When exceeded, the transfer process is killed and the session returns to the BBS.
	TransferTimeoutMinutes int `json:"transferTimeoutMinutes"`

	// DataDir is the runtime data directory (set by main at startup, not from JSON).
	DataDir string `json:"-"`

	// Number of days to retain soft-deleted user accounts before they are eligible
	// for permanent purge. 0 = purge immediately; -1 = never purge automatically.
	DeletedUserRetentionDays int `json:"deletedUserRetentionDays"`

	// New User Voting (NUV) — community-based new user approval (V2 NUV system).
	UseNUV      bool `json:"useNuv"`      // enable NUV system
	AutoAddNUV  bool `json:"autoAddNuv"`  // automatically add new registrants to NUV queue
	NUVUseLevel int  `json:"nuvUseLevel"` // minimum access level required to vote
	NUVYesVotes int  `json:"nuvYesVotes"` // yes votes required to auto-validate
	NUVNoVotes  int  `json:"nuvNoVotes"`  // no votes required to auto-delete
	NUVValidate bool `json:"nuvValidate"` // auto-validate user when yes threshold reached
	NUVKill     bool `json:"nuvKill"`     // auto-delete user when no threshold reached
	NUVLevel    int  `json:"nuvLevel"`    // access level assigned on NUV auto-validation
	NUVForm     int  `json:"nuvForm"`     // infoform number (1-5) to display during NUV voting; 0 = disabled

	// DOS emulation — system-wide dosemu2 settings for DOS door games.
	DosemuPath string `json:"dosemuPath,omitempty"` // Path to dosemu2 binary (default: /usr/libexec/dosemu2/dosemu2.bin)

	// QWKAPI configures the optional QWK packet transport HTTP API (Phase 7).
	// Disabled by default.
	QWKAPI QWKAPIConfig `json:"qwkAPI"`

	// Logging configuration (file location, level, caching, rolling). Shared by
	// all binaries; each supplies its own default log filename in code.
	Logging LoggingConfig `json:"logging"`
}

// Log rolling types persisted in LoggingConfig.Type.
const (
	LogTypeNone  = 0 // append to a single file indefinitely
	LogTypeSize  = 1 // roll by size, keeping MaxFiles numbered backups
	LogTypeDaily = 2 // roll daily, keeping MaxFiles days of dated files
)

// Default logging values, applied when the "logging" key is absent or a field
// is left at its zero value.
const (
	DefaultLogDir       = "data/logs"
	DefaultLogLevel     = "INFO"
	DefaultLogMaxFiles  = 5
	DefaultLogMaxSizeKB = 1024 // 1 MB
)

// LoggingConfig controls log file location, minimum level, write caching, and
// rolling behavior. It is persisted under the "logging" key in config.json.
type LoggingConfig struct {
	Dir       string `json:"dir"`       // directory for log files (default "data/logs")
	Level     string `json:"level"`     // DEBUG|INFO|WARN|ERROR minimum level (default INFO)
	Cache     bool   `json:"cache"`     // buffer writes in an 8KB cache (default true)
	Type      int    `json:"type"`      // 0=none 1=size 2=daily (default 0)
	MaxFiles  int    `json:"maxFiles"`  // retained backups (type 1) / days (type 2)
	MaxSizeKB int    `json:"maxSizeKB"` // rotate threshold in KB (type 1)
}

// Normalize fills empty strings with defaults and clamps numeric fields to safe
// lower bounds so a config typo (e.g. MaxSizeKB=0) cannot cause immediate log
// loss or rotate-on-every-write. The level string is validated separately by
// the logging package at Init time.
func (c *LoggingConfig) Normalize() {
	if strings.TrimSpace(c.Dir) == "" {
		c.Dir = DefaultLogDir
	}
	if strings.TrimSpace(c.Level) == "" {
		c.Level = DefaultLogLevel
	}
	// MaxFiles bounds the number of retained backups (type 1) or days (type 2);
	// it must be positive whenever rolling is enabled.
	if c.Type != LogTypeNone && c.MaxFiles < 1 {
		c.MaxFiles = DefaultLogMaxFiles
	}
	// MaxSizeKB only applies to size-based rolling; a non-positive threshold
	// would rotate on every write.
	if c.Type == LogTypeSize && c.MaxSizeKB < 1 {
		c.MaxSizeKB = DefaultLogMaxSizeKB
	}
}

// LoadServerConfig loads the server configuration from config.json
func LoadServerConfig(configPath string) (ServerConfig, error) {
	filePath := filepath.Join(configPath, "config.json")
	slog.Info("loading server configuration", "path", filePath)

	// Default config values
	defaultConfig := ServerConfig{
		BoardName:                    "ViSiON/3 BBS",
		Timezone:                     "",
		SysOpLevel:                   255,
		CoSysOpLevel:                 250,
		WFCEnabled:                   true,
		NewUserLevel:                 1,
		RegularUserLevel:             10,
		LogonLevel:                   10,
		AnonymousLevel:               5,
		SSHPort:                      2222,
		SSHHost:                      "0.0.0.0",
		SSHEnabled:                   true,
		TelnetPort:                   2323,
		TelnetHost:                   "0.0.0.0",
		TelnetEnabled:                false,
		MaxNodes:                     10,
		MaxConnectionsPerIP:          3,
		MaxFailedLogins:              5,
		LockoutMinutes:               30,
		AllowNewUsers:                true,
		EnableChallengeGate:          false,
		ChallengeGateFile:            "BOTCHECK.ASC",
		ChallengeGateKey:             "ESC",
		ChallengeGateTimeoutSeconds:  20,
		ChallengeGateRequiredPresses: 2,
		ChallengeGateLiveCountdown:   true,
		EnableConnRateLimit:          false,
		ConnRateLimitHits:            20,
		ConnRateLimitWindowSeconds:   10,
		ConnRateLimitBanMinutes:      90,
		SessionIdleTimeoutMinutes:    5,
		TransferTimeoutMinutes:       10,
		LegacySSHAlgorithms:          true,
		DeletedUserRetentionDays:     30,
		UseNUV:                       false,
		AutoAddNUV:                   false,
		NUVUseLevel:                  25,
		NUVYesVotes:                  5,
		NUVNoVotes:                   5,
		NUVValidate:                  true,
		NUVKill:                      false,
		NUVLevel:                     25,
		NUVForm:                      1,
		Logging: LoggingConfig{
			Dir:       DefaultLogDir,
			Level:     DefaultLogLevel,
			Cache:     true,
			Type:      LogTypeNone,
			MaxFiles:  DefaultLogMaxFiles,
			MaxSizeKB: DefaultLogMaxSizeKB,
		},
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Warn("config.json not found, using default settings", "path", filePath)
			return defaultConfig, nil
		}
		return defaultConfig, fmt.Errorf("failed to read config file %s: %w", filePath, err)
	}

	// Initialize with defaults before unmarshalling
	config := defaultConfig
	err = json.Unmarshal(data, &config)
	if err != nil {
		slog.Error("failed to parse config JSON, using default settings", "path", filePath, "error", err)
		return defaultConfig, fmt.Errorf("failed to parse config JSON from %s: %w", filePath, err)
	}

	config.SanitizeChallengeGate()
	slog.Info("loaded server configuration", "path", filePath)
	return config, nil
}

// SanitizeChallengeGate fills invalid/zero Challenge Gate values with safe
// defaults, and also clamps the connection-rate-limit fields. Called after
// load so a hand-edited config.json can't disable the gate through
// out-of-range numbers or empty required strings. Note this does not
// guarantee the connection-rate limiter stays enabled: a negative
// ConnRateLimitHits clamps to 0, and 0 (or lower) hits is treated as
// "disabled" by SetConnRateLimit.
func (c *ServerConfig) SanitizeChallengeGate() {
	if c.ChallengeGateFile == "" {
		c.ChallengeGateFile = "BOTCHECK.ASC"
	}
	if c.ChallengeGateKey == "" {
		c.ChallengeGateKey = "ESC"
	}
	if c.ChallengeGateTimeoutSeconds < 1 {
		c.ChallengeGateTimeoutSeconds = 20
	}
	if c.ChallengeGateRequiredPresses < 1 {
		c.ChallengeGateRequiredPresses = 2
	}
	if c.ConnRateLimitHits < 0 {
		c.ConnRateLimitHits = 0
	}
	if c.ConnRateLimitWindowSeconds < 1 {
		c.ConnRateLimitWindowSeconds = 10
	}
	if c.ConnRateLimitBanMinutes < 1 {
		c.ConnRateLimitBanMinutes = 90
	}
}

// SaveServerConfig writes the ServerConfig back to config.json in the given configPath directory.
func SaveServerConfig(configPath string, cfg ServerConfig) error {
	filePath := filepath.Join(configPath, "config.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal server config: %w", err)
	}
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", filePath, err)
	}
	slog.Info("server configuration saved", "path", filePath)
	return nil
}
