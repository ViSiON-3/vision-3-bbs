package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// FTNLinkConfig defines an FTN link (uplink/downlink node).
// Echo area routing is derived from message_areas.json (areas where Network matches),
// not stored per-link. The Message Areas TUI is the canonical place to manage subscriptions.
type FTNLinkConfig struct {
	Address         string `json:"address"`                    // e.g., "21:1/100"
	PacketPassword  string `json:"packet_password"`            // Packet password (formerly "password")
	SessionPassword string `json:"session_password,omitempty"` // BinkP session/connection password
	AreafixPassword string `json:"areafix_password,omitempty"` // Password for AreaFix netmail (subject line)
	Name            string `json:"name"`                       // Human-readable name
	Flavour         string `json:"flavour,omitempty"`          // Delivery flavour: Normal (default), Crash, Hold, Direct
	Hostname        string `json:"hostname,omitempty"`         // Hub BinkP hostname; source of truth for the binkd.conf node line
	Port            int    `json:"port,omitempty"`             // Hub BinkP port (default 24554 when Hostname is set)
}

// HostPort returns "hostname:port" for the link, defaulting the port to
// 24554. Empty when no hostname is configured.
func (c FTNLinkConfig) HostPort() string {
	if c.Hostname == "" {
		return ""
	}
	port := c.Port
	if port <= 0 {
		port = 24554
	}
	return fmt.Sprintf("%s:%d", c.Hostname, port)
}

// UnmarshalJSON supports backward compatibility: "password" is read into PacketPassword
// when packet_password is absent (nil pointer = field omitted vs explicitly empty string).
func (c *FTNLinkConfig) UnmarshalJSON(data []byte) error {
	var r struct {
		Address         string  `json:"address"`
		PacketPassword  *string `json:"packet_password"`
		SessionPassword string  `json:"session_password,omitempty"`
		AreafixPassword string  `json:"areafix_password,omitempty"`
		Name            string  `json:"name"`
		Flavour         string  `json:"flavour,omitempty"`
		Hostname        string  `json:"hostname,omitempty"`
		Port            int     `json:"port,omitempty"`
		LegacyPassword  string  `json:"password"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	c.Address = r.Address
	c.SessionPassword = r.SessionPassword
	c.AreafixPassword = r.AreafixPassword
	c.Name = r.Name
	c.Flavour = r.Flavour
	c.Hostname = r.Hostname
	c.Port = r.Port
	if r.PacketPassword != nil {
		c.PacketPassword = *r.PacketPassword
	} else if r.LegacyPassword != "" {
		c.PacketPassword = r.LegacyPassword
	}
	return nil
}

// FTNNetworkConfig holds settings for a single FTN network (e.g., FSXNet, FidoNet).
// Netmail routing is derived from message_areas.json (areas where Network matches and AreaType == "netmail").
type FTNNetworkConfig struct {
	InternalTosserEnabled bool            `json:"internal_tosser_enabled"` // Enable internal tosser
	OwnAddress            string          `json:"own_address"`             // e.g., "21:4/158.1"
	PollSeconds           int             `json:"poll_interval_seconds"`   // 0 = manual only (v3mail toss/scan)
	Tearline              string          `json:"tearline,omitempty"`      // Custom tearline text for echomail
	Links                 []FTNLinkConfig `json:"links"`
}

// BinkdServerConfig controls the integrated binkd mailer daemon.
// Zero-valued numeric/string fields are filled with defaults by LoadFTNConfig.
type BinkdServerConfig struct {
	Enabled    bool   `json:"enabled"`                 // Run binkd as a supervised child of the BBS
	Port       int    `json:"port"`                    // binkp listen port (default 24554)
	BinaryPath string `json:"binary_path"`             // Path to binkd binary, relative to BBS root (default "bin/binkd")
	LogLevel   int    `json:"log_level"`               // binkd loglevel (default 4)
	ExportSecs int    `json:"export_interval_seconds"` // Outbound scan/pack cadence (default 300)
}

// FTNConfig holds all FTN (FidoNet Technology Network) echomail settings.
// Loaded from configs/ftn.json.
type FTNConfig struct {
	DupeDBPath        string                      `json:"dupe_db_path"`                  // e.g., "data/ftn/dupes.json"
	InboundPath       string                      `json:"inbound_path"`                  // Where binkd deposits received bundles
	SecureInboundPath string                      `json:"secure_inbound_path,omitempty"` // Authenticated inbound
	OutboundPath      string                      `json:"outbound_path"`                 // Staging dir for outbound .PKT files
	BinkdOutboundPath string                      `json:"binkd_outbound_path"`           // Binkd outbound dir for ZIP bundles
	TempPath          string                      `json:"temp_path"`                     // Temp dir for processing
	BadAreaTag        string                      `json:"bad_area_tag,omitempty"`        // Area for unroutable messages (e.g., "BAD")
	DupeAreaTag       string                      `json:"dupe_area_tag,omitempty"`       // Area for duplicate messages (e.g., "DUPE")
	Binkd             BinkdServerConfig           `json:"binkd"`                         // Integrated binkd mailer daemon
	Networks          map[string]FTNNetworkConfig `json:"networks"`
}

// applyBinkdDefaults fills zero-valued BinkdServerConfig fields with defaults.
func applyBinkdDefaults(c *BinkdServerConfig) {
	if c.Port == 0 {
		c.Port = 24554
	}
	if c.BinaryPath == "" {
		c.BinaryPath = "bin/binkd"
	}
	if c.LogLevel == 0 {
		c.LogLevel = 4
	}
	if c.ExportSecs == 0 {
		c.ExportSecs = 300
	}
}

// LoadFTNConfig loads FTN network configuration from ftn.json.
// Returns an empty config (no networks) if the file does not exist.
func LoadFTNConfig(configPath string) (FTNConfig, error) {
	filePath := filepath.Join(configPath, "ftn.json")
	slog.Info("loading FTN configuration", "path", filePath)

	defaultConfig := FTNConfig{
		Networks: make(map[string]FTNNetworkConfig),
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("ftn.json not found, FTN disabled", "path", filePath)
			applyBinkdDefaults(&defaultConfig.Binkd)
			return defaultConfig, nil
		}
		return defaultConfig, fmt.Errorf("failed to read FTN config file %s: %w", filePath, err)
	}

	var config FTNConfig
	err = json.Unmarshal(data, &config)
	if err != nil {
		slog.Error("failed to parse FTN config JSON", "path", filePath, "error", err)
		return defaultConfig, fmt.Errorf("failed to parse FTN config JSON from %s: %w", filePath, err)
	}

	if config.Networks == nil {
		config.Networks = make(map[string]FTNNetworkConfig)
	}

	enabledCount := 0
	for name, net := range config.Networks {
		if net.InternalTosserEnabled {
			enabledCount++
			slog.Info("ftn network internal tosser enabled", "network", name, "address", net.OwnAddress)
		}
	}
	slog.Info("loaded FTN configuration", "networks", len(config.Networks), "tosserEnabled", enabledCount)

	applyBinkdDefaults(&config.Binkd)
	return config, nil
}

// ValidateFTNConfig checks that all required global path fields are set for any
// network that has internal_tosser_enabled=true. Call this before starting the
// tosser, not during editing, so the config editor can open an incomplete config.
func ValidateFTNConfig(cfg FTNConfig) error {
	tosserEnabled := false
	for _, net := range cfg.Networks {
		if net.InternalTosserEnabled {
			tosserEnabled = true
			break
		}
	}
	if !tosserEnabled {
		return nil
	}
	type requiredPath struct {
		field string
		value string
	}
	required := []requiredPath{
		{"inbound_path", cfg.InboundPath},
		{"outbound_path", cfg.OutboundPath},
		{"binkd_outbound_path", cfg.BinkdOutboundPath},
		{"temp_path", cfg.TempPath},
	}
	for _, r := range required {
		if strings.TrimSpace(r.value) == "" {
			return fmt.Errorf("ftn.json: %q is required when internal_tosser_enabled is true", r.field)
		}
	}
	return nil
}

// ResolvePaths makes the FTN path fields absolute by joining relative paths
// against root (the BBS root directory). Empty and absolute paths are unchanged.
func (c *FTNConfig) ResolvePaths(root string) {
	resolve := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(root, p)
	}
	c.InboundPath = resolve(c.InboundPath)
	c.SecureInboundPath = resolve(c.SecureInboundPath)
	c.OutboundPath = resolve(c.OutboundPath)
	c.BinkdOutboundPath = resolve(c.BinkdOutboundPath)
	c.TempPath = resolve(c.TempPath)
	if c.DupeDBPath != "" {
		c.DupeDBPath = resolve(c.DupeDBPath)
	}
}
