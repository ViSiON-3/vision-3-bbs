package config

import (
	"encoding/json"
	"fmt"
	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
	"log/slog"
	"net/netip"
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
	TICPassword     string `json:"tic_password,omitempty"`     // Password inbound TIC files from this link must carry (Pw line); empty = accepted only from the secure inbound
	Name            string `json:"name"`                       // Human-readable name
	Flavour         string `json:"flavour,omitempty"`          // Delivery flavour: Normal (default), Crash, Hold, Direct
	Hostname        string `json:"hostname,omitempty"`         // Hub BinkP hostname; source of truth for the binkd.conf node line
	Port            int    `json:"port,omitempty"`             // Hub BinkP port (default 24554 when Hostname is set)
	IPFamily        string `json:"ip_family,omitempty"`        // Address family binkd calls the hub over: "" (auto), "ipv4" or "ipv6"
}

// Address families a link can be called over (FTNLinkConfig.IPFamily).
const (
	IPFamilyAuto = ""     // whatever the hostname resolves to, binkd's default
	IPFamilyIPv4 = "ipv4" // IPv4 only: binkd's "-4" node option
	IPFamilyIPv6 = "ipv6" // IPv6 only: binkd's "-6" node option
)

// NormalizeIPFamily maps a configured address family to one of the IPFamily
// constants, case-insensitively. ok is false for a value that is none of them,
// which normalizes to IPFamilyAuto.
func NormalizeIPFamily(s string) (fam string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return IPFamilyAuto, true
	case "ipv4", "4":
		return IPFamilyIPv4, true
	case "ipv6", "6":
		return IPFamilyIPv6, true
	}
	return IPFamilyAuto, false
}

// ValidateLinkIPFamily rejects a family the hostname cannot be reached over:
// an IPv6 literal forced to IPv4, or an IPv4 literal forced to IPv6. binkd
// would never connect, and only say so in its log. A DNS name is always
// accepted, since which records it has is only known when binkd resolves it.
func ValidateLinkIPFamily(hostname, fam string) error {
	ip, ok := parseIPLiteral(hostname)
	if !ok {
		return nil
	}
	// An IPv4-mapped literal ("::ffff:192.0.2.1") is an IPv4 host.
	is4 := ip.Unmap().Is4()
	switch {
	case fam == IPFamilyIPv4 && !is4:
		return fmt.Errorf("%s is an IPv6 address and cannot be reached over IPv4", hostname)
	case fam == IPFamilyIPv6 && is4:
		return fmt.Errorf("%s is an IPv4 address and cannot be reached over IPv6", hostname)
	}
	return nil
}

// parseIPLiteral parses a hostname that is an IP address, bracketed or not.
// netip, not net.ParseIP: a link-local IPv6 address needs its zone
// ("fe80::1%eth0"), and net.ParseIP rejects one, which would let it pass for
// a DNS name.
func parseIPLiteral(hostname string) (netip.Addr, bool) {
	h := strings.TrimSpace(hostname)
	if strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
		h = h[1 : len(h)-1]
	}
	ip, err := netip.ParseAddr(h)
	return ip, err == nil
}

// HostPort returns "hostname:port" for the link, defaulting the port to
// 24554. Empty when no hostname is configured. An IPv6 literal is bracketed
// ("[2001:db8::1]:24554"), the form binkd needs to tell the port from the
// address; written bare, binkd takes the address's last group for the port.
func (c FTNLinkConfig) HostPort() string {
	return JoinBinkpHostPort(c.Hostname, c.Port)
}

// JoinBinkpHostPort renders a BinkP host and port for a binkd node line,
// defaulting the port to 24554 and bracketing an IPv6 literal. Empty when
// host is empty.
func JoinBinkpHostPort(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if port <= 0 {
		port = 24554
	}
	// By its colons, not by family: an IPv4-mapped literal
	// ("::ffff:192.0.2.1") is an IPv4 host but is still written with colons.
	// The zone of a link-local address stays inside the brackets.
	if !strings.HasPrefix(host, "[") && strings.Contains(host, ":") {
		if _, ok := parseIPLiteral(host); ok {
			host = "[" + host + "]"
		}
	}
	return fmt.Sprintf("%s:%d", host, port)
}

// UnmarshalJSON supports backward compatibility: "password" is read into PacketPassword
// when packet_password is absent (nil pointer = field omitted vs explicitly empty string).
func (c *FTNLinkConfig) UnmarshalJSON(data []byte) error {
	var r struct {
		Address         string  `json:"address"`
		PacketPassword  *string `json:"packet_password"`
		SessionPassword string  `json:"session_password,omitempty"`
		AreafixPassword string  `json:"areafix_password,omitempty"`
		TICPassword     string  `json:"tic_password,omitempty"`
		Name            string  `json:"name"`
		Flavour         string  `json:"flavour,omitempty"`
		Hostname        string  `json:"hostname,omitempty"`
		Port            int     `json:"port,omitempty"`
		IPFamily        string  `json:"ip_family,omitempty"`
		LegacyPassword  string  `json:"password"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	c.Address = r.Address
	c.SessionPassword = r.SessionPassword
	c.AreafixPassword = r.AreafixPassword
	c.TICPassword = r.TICPassword
	c.Name = r.Name
	c.Flavour = r.Flavour
	c.Hostname = r.Hostname
	c.Port = r.Port
	fam, ok := NormalizeIPFamily(r.IPFamily)
	if !ok {
		slog.Warn("ftn link has an unknown ip_family; calling it over whatever its hostname resolves to",
			"link", r.Address, "ip_family", r.IPFamily, "valid", "ipv4, ipv6 or empty")
	}
	c.IPFamily = fam
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

	for _, a := range config.AssignSharedOutbounds() {
		slog.Warn("ftn network was sharing a binkd outbound with another network; giving it its own "+
			"(set binkd_outbound_path on the network to choose a different one). "+
			"Mail it had already queued stays in the shared outbound — check there if anything for it goes missing",
			"network", a.Network, "outbound", a.Path, "shared_outbound", a.SharedPath, "kept_by", a.KeptBy)
	}

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

// OutboundAssignment records one network AssignSharedOutbounds moved off the
// shared global outbound.
type OutboundAssignment struct {
	Network    string // network key as written in ftn.json
	Path       string // its new binkd_outbound_path, relative as configured
	SharedPath string // the global outbound it was sharing
	KeptBy     string // the network left on the shared outbound
}

// AssignSharedOutbounds gives each network its own BSO outbound when two or
// more would otherwise share the global one. Sharing is never safe: BSO flow
// and bundle names carry only net/node, so a link at 3/123 in one network and
// 3/123 in another resolve to the same file and one network's mail goes to the
// other's hub, and binkd itself tells domains apart by their outbound.
//
// A network shares the global outbound when it sets no binkd_outbound_path or
// sets it to the global path; networks with a path of their own are left
// alone. One sharer keeps the global outbound, so mail already queued there is
// still sent, and each of the rest gets a free NetworkOutboundPath. The keeper
// is, in order of preference: one whose path names the global outbound
// explicitly, then one that is enabled and has an address, then the first by
// name. The keeper's path is then set to the global outbound, so once ftn.json
// is saved the choice sticks and a network added later cannot take it over.
//
// The assignments are made in memory, where every consumer (tosser, binkd.conf
// writer, mailer) reads them; the config editor persists them the next time it
// saves ftn.json.
func (c *FTNConfig) AssignSharedOutbounds() []OutboundAssignment {
	global := c.BinkdOutboundPath
	if global == "" {
		global = DefaultBinkdOutboundPath
	}
	var sharing []string
	for name, netCfg := range c.Networks {
		p := strings.TrimSpace(netCfg.BinkdOutboundPath)
		if p == "" || sameOutbound(p, global) {
			sharing = append(sharing, name)
		}
	}
	if len(sharing) < 2 {
		return nil
	}
	rank := func(name string) int {
		netCfg := c.Networks[name]
		r := 0
		if strings.TrimSpace(netCfg.BinkdOutboundPath) == "" {
			r += 2 // an explicit global path marks the network that had it first
		}
		if !netCfg.InternalTosserEnabled || strings.TrimSpace(netCfg.OwnAddress) == "" {
			r++ // a disabled or placeholder network has no mail queued to keep
		}
		return r
	}
	sort.Slice(sharing, func(i, j int) bool {
		if ri, rj := rank(sharing[i]), rank(sharing[j]); ri != rj {
			return ri < rj
		}
		return sharing[i] < sharing[j]
	})

	keeper := sharing[0]
	keeperCfg := c.Networks[keeper]
	keeperCfg.BinkdOutboundPath = global
	c.Networks[keeper] = keeperCfg

	assigned := make([]OutboundAssignment, 0, len(sharing)-1)
	for _, name := range sharing[1:] {
		netCfg := c.Networks[name]
		netCfg.BinkdOutboundPath = FreeNetworkOutboundPath(global, name, c.outboundsExcept(name))
		c.Networks[name] = netCfg
		assigned = append(assigned, OutboundAssignment{
			Network: name, Path: netCfg.BinkdOutboundPath, SharedPath: global, KeptBy: keeper,
		})
	}
	return assigned
}

// SaveOutboundSplit applies AssignSharedOutbounds to ftn.json on disk and
// writes the result back, so the split made at load survives: without it the
// split lives only in memory, and a network added to ftn.json by hand before
// the next config-editor save could change which network keeps the global
// outbound. It returns the networks it moved; nothing is written when there
// were none.
//
// The file is re-read rather than taken from a loaded FTNConfig, so load-time
// defaults are not written into it, and it is written the way the config
// editor writes it.
func SaveOutboundSplit(configDir string) ([]OutboundAssignment, error) {
	path := filepath.Join(configDir, "ftn.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cfg FTNConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	assigned := cfg.AssignSharedOutbounds()
	if len(assigned) == 0 {
		return nil, nil
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := atomicfile.WriteFile(path, out, 0o644); err != nil {
		return nil, err
	}
	return assigned, nil
}

// outboundsExcept returns the outbound paths set on every network but skip.
func (c *FTNConfig) outboundsExcept(skip string) []string {
	var paths []string
	for name, netCfg := range c.Networks {
		if name != skip && strings.TrimSpace(netCfg.BinkdOutboundPath) != "" {
			paths = append(paths, netCfg.BinkdOutboundPath)
		}
	}
	return paths
}

// sameOutbound reports whether two configured outbound paths name the same
// directory, ignoring separators and trailing slashes.
func sameOutbound(a, b string) bool {
	return filepath.ToSlash(filepath.Clean(a)) == filepath.ToSlash(filepath.Clean(b))
}

// FreeNetworkOutboundPath returns NetworkOutboundPath(global, network), or,
// when that directory is the global outbound or one in inUse, the same path
// with the first free _2, _3, ... suffix. Network names that differ only in
// characters NetworkOutboundPath replaces ("foo.bar", "foo_bar") would
// otherwise be given the same directory.
func FreeNetworkOutboundPath(global, network string, inUse []string) string {
	if global == "" {
		global = DefaultBinkdOutboundPath
	}
	taken := func(p string) bool {
		if sameOutbound(p, global) {
			return true
		}
		for _, u := range inUse {
			if sameOutbound(p, u) {
				return true
			}
		}
		return false
	}
	base := NetworkOutboundPath(global, network)
	p := base
	for i := 2; taken(p); i++ {
		p = fmt.Sprintf("%s_%d", base, i)
	}
	return p
}

// DefaultBinkdOutboundPath is the global BSO outbound used when ftn.json sets
// none.
const DefaultBinkdOutboundPath = "data/ftn/out"

// NetworkOutboundPath derives a network's own BSO outbound from the global
// one: a sibling directory named <global>_<network>, e.g. data/ftn/out_fsxnet.
// The network name is reduced to [a-z0-9_-] so the result never carries the
// dot binkd rejects on a base outbound (see ValidateBinkdOutboundPath).
func NetworkOutboundPath(global, network string) string {
	if global == "" {
		global = DefaultBinkdOutboundPath
	}
	var b strings.Builder
	for _, r := range strings.ToLower(network) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	clean := filepath.Clean(global)
	return filepath.Join(filepath.Dir(clean), filepath.Base(clean)+"_"+b.String())
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
