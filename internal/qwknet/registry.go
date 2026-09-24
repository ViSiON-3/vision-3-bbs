package qwknet

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
)

//go:embed registry.json
var registryJSON []byte

// KnownNetwork is a QWK network the wizard can pre-fill: where its hub is,
// how to get an account, and the conferences it carries.
type KnownNetwork struct {
	Key         string               `json:"key"`         // suggested network key (e.g. "dovenet")
	Name        string               `json:"name"`        // display name
	Description string               `json:"description"` //
	HubID       string               `json:"hub_id"`      // hub's QWK ID
	Host        string               `json:"host"`
	Port        int                  `json:"port,omitempty"`
	InfoURL     string               `json:"info_url,omitempty"`
	JoinNotes   string               `json:"join_notes,omitempty"` // how to get a node account
	Conferences []qwk.ConferenceInfo `json:"conferences,omitempty"`
}

// LoadRegistry returns the embedded list of known QWK networks.
func LoadRegistry() ([]KnownNetwork, error) {
	var nets []KnownNetwork
	if err := json.Unmarshal(registryJSON, &nets); err != nil {
		return nil, fmt.Errorf("parsing embedded QWK network registry: %w", err)
	}
	return nets, nil
}
