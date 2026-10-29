package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
	"github.com/ViSiON-3/vision-3-bbs/internal/configmerge"
	"github.com/ViSiON-3/vision-3-bbs/internal/util"
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
	Enabled bool   `json:"enabled"`
	Host    string `json:"host"` // Listen host (blank = all interfaces)
	Port    int    `json:"port"` // Listen port (default: 8765)
	DataDir string `json:"dataDir"`
	// AutoApprove activates new leaf subscriptions without the coordinator
	// approving each node.
	AutoApprove bool `json:"autoApprove"`
	// RequireSignedSubscribe rejects legacy unsigned registrations when enabled.
	// Default false keeps existing networks working during rolling upgrades.
	RequireSignedSubscribe bool `json:"requireSignedSubscribe"`
	// AutoApproveAreas adds proposed areas to the NAL without the coordinator
	// reviewing them. Unset follows AutoApprove, which used to cover both;
	// see AreaProposalsAutoApproved.
	AutoApproveAreas *bool             `json:"autoApproveAreas,omitempty"`
	Networks         []V3NetHubNetwork `json:"networks,omitempty"`
	InitialAreas     []V3NetHubArea    `json:"initialAreas,omitempty"`
}

// AreaProposalsAutoApproved reports whether the hub approves area proposals
// as they arrive. A config written before AutoApproveAreas existed keeps its
// old behaviour, where AutoApprove approved proposals as well as subscribers.
func (c *V3NetHubConfig) AreaProposalsAutoApproved() bool {
	if c.AutoApproveAreas != nil {
		return *c.AutoApproveAreas
	}
	return c.AutoApprove
}

// ListenAddr returns the host:port string for net.Listen / http.Server.
func (c *V3NetHubConfig) ListenAddr() string {
	port := c.Port
	if port == 0 {
		port = 8765
	}
	return util.ListenAddress(c.Host, port)
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

	// CoordinatorPubKey pins the network coordinator's ed25519 public key
	// (standard base64). The leaf rejects any NAL signed by a different key.
	// Blank means not pinned yet: the leaf pins the key of the next NAL it
	// verifies and writes it here (trust on first use). The sysop accepts a
	// coordinator key change by pasting the new key here or blanking it.
	CoordinatorPubKey string `json:"coordinatorPubKey,omitempty"`
	// HubPubKey pins the hub's ed25519 public key (standard base64), as
	// served by the network's /info endpoint. Pinned and accepted the same
	// way as CoordinatorPubKey.
	HubPubKey string `json:"hubPubKey,omitempty"`
	// PendingCoordinatorPubKey is a coordinator key the hub offered that
	// differs from CoordinatorPubKey. The leaf records it here for the sysop
	// to inspect; it is never trusted until the sysop accepts it.
	PendingCoordinatorPubKey string `json:"pendingCoordinatorPubKey,omitempty"`
	// PendingHubPubKey is a hub key /info served that differs from
	// HubPubKey, recorded for the sysop like PendingCoordinatorPubKey.
	PendingHubPubKey string `json:"pendingHubPubKey,omitempty"`
}

// AutoJoinEnabled reports whether message areas created for this leaf
// should be flagged AutoJoin (newscan default). Unset means enabled, so
// existing configs keep today's behavior.
func (l V3NetLeafConfig) AutoJoinEnabled() bool {
	return l.AutoJoinAreas == nil || *l.AutoJoinAreas
}

// ValidV3NetPubKey reports whether s, ignoring surrounding whitespace, is an
// ed25519 public key in standard base64: the form V3NetLeafConfig pins keys
// in, and the form hubs and the registry publish them in.
func ValidV3NetPubKey(s string) bool {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	return err == nil && len(raw) == ed25519.PublicKeySize
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

// ParseV3NetConfig parses v3net.json's contents.
func ParseV3NetConfig(data []byte) (V3NetConfig, error) {
	var cfg V3NetConfig
	err := json.Unmarshal(data, &cfg)
	return cfg, err
}

// V3NetConfigFile describes v3net.json for configmerge.SaveFile.
func V3NetConfigFile(configPath string) configmerge.File[V3NetConfig] {
	return configmerge.File[V3NetConfig]{
		Path:  filepath.Join(configPath, "v3net.json"),
		Parse: ParseV3NetConfig,
		Marshal: func(cfg V3NetConfig) ([]byte, error) {
			return json.MarshalIndent(cfg, "", "  ")
		},
		Reconcile: reconcileV3NetLeafKeys,
	}
}

// reconcileV3NetLeafKeys three-way merges the pinned and pending key fields
// of each leaf, matched by network and hub URL. The leaves array has no ids,
// so the generic merge keeps one side's array whole; without this, a config
// editor that loaded v3net.json before the BBS pinned a key would erase the
// pin when it saved an unrelated edit. A key field the saving side left as
// it was in base takes the value on disk; one it changed keeps its value.
func reconcileV3NetLeafKeys(base, theirs, merged V3NetConfig) V3NetConfig {
	type leafKey struct{ network, hubURL string }
	index := func(leaves []V3NetLeafConfig) map[leafKey]V3NetLeafConfig {
		m := make(map[leafKey]V3NetLeafConfig, len(leaves))
		for _, l := range leaves {
			m[leafKey{l.Network, l.HubURL}] = l
		}
		return m
	}
	baseLeaves, diskLeaves := index(base.Leaves), index(theirs.Leaves)
	field := func(merged, base, disk *string) {
		if *merged == *base {
			*merged = *disk
		}
	}
	out := merged
	out.Leaves = slices.Clone(merged.Leaves)
	for i := range out.Leaves {
		l := &out.Leaves[i]
		k := leafKey{l.Network, l.HubURL}
		b, inBase := baseLeaves[k]
		d, onDisk := diskLeaves[k]
		if !inBase || !onDisk {
			continue // added on one side or removed on disk: nothing to merge
		}
		field(&l.CoordinatorPubKey, &b.CoordinatorPubKey, &d.CoordinatorPubKey)
		field(&l.HubPubKey, &b.HubPubKey, &d.HubPubKey)
		field(&l.PendingCoordinatorPubKey, &b.PendingCoordinatorPubKey, &d.PendingCoordinatorPubKey)
		field(&l.PendingHubPubKey, &b.PendingHubPubKey, &d.PendingHubPubKey)
	}
	return out
}

// SaveV3NetConfig writes cfg to v3net.json under the file's cross-process
// lock, replacing the whole file. To change part of it use
// UpdateV3NetConfig, which keeps changes other programs made.
func SaveV3NetConfig(configPath string, cfg V3NetConfig) error {
	path := filepath.Join(configPath, "v3net.json")
	lock, err := configmerge.Lock(path)
	if err != nil {
		return err
	}
	defer lock.Release()
	return writeV3NetConfig(path, cfg)
}

func writeV3NetConfig(filePath string, cfg V3NetConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal V3Net config: %w", err)
	}
	if err := atomicfile.WriteFile(filePath, data, configmerge.FileMode(filePath, 0644)); err != nil {
		return fmt.Errorf("failed to write V3Net config to %s: %w", filePath, err)
	}
	return nil
}

// UpdateV3NetConfig applies fn to v3net.json as it is on disk now and writes
// the result, holding the file's lock throughout, so a change another
// program made since this process loaded the file is kept. A missing file
// starts from an empty configuration. fn may return ErrNoChange to leave
// the file as it is; UpdateV3NetConfig then returns ErrNoChange.
func UpdateV3NetConfig(configPath string, fn func(*V3NetConfig) error) error {
	path := filepath.Join(configPath, "v3net.json")
	lock, err := configmerge.Lock(path)
	if err != nil {
		return err
	}
	defer lock.Release()

	var cfg V3NetConfig
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if cfg, err = ParseV3NetConfig(data); err != nil {
			return fmt.Errorf("failed to parse V3Net config JSON from %s: %w", path, err)
		}
	case !os.IsNotExist(err):
		return err
	}
	if err := fn(&cfg); err != nil {
		return err // ErrNoChange included: nothing is written
	}
	return writeV3NetConfig(path, cfg)
}
