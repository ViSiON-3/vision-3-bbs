package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// QWKNetConfig is the root of configs/qwknet.json: the shared working
// directories and the QWK networks this system is a node of.
//
// A QWK network (QWKnet) is a hub-and-node message network that moves mail
// in ordinary QWK/REP packets: the node uploads a REP packet of its new
// messages to the hub and downloads a QWK packet of everything the hub has
// collected since the last call. The transport is FTP, which is what
// Synchronet-hosted networks such as DOVE-Net expect. A node's identity on
// the hub is its QWK ID, which is also the hub-side user name.
type QWKNetConfig struct {
	// InboundPath receives downloaded QWK packets waiting to be tossed.
	InboundPath string `json:"inboundPath"`
	// OutboundPath holds packed REP files waiting to be uploaded.
	OutboundPath string `json:"outboundPath"`
	// TempPath is scratch space for building and unpacking packets.
	TempPath string `json:"tempPath"`
	// DupeDBPath is the JSON file of Message-IDs already imported.
	DupeDBPath string `json:"dupeDbPath"`
	// Networks is keyed by the network key (e.g. "dovenet"), which is also
	// what message areas name in their Network field.
	Networks map[string]QWKNetworkConfig `json:"networks"`
}

// QWKNetworkConfig describes one hub this system polls as a node.
type QWKNetworkConfig struct {
	Enabled bool `json:"enabled"`
	// Name is the display name (e.g. "DOVE-Net").
	Name string `json:"name"`
	// HubID is the hub's QWK ID (e.g. "VERT"). It names the packets on the
	// wire: the node uploads <HubID>.REP and downloads <HubID>.QWK.
	HubID string `json:"hubId"`
	// OwnID is this node's QWK ID on the hub. Blank uses the system QWK ID
	// (config.json qwkID, or one derived from the board name).
	OwnID string `json:"ownId,omitempty"`
	// Host and Port locate the hub's FTP server. Port 0 means 21.
	Host string `json:"host"`
	Port int    `json:"port,omitempty"`
	// Username is the FTP login. Blank uses OwnID, which is how Synchronet
	// hubs identify a node.
	Username string `json:"username,omitempty"`
	Password string `json:"password"`
	// Tagline is appended below the tearline of every exported message,
	// the way QWK networks expect a node to identify itself.
	Tagline string `json:"tagline,omitempty"`
	// NoHeaders drops HEADERS.DAT from outbound REP packets. Leave it off:
	// HEADERS.DAT carries full-length names, Message-IDs and time zones.
	NoHeaders bool `json:"noHeaders,omitempty"`
	// TimeoutSeconds bounds each FTP transfer. 0 means 300.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
}

// HostPort returns host:port for dialing, with the FTP default port.
func (n QWKNetworkConfig) HostPort() string {
	port := n.Port
	if port <= 0 {
		port = 21
	}
	return fmt.Sprintf("%s:%d", strings.TrimSpace(n.Host), port)
}

// LoginUser returns the FTP user name: the configured one, else the node's
// QWK ID resolved by the caller.
func (n QWKNetworkConfig) LoginUser(ownID string) string {
	if u := strings.TrimSpace(n.Username); u != "" {
		return u
	}
	return ownID
}

// NodeID returns this node's QWK ID for the network, falling back to the
// system-wide ID when the network sets none.
func (n QWKNetworkConfig) NodeID(systemID string) string {
	if id := NormalizeQWKID(n.OwnID); id != "" {
		return id
	}
	return NormalizeQWKID(systemID)
}

// Default working directories, relative to the BBS root.
const (
	DefaultQWKNetInboundPath  = "data/qwknet/in"
	DefaultQWKNetOutboundPath = "data/qwknet/out"
	DefaultQWKNetTempPath     = "data/qwknet/temp"
	DefaultQWKNetDupeDBPath   = "data/qwknet/dupes.json"
)

// ApplyDefaults fills blank paths with the standard layout.
func (c *QWKNetConfig) ApplyDefaults() {
	if c.InboundPath == "" {
		c.InboundPath = DefaultQWKNetInboundPath
	}
	if c.OutboundPath == "" {
		c.OutboundPath = DefaultQWKNetOutboundPath
	}
	if c.TempPath == "" {
		c.TempPath = DefaultQWKNetTempPath
	}
	if c.DupeDBPath == "" {
		c.DupeDBPath = DefaultQWKNetDupeDBPath
	}
	if c.Networks == nil {
		c.Networks = make(map[string]QWKNetworkConfig)
	}
}

// ResolvePaths makes the working directories absolute under root.
func (c *QWKNetConfig) ResolvePaths(root string) {
	c.ApplyDefaults()
	resolve := func(p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(root, p)
	}
	c.InboundPath = resolve(c.InboundPath)
	c.OutboundPath = resolve(c.OutboundPath)
	c.TempPath = resolve(c.TempPath)
	c.DupeDBPath = resolve(c.DupeDBPath)
}

// NetworkKeys returns the configured network keys, sorted.
func (c QWKNetConfig) NetworkKeys() []string {
	keys := make([]string, 0, len(c.Networks))
	for k := range c.Networks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// LoadQWKNetConfig reads configs/qwknet.json. A missing file means no QWK
// networks are configured and is not an error.
func LoadQWKNetConfig(configPath string) (QWKNetConfig, error) {
	filePath := filepath.Join(configPath, "qwknet.json")
	var cfg QWKNetConfig
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			cfg.ApplyDefaults()
			return cfg, nil
		}
		return cfg, fmt.Errorf("failed to read QWK network config %s: %w", filePath, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("failed to parse QWK network config %s: %w", filePath, err)
	}
	cfg.ApplyDefaults()
	slog.Info("loaded QWK network configuration", "networks", len(cfg.Networks))
	return cfg, nil
}

// SaveQWKNetConfig writes configs/qwknet.json.
func SaveQWKNetConfig(configPath string, cfg QWKNetConfig) error {
	filePath := filepath.Join(configPath, "qwknet.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal QWK network config: %w", err)
	}
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write QWK network config to %s: %w", filePath, err)
	}
	return nil
}

// ValidateQWKNetwork reports the first thing that would stop a network from
// being polled. systemID is the system-wide QWK ID used when OwnID is blank.
func ValidateQWKNetwork(key string, n QWKNetworkConfig, systemID string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("network key cannot be empty")
	}
	if NormalizeQWKID(n.HubID) == "" {
		return fmt.Errorf("network %q: hub QWK ID is required", key)
	}
	if n.NodeID(systemID) == "" {
		return fmt.Errorf("network %q: this system has no QWK ID (set qwkID in config.json or ownId for the network)", key)
	}
	if strings.TrimSpace(n.Host) == "" {
		return fmt.Errorf("network %q: hub host is required", key)
	}
	if n.Port < 0 || n.Port > 65535 {
		return fmt.Errorf("network %q: port must be 1-65535", key)
	}
	if n.Password == "" {
		return fmt.Errorf("network %q: hub password is required", key)
	}
	return nil
}
