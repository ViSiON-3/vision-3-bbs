package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
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
	Origin                string          `json:"origin,omitempty"`        // Origin line text for echomail (empty = board name)
	Links                 []FTNLinkConfig `json:"links"`

	// BinkdOutboundPath overrides the global binkd_outbound_path for this
	// network, becoming both its "domain" line in binkd.conf and the
	// directory its bundles and flow files are packed into. Empty shares the
	// global one.
	//
	// Worth setting whenever two networks are carried. BSO flow files are
	// named from the destination net/node alone with no zone component, so
	// two links in different networks that share a net/node pair resolve to
	// one filename and one network's mail is handed to the other's hub.
	// Separate outbounds are also how binkd itself tells two domains apart.
	BinkdOutboundPath string `json:"binkd_outbound_path,omitempty"`
}

// BinkdServerConfig controls the integrated binkd mailer daemon.
// Zero-valued numeric/string fields are filled with defaults by LoadFTNConfig.
type BinkdServerConfig struct {
	Enabled    bool   `json:"enabled"`                 // Run binkd as a supervised child of the BBS
	Port       int    `json:"port"`                    // binkp listen port (default 24554)
	BinaryPath string `json:"binary_path"`             // Path to binkd binary, relative to BBS root (default "bin/binkd")
	LogLevel   int    `json:"log_level"`               // binkd loglevel (default 4)
	ExportSecs int    `json:"export_interval_seconds"` // Outbound scan/pack cadence (default 300)

	// DisableCramMD5 launches binkd with -m, which both stops it offering
	// CRAM-MD5 to callers and stops it answering a remote's offer, so
	// sessions authenticate with a plaintext password in both directions.
	// Needed for hubs whose CRAM-MD5 rejects an otherwise-correct password;
	// leave off unless a link actually fails that way (see issue #268).
	DisableCramMD5 bool `json:"disable_cram_md5,omitempty"`
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
			warnPointWithoutBossLink(name, net)
		}
	}
	slog.Info("loaded FTN configuration", "networks", len(config.Networks), "tosserEnabled", enabledCount)

	applyBinkdDefaults(&config.Binkd)
	return config, nil
}

// warnPointWithoutBossLink logs a warning when a network posts from a point
// address (e.g. 1337:3/123.1) whose boss node (1337:3/123) is not among its
// links. Inbound echomail arrives from the boss node, so if it is not a link
// the tosser matches nothing and the mail piles up unclaimed — the exact
// misconfiguration behind #276, surfaced here at load instead of silently at
// toss time. Point-numbers and a mismatched boss are common enough to warn,
// not fail: the config still loads.
func warnPointWithoutBossLink(name string, net FTNNetworkConfig) {
	if !pointBossMissing(net) {
		return
	}
	own, _ := jam.ParseAddress(net.OwnAddress)
	slog.Warn("ftn network posts from a point but its boss node is not a link — "+
		"inbound mail from the boss will match no link and pile up unclaimed; add it under Echomail Links",
		"network", name,
		"own_address", net.OwnAddress,
		"boss_node", fmt.Sprintf("%d:%d/%d", own.Zone, own.Net, own.Node))
}

// pointBossMissing reports whether the network's own_address is a point whose
// boss node matches none of its links. Link matching compares zone/net/node
// and ignores the point, the same way the tosser matches an inbound packet's
// origin, so a link recorded as another point of the boss node still counts.
func pointBossMissing(net FTNNetworkConfig) bool {
	own, err := jam.ParseAddress(net.OwnAddress)
	if err != nil || own.Point == 0 {
		return false // not a point, or unparseable (validated elsewhere)
	}
	for _, l := range net.Links {
		la, err := jam.ParseAddress(l.Address)
		if err == nil && la.Zone == own.Zone && la.Net == own.Net && la.Node == own.Node {
			return false // the boss node is a configured link
		}
	}
	return true
}

// ValidateFTNConfig checks that all required global path fields are set for any
// network that has internal_tosser_enabled=true, and that every binkd outbound
// name is one binkd will accept. Call this before starting the tosser, not
// during editing, so the config editor can open an incomplete config.
func ValidateFTNConfig(cfg FTNConfig) error {
	// The outbound names are checked whether or not any tosser is enabled:
	// binkd consumes them regardless, and it is binkd that refuses to start on
	// a dotted one (see ValidateBinkdOutboundPaths).
	if err := ValidateBinkdOutboundPaths(cfg); err != nil {
		return err
	}
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

// ValidateBinkdOutboundPaths checks the global and every per-network
// binkd_outbound_path with ValidateBinkdOutboundPath. It is separate from
// ValidateFTNConfig because a failure here is fatal to binkd specifically: the
// mailer must refuse to launch binkd on one, where a missing tosser path only
// disables the export loop.
func ValidateBinkdOutboundPaths(cfg FTNConfig) error {
	if err := ValidateBinkdOutboundPath(cfg.BinkdOutboundPath); err != nil {
		return fmt.Errorf("ftn.json: binkd_outbound_path: %w", err)
	}
	// Sorted so the same bad config always reports the same network first.
	names := make([]string, 0, len(cfg.Networks))
	for name := range cfg.Networks {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := ValidateBinkdOutboundPath(cfg.Networks[name].BinkdOutboundPath); err != nil {
			return fmt.Errorf("ftn.json: network %q: binkd_outbound_path: %w", name, err)
		}
	}
	return nil
}

// ValidateBinkdOutboundPath rejects a BSO outbound whose final path component
// carries an extension. binkd refuses to start on one — "there should be no
// extension for the base outbound name" — because it reserves that suffix for
// zone outbounds, deriving them by appending the zone as lowercase hex
// (out.016 for zone 22). A dotted base would collide with that scheme.
//
// Validated rather than silently corrected because the value is written
// straight into binkd.conf: an unusable path there does not fail the save, it
// crash-loops binkd afterwards with all mail stopped, and the cause shows up
// only in the mailer's stderr.
func ValidateBinkdOutboundPath(configured string) error {
	if strings.TrimSpace(configured) == "" {
		return nil // unset falls back to the default, which is always valid
	}
	base := filepath.Base(filepath.Clean(configured))
	if strings.Contains(base, ".") {
		return fmt.Errorf("directory name %q must not contain a dot — binkd rejects an extension "+
			"on the base outbound name (it reserves .<zone> for zone outbounds); use %q instead",
			base, strings.ReplaceAll(base, ".", "_"))
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
	// Per-network outbound overrides resolve the same way. Networks is a map
	// of values, so each entry has to be written back.
	for name, netCfg := range c.Networks {
		if netCfg.BinkdOutboundPath == "" {
			continue
		}
		netCfg.BinkdOutboundPath = resolve(netCfg.BinkdOutboundPath)
		c.Networks[name] = netCfg
	}
}

// BinkdOutboundFor returns the BSO outbound directory this network's bundles
// belong in: its own override when set, otherwise the global one. Both are
// returned as configured (relative or absolute) — resolve with
// ftn.BinkdOutboundDir, or call after ResolvePaths.
func (c FTNConfig) BinkdOutboundFor(network string) string {
	if netCfg, ok := c.Networks[network]; ok && netCfg.BinkdOutboundPath != "" {
		return netCfg.BinkdOutboundPath
	}
	// The map is keyed as the sysop wrote it; binkd.conf domain names are
	// lower-cased, so fall back to a case-insensitive match.
	for name, netCfg := range c.Networks {
		if strings.EqualFold(name, network) && netCfg.BinkdOutboundPath != "" {
			return netCfg.BinkdOutboundPath
		}
	}
	return c.BinkdOutboundPath
}

// NetworkOrigins collects each network's origin-line override, keyed by
// lower-cased network name. Networks with no origin set are omitted; an
// empty result returns nil. Shared by startup and the ftn.json reload so
// both build the same map.
func (c FTNConfig) NetworkOrigins() map[string]string {
	origins := make(map[string]string)
	for name, netCfg := range c.Networks {
		if strings.TrimSpace(netCfg.Origin) == "" {
			continue
		}
		origins[strings.ToLower(strings.TrimSpace(name))] = netCfg.Origin
	}
	if len(origins) == 0 {
		return nil
	}
	return origins
}
