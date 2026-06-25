package ftn

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	AreatagPrefix    string   `json:"areatag_prefix,omitempty"`
	AreatagExclude   []string `json:"areatag_exclude,omitempty"`
	AreatitlePrefix  string   `json:"areatitle_prefix,omitempty"`
	HandlesAllowed   bool     `json:"handles_allowed,omitempty"`
	AreaManager      string   `json:"area_manager,omitempty"`
}

// LoadRegistry returns the embedded FTN network registry.
func LoadRegistry() ([]RegistryNetwork, error) {
	var networks []RegistryNetwork
	if err := json.Unmarshal(registryJSON, &networks); err != nil {
		return nil, fmt.Errorf("parsing embedded FTN registry: %w", err)
	}
	return networks, nil
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
	return networks, nil
}
