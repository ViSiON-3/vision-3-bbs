package config

import "testing"

// TestPointWithoutBossLink covers the #276 startup guard: a network posting
// from a point whose boss node is not a link is the misconfiguration that made
// inbound mail pile up unclaimed. warnPointWithoutBossLink only logs, so this
// asserts the detection predicate via the same parse/compare logic.
func TestPointWithoutBossLink(t *testing.T) {
	tests := []struct {
		name        string
		own         string
		links       []string
		wantMissing bool
	}{
		{"point, boss not a link", "1337:3/123.1", []string{"1337:3/100"}, true},
		{"point, boss is a link", "1337:3/123.1", []string{"1337:3/123"}, false},
		{"point, boss among several", "1337:3/123.1", []string{"1337:3/100", "1337:3/123"}, false},
		{"not a point", "21:4/158", []string{"21:1/100"}, false},
		{"point, boss link carries its own point", "1337:3/123.1", []string{"1337:3/123.9"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			net := FTNNetworkConfig{OwnAddress: tc.own}
			for _, l := range tc.links {
				net.Links = append(net.Links, FTNLinkConfig{Address: l})
			}
			if got := pointBossMissing(net); got != tc.wantMissing {
				t.Errorf("pointBossMissing(%s, %v) = %v, want %v", tc.own, tc.links, got, tc.wantMissing)
			}
		})
	}
}
