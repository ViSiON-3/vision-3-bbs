package ftn

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed registry.json
var registryJSON []byte

// RegistryNetwork represents a single FTN network from the embedded registry.
type RegistryNetwork struct {
	Zone             int      `json:"zone"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	InfoURL          string   `json:"info_url,omitempty"`
	PackURL          string   `json:"pack_url,omitempty"`
	Coordinator      string   `json:"coordinator,omitempty"`
	CoordinatorEmail string   `json:"coordinator_email,omitempty"`
	CoordinatorFTN   string   `json:"coordinator_ftn,omitempty"`
	AlsoContact      string   `json:"also_contact,omitempty"`
	HubAddress       string   `json:"hub_address,omitempty"`
	HubHostname      string   `json:"hub_hostname,omitempty"`
	HubPort          int      `json:"hub_port,omitempty"`
	DNSSuffix        string   `json:"dns_suffix,omitempty"`
	EcholistURL      string   `json:"echolist_url,omitempty"`
	NodelistURL      string   `json:"nodelist_url,omitempty"`
	AreatagPrefix    string   `json:"areatag_prefix,omitempty"`
	AreatagExclude   []string `json:"areatag_exclude,omitempty"`
	AreatitlePrefix  string   `json:"areatitle_prefix,omitempty"`
	HandlesAllowed   bool     `json:"handles_allowed,omitempty"`
	AreaManager      string   `json:"area_manager,omitempty"`
}

// normalizeRegistry trims the fields that get handed to a URL parser. Both
// registries are hand-maintained JSON — the embedded one ported from an ini
// file that indents some values, the override one edited by the sysop — so a
// stray space is easy to introduce. Untrimmed it survives
// EcholistIsDownloadable (which trims) and then fails in url.Parse, where a
// leading space makes "https:" a relative path segment; a whitespace-only
// value would likewise read as "configured" rather than "absent".
func normalizeRegistry(networks []RegistryNetwork) []RegistryNetwork {
	for i := range networks {
		networks[i].EcholistURL = strings.TrimSpace(networks[i].EcholistURL)
		networks[i].NodelistURL = strings.TrimSpace(networks[i].NodelistURL)
	}
	return networks
}

// LoadRegistry returns the embedded FTN network registry.
func LoadRegistry() ([]RegistryNetwork, error) {
	var networks []RegistryNetwork
	if err := json.Unmarshal(registryJSON, &networks); err != nil {
		return nil, fmt.Errorf("parsing embedded FTN registry: %w", err)
	}
	return normalizeRegistry(networks), nil
}

// LoadOverrideRegistry loads an optional sysop-provided ftn_networks.json
// from the given config directory. Returns nil (no error) if the file does
// not exist.
func LoadOverrideRegistry(configPath string) ([]RegistryNetwork, error) {
	data, err := os.ReadFile(filepath.Join(configPath, "ftn_networks.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading ftn_networks.json: %w", err)
	}
	var networks []RegistryNetwork
	if err := json.Unmarshal(data, &networks); err != nil {
		return nil, fmt.Errorf("parsing ftn_networks.json: %w", err)
	}
	return normalizeRegistry(networks), nil
}
