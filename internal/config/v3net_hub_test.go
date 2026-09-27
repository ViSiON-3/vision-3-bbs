package config

import (
	"encoding/json"
	"testing"
)

func TestAreaProposalsAutoApproved(t *testing.T) {
	cases := []struct {
		name string
		json string
		want bool
	}{
		// Configs from before the split: autoApprove covered proposals too.
		{"legacy on", `{"autoApprove":true}`, true},
		{"legacy off", `{"autoApprove":false}`, false},
		// An explicit autoApproveAreas wins either way.
		{"join auto, areas reviewed", `{"autoApprove":true,"autoApproveAreas":false}`, false},
		{"join reviewed, areas auto", `{"autoApprove":false,"autoApproveAreas":true}`, true},
	}
	for _, c := range cases {
		var h V3NetHubConfig
		if err := json.Unmarshal([]byte(c.json), &h); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := h.AreaProposalsAutoApproved(); got != c.want {
			t.Errorf("%s: AreaProposalsAutoApproved = %v, want %v", c.name, got, c.want)
		}
	}
}
