package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// V3NetConfig holds V3Net networking configuration.
type V3NetConfig struct {
	Enabled      bool   `json:"enabled"`
	KeystorePath string `json:"keystorePath"` // Path to ed25519 keypair file
	DedupDBPath  string `json:"dedupDbPath"`  // Path to dedup SQLite database
	RegistryURL  string `json:"registryUrl"`  // Central registry URL (optional)
	// ConfigPath is the configs directory path. Set at runtime, not persisted.
	ConfigPath string            `json:"-"`
	Hub        V3NetHubConfig    `json:"hub,omitempty"`
	Leaves     []V3NetLeafConfig `json:"leaves,omitempty"`
}

// QWKAPIConfig configures the optional QWK packet transport API (Phase 7).
// Disabled by default. See docs/sysop/messages/qwk-api.md.
type QWKAPIConfig struct {
	Enabled       bool   `json:"enabled"`
	Host          string `json:"host"`          // blank = all interfaces
	Port          int    `json:"port"`          // default 8666
	CertFile      string `json:"certFile"`      // blank = auto self-signed
	KeyFile       string `json:"keyFile"`       // blank = auto self-signed
	TokenTTLHours int    `json:"tokenTTLHours"` // default 24
}

// ListenAddr returns host:port for http.Server, defaulting the port to 8666.
func (c *QWKAPIConfig) ListenAddr() string {
	port := c.Port
	if port == 0 {
		port = 8666
	}
	return net.JoinHostPort(c.Host, strconv.Itoa(port))
}

// TokenTTL returns the bearer-token lifetime, defaulting to 24h.
func (c *QWKAPIConfig) TokenTTL() time.Duration {
	if c.TokenTTLHours <= 0 {
		return 24 * time.Hour
	}
	return time.Duration(c.TokenTTLHours) * time.Hour
}

// V3NetHubConfig configures this node as a V3Net hub.
type V3NetHubConfig struct {
	Enabled      bool              `json:"enabled"`
	Host         string            `json:"host"` // Listen host (blank = all interfaces)
	Port         int               `json:"port"` // Listen port (default: 8765)
	DataDir      string            `json:"dataDir"`
	AutoApprove  bool              `json:"autoApprove"`
	Networks     []V3NetHubNetwork `json:"networks,omitempty"`
	InitialAreas []V3NetHubArea    `json:"initialAreas,omitempty"`
}

// ListenAddr returns the host:port string for net.Listen / http.Server.
func (c *V3NetHubConfig) ListenAddr() string {
	port := c.Port
	if port == 0 {
		port = 8765
	}
	return fmt.Sprintf("%s:%d", c.Host, port)
}

// V3NetHubNetwork defines a network hosted by this hub.
type V3NetHubNetwork struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// V3NetHubArea is an area spec written by the setup wizard and consumed
// once at hub startup to seed the initial NAL.
type V3NetHubArea struct {
	Tag         string `json:"tag"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// V3NetLeafConfig configures a subscription to a V3Net network.
type V3NetLeafConfig struct {
	HubURL        string   `json:"hubUrl"`
	Network       string   `json:"network"`
	Boards        []string `json:"boards"`                  // Local message area tags to write received messages
	PollInterval  string   `json:"pollInterval"`            // Duration string (e.g., "5m")
	Origin        string   `json:"origin,omitempty"`        // Origin line text (e.g. "My Cool BBS - bbs.example.com")
	AutoJoinAreas *bool    `json:"autoJoinAreas,omitempty"` // Flag created areas as newscan defaults (nil = true)
}

// AutoJoinEnabled reports whether message areas created for this leaf
// should be flagged AutoJoin (newscan default). Unset means enabled, so
// existing configs keep today's behavior.
func (l V3NetLeafConfig) AutoJoinEnabled() bool {
	return l.AutoJoinAreas == nil || *l.AutoJoinAreas
}

// LoadV3NetConfig loads V3Net networking configuration from v3net.json.
// Returns a disabled config if the file does not exist.
func LoadV3NetConfig(configPath string) (V3NetConfig, error) {
	filePath := filepath.Join(configPath, "v3net.json")
	slog.Info("loading V3Net configuration", "path", filePath)

	defaultConfig := V3NetConfig{}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("v3net.json not found, V3Net disabled", "path", filePath)
			return defaultConfig, nil
		}
		return defaultConfig, fmt.Errorf("failed to read V3Net config file %s: %w", filePath, err)
	}

	var cfg V3NetConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		slog.Error("failed to parse V3Net config JSON", "path", filePath, "error", err)
		return defaultConfig, fmt.Errorf("failed to parse V3Net config JSON from %s: %w", filePath, err)
	}

	slog.Info("loaded V3Net configuration", "enabled", cfg.Enabled, "hub", cfg.Hub.Enabled, "leaves", len(cfg.Leaves))

	return cfg, nil
}

// SaveV3NetConfig writes the V3NetConfig back to v3net.json in the given configPath directory.
func SaveV3NetConfig(configPath string, cfg V3NetConfig) error {
	filePath := filepath.Join(configPath, "v3net.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal V3Net config: %w", err)
	}
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write V3Net config to %s: %w", filePath, err)
	}
	return nil
}
