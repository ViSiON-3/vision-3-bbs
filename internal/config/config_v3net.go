package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"os"
	"path/filepath"
	"reflect"
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
	// RequireSignedMessages rejects posted messages without an author
	// signature when enabled. Default false accepts legacy unsigned posts
	// during rolling upgrades.
	RequireSignedMessages bool `json:"requireSignedMessages"`
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
	// RequireSignedMessages drops polled messages that carry no author
	// signature. Default false imports legacy unsigned messages while the
	// network's nodes upgrade; a signature that does not verify is refused
	// either way.
	RequireSignedMessages bool `json:"requireSignedMessages,omitempty"`
	// RequireSignedHub refuses any reply to a signed request, or SSE stream,
	// that the hub has not signed, and pauses syncing as a bad signature
	// does. Default false accepts unsigned replies from hubs that predate
	// hub signing, until HubSigned is set.
	RequireSignedHub bool `json:"requireSignedHub,omitempty"`
	// HubSigned records that the hub has signed a reply that verified
	// against HubPubKey. The leaf sets it, and from then on refuses
	// unsigned replies as if RequireSignedHub were on, so stripping the
	// signature header cannot switch the check off, across restarts
	// included. The sysop clears it only if the hub really stopped signing
	// (a rollback to an older version, for one).
	HubSigned bool `json:"hubSigned,omitempty"`

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
		// A leaf is a subscription to one network through one hub, so
		// leaves added or removed elsewhere, and keys the BBS pins, merge
		// leaf by leaf with the saving side's edits.
		ListKeys: v3netListKeys,
		Prepare:  settleV3NetHubMoves,
	}
}

// v3netLeafKeyFields are the JSON fields that identify a leaf in the merge.
var v3netLeafKeyFields = []string{"network", "hubUrl"}

// v3netListKeys names the lists of v3net.json that merge item by item.
var v3netListKeys = map[string][]string{"leaves": v3netLeafKeyFields}

// v3netLeafKey identifies a leaf the way the merge does.
type v3netLeafKey struct{ network, hubURL string }

// settleV3NetHubMoves lets a leaf moved to a new hub on one side replace the
// old leaf even if the other side meanwhile touched only the old leaf's key
// state. Matched on network and hub URL, changing a hub URL reads as removing
// one leaf and adding another, so a key the BBS pinned on the old leaf
// meanwhile would read as "removed here, edited elsewhere": the old leaf
// would be kept and the network listed twice. Pins for the old hub do not
// apply to the new one, so for a network where one side removed a leaf and
// added another, the other side's version of the removed leaf is taken back
// to base when only its key state differs, and the leaf goes. An edit to any
// other field of it is still kept and reported, as is a plain removal.
//
// A network both sides moved to different hubs is a hub URL changed on both
// sides: ours is kept, theirs' move reported, and theirs' other edits merged
// into our leaf (mergeV3NetHubMovedElsewhere).
//
// When a version has two leaves with one network and hub URL, the merge
// takes the leaves as a single value and there are no single leaves to
// settle, so all three are left alone.
func settleV3NetHubMoves(base, ours, theirs V3NetConfig) (V3NetConfig, V3NetConfig, []configmerge.Conflict) {
	for _, leaves := range [][]V3NetLeafConfig{base.Leaves, ours.Leaves, theirs.Leaves} {
		if !configmerge.ItemsKeyed(leaves, v3netLeafKeyFields) {
			return ours, theirs, nil
		}
	}
	theirs.Leaves = settleV3NetLeaves(base.Leaves, ours.Leaves, theirs.Leaves)
	ours.Leaves = settleV3NetLeaves(base.Leaves, theirs.Leaves, ours.Leaves)
	var conflicts []configmerge.Conflict
	ours.Leaves, theirs.Leaves, conflicts = mergeV3NetHubMovedElsewhere(base.Leaves, ours.Leaves, theirs.Leaves)
	return ours, theirs, conflicts
}

// mergeV3NetHubMovedElsewhere settles each network that both sides moved to
// a different new hub, returning ours and theirs without modifying either.
// The two new leaves have different keys, so the merge would keep both and
// list the network twice; like a field changed on both sides, this save's
// hub wins instead, with a conflict. Theirs' leaf leaves theirs, and its
// other edits, measured from the old leaf, merge into ours' leaf as they
// would into a leaf that had not moved (v3netLeafOnHubOf), so a field
// changed on both sides is reported too. Only a network with one leaf in
// each version counts: a second leaf added for a network on either side is
// a subscription through another hub, kept as such.
func mergeV3NetHubMovedElsewhere(base, ours, theirs []V3NetLeafConfig) ([]V3NetLeafConfig, []V3NetLeafConfig, []configmerge.Conflict) {
	// only maps each network with exactly one leaf to that leaf.
	only := func(leaves []V3NetLeafConfig) map[string]V3NetLeafConfig {
		m := make(map[string]V3NetLeafConfig, len(leaves))
		n := make(map[string]int, len(leaves))
		for _, l := range leaves {
			m[l.Network] = l
			n[l.Network]++
		}
		for network, count := range n {
			if count > 1 {
				delete(m, network)
			}
		}
		return m
	}
	baseOnly, oursOnly, theirsOnly := only(base), only(ours), only(theirs)

	var conflicts []configmerge.Conflict
	cloned := false
	for _, network := range slices.Sorted(maps.Keys(theirsOnly)) {
		t := theirsOnly[network]
		b, inBase := baseOnly[network]
		o, inOurs := oursOnly[network]
		if !inBase || !inOurs || t.HubURL == b.HubURL || o.HubURL == b.HubURL || o.HubURL == t.HubURL {
			continue
		}
		if !cloned {
			cloned = true
			ours, theirs = slices.Clone(ours), slices.Clone(theirs)
		}
		theirs = slices.DeleteFunc(theirs, func(l V3NetLeafConfig) bool { return l.Network == network })
		path := "leaves." + network + " " + b.HubURL + ".hubUrl"
		conflicts = append(conflicts, configmerge.Conflict{
			Path: path, Field: "hubUrl",
			What: fmt.Sprintf("%s: changed to %s here and to %s elsewhere; kept this save's hub",
				path, o.HubURL, t.HubURL),
		})
		// Merged as one-leaf files, the leaf is treated exactly as the
		// main merge treats a leaf present on all three sides.
		merged, res, err := configmerge.MergeTypedKeys(v3netListKeys,
			V3NetConfig{Leaves: []V3NetLeafConfig{v3netLeafOnHubOf(b, o)}},
			V3NetConfig{Leaves: []V3NetLeafConfig{o}},
			V3NetConfig{Leaves: []V3NetLeafConfig{v3netLeafOnHubOf(t, o)}})
		if err != nil {
			conflicts = append(conflicts, configmerge.Conflict{
				Path: path, Field: "hubUrl",
				What: fmt.Sprintf("%s: other changes made elsewhere not merged: %v", path, err),
			})
			continue
		}
		conflicts = append(conflicts, res.Conflicts...)
		ours[slices.IndexFunc(ours, func(l V3NetLeafConfig) bool { return l.Network == network })] = merged.Leaves[0]
	}
	return ours, theirs, conflicts
}

// v3netLeafOnHubOf returns l with the hub URL and v3netLeafHubState fields
// of hub. Those belong to the hub a leaf points at, so a version of a leaf
// on another hub has nothing to say about them.
func v3netLeafOnHubOf(l, hub V3NetLeafConfig) V3NetLeafConfig {
	lv, hv := reflect.ValueOf(&l).Elem(), reflect.ValueOf(hub)
	for i := 0; i < lv.NumField(); i++ {
		name, _, _ := strings.Cut(lv.Type().Field(i).Tag.Get("json"), ",")
		if name == "hubUrl" || slices.Contains(v3netLeafHubState, name) {
			lv.Field(i).Set(hv.Field(i))
		}
	}
	return l
}

// settleV3NetLeaves returns other with each leaf that mover moved to a new
// hub, and that other changed only in its key state, put back as it was in
// base. other itself is not modified.
func settleV3NetLeaves(base, mover, other []V3NetLeafConfig) []V3NetLeafConfig {
	index := func(leaves []V3NetLeafConfig) map[v3netLeafKey]V3NetLeafConfig {
		m := make(map[v3netLeafKey]V3NetLeafConfig, len(leaves))
		for _, l := range leaves {
			m[v3netLeafKey{l.Network, l.HubURL}] = l
		}
		return m
	}
	baseLeaves, moverLeaves := index(base), index(mover)
	removed, added := make(map[string]bool), make(map[string]bool)
	for k := range baseLeaves {
		if _, ok := moverLeaves[k]; !ok {
			removed[k.network] = true
		}
	}
	for k := range moverLeaves {
		if _, ok := baseLeaves[k]; !ok {
			added[k.network] = true
		}
	}

	var out []V3NetLeafConfig
	for i, l := range other {
		k := v3netLeafKey{l.Network, l.HubURL}
		b, inBase := baseLeaves[k]
		_, kept := moverLeaves[k]
		if !inBase || kept || !removed[l.Network] || !added[l.Network] ||
			!sameV3NetLeafApartFromKeys(l, b) {
			continue
		}
		if out == nil {
			out = slices.Clone(other)
		}
		out[i] = b
	}
	if out == nil {
		return other
	}
	return out
}

// v3netLeafHubState lists, by JSON name, the leaf fields that record what
// the leaf learned from or about its hub: pinned and pending keys, and any
// flag of that kind added later. None of it applies to another hub, so an
// edit elsewhere to these fields alone does not keep a leaf whose hub URL
// was changed (settleV3NetHubMoves). Per-hub trust state the BBS writes
// into a leaf belongs here.
var v3netLeafHubState = []string{
	"coordinatorPubKey",
	"hubPubKey",
	"pendingCoordinatorPubKey",
	"pendingHubPubKey",
	"hubSigned",
}

// sameV3NetLeafApartFromKeys reports whether a and b differ at most in
// v3netLeafHubState.
func sameV3NetLeafApartFromKeys(a, b V3NetLeafConfig) bool {
	strip := func(l V3NetLeafConfig) (map[string]any, error) {
		data, err := json.Marshal(l)
		if err != nil {
			return nil, err
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		for _, k := range v3netLeafHubState {
			delete(m, k)
		}
		return m, nil
	}
	ma, err1 := strip(a)
	mb, err2 := strip(b)
	return err1 == nil && err2 == nil && reflect.DeepEqual(ma, mb)
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
