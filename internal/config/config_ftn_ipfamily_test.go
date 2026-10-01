package config

import (
	"encoding/json"
	"strings"
	"testing"
)

// A bare IPv6 literal on a binkd node line is misread: binkd takes its last
// group for the port. It has to be bracketed.
func TestJoinBinkpHostPort(t *testing.T) {
	for _, tc := range []struct {
		host string
		port int
		want string
	}{
		{"", 24554, ""},
		{"hub.example", 0, "hub.example:24554"},
		{"203.0.113.5", 24556, "203.0.113.5:24556"},
		{"2001:db8::1", 0, "[2001:db8::1]:24554"},
		{"[2001:db8::1]", 24556, "[2001:db8::1]:24556"},
		{" hub.example ", 1, "hub.example:1"},
	} {
		if got := JoinBinkpHostPort(tc.host, tc.port); got != tc.want {
			t.Errorf("JoinBinkpHostPort(%q, %d) = %q, want %q", tc.host, tc.port, got, tc.want)
		}
	}
}

func TestValidateLinkIPFamily(t *testing.T) {
	for _, tc := range []struct {
		host, fam string
		ok        bool
	}{
		{"hub.example", IPFamilyIPv4, true},
		{"hub.example", IPFamilyIPv6, true},
		{"2001:db8::1", IPFamilyIPv4, false},
		{"[2001:db8::1]", IPFamilyIPv4, false},
		{"2001:db8::1", IPFamilyIPv6, true},
		{"203.0.113.5", IPFamilyIPv6, false},
		{"203.0.113.5", IPFamilyIPv4, true},
		{"203.0.113.5", IPFamilyAuto, true},
		{"", IPFamilyIPv4, true},
	} {
		if err := ValidateLinkIPFamily(tc.host, tc.fam); (err == nil) != tc.ok {
			t.Errorf("ValidateLinkIPFamily(%q, %q) = %v, want ok=%v", tc.host, tc.fam, err, tc.ok)
		}
	}
}

func TestFTNLinkIPFamilyJSON(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`{"address":"1:2/3"}`, IPFamilyAuto},
		{`{"address":"1:2/3","ip_family":"ipv4"}`, IPFamilyIPv4},
		{`{"address":"1:2/3","ip_family":"IPv6"}`, IPFamilyIPv6},
		{`{"address":"1:2/3","ip_family":"bogus"}`, IPFamilyAuto},
	} {
		var l FTNLinkConfig
		if err := json.Unmarshal([]byte(tc.in), &l); err != nil {
			t.Fatal(err)
		}
		if l.IPFamily != tc.want {
			t.Errorf("%s: IPFamily = %q, want %q", tc.in, l.IPFamily, tc.want)
		}
	}

	out, err := json.Marshal(FTNLinkConfig{Address: "1:2/3"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "ip_family") {
		t.Errorf("auto is written out: %s — it should be omitted", out)
	}
}
