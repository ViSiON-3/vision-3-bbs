package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
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
	// TempPath is a scratch directory. Packets are not staged here: each is
	// built or downloaded beside its destination so the final rename never
	// crosses a filesystem.
	TempPath string `json:"tempPath"`
	// DupeDBPath is the JSON file of Message-IDs already imported.
	DupeDBPath string `json:"dupeDbPath"`
	// BadAreaTag names a local message area that receives messages for
	// conferences no area mirrors. Blank drops them with a log line, which
	// is what a hub sending more conferences than the node carries expects.
	BadAreaTag string `json:"badAreaTag,omitempty"`
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
	return net.JoinHostPort(strings.TrimSpace(n.Host), strconv.Itoa(port))
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

// HubIDOwner returns the key of a network other than except whose hub QWK
// ID is hubID, or "" when none is. Packets are named by hub ID in the
// shared inbound and outbound directories, so two networks on one hub ID
// would read each other's REPs and packets.
func (c QWKNetConfig) HubIDOwner(hubID, except string) string {
	id := NormalizeQWKID(hubID)
	if id == "" {
		return ""
	}
	for _, k := range c.NetworkKeys() {
		if k != except && NormalizeQWKID(c.Networks[k].HubID) == id {
			return k
		}
	}
	return ""
}

// ValidateHubIDs reports the first two networks that share a hub QWK ID.
func (c QWKNetConfig) ValidateHubIDs() error {
	for _, k := range c.NetworkKeys() {
		if other := c.HubIDOwner(c.Networks[k].HubID, k); other != "" {
			a, b := k, other
			if b < a {
				a, b = b, a
			}
			return fmt.Errorf("QWK networks %q and %q both use hub ID %s; each network needs its own hub",
				a, b, NormalizeQWKID(c.Networks[k].HubID))
		}
	}
	return nil
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
	// The file holds the hub password. atomicfile replaces the target with
	// a fresh file, so the mode applies even when qwknet.json already
	// existed with broader permissions (a template copied by setup, say).
	if err := atomicfile.WriteFile(filePath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write QWK network config to %s: %w", filePath, err)
	}
	if err := os.Chmod(filePath, 0o600); err != nil {
		return fmt.Errorf("failed to secure QWK network config %s: %w", filePath, err)
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
	// The FTP client refuses the same characters; catching them here means
	// the sysop hears about it while editing, not at the next poll.
	for name, v := range map[string]string{"login name": n.Username, "password": n.Password, "host": n.Host} {
		if strings.ContainsFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return fmt.Errorf("network %q: %s contains a control character", key, name)
		}
	}
	return nil
}
