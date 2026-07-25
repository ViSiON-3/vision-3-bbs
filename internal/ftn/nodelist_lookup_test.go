package ftn

import (
	"strings"
	"testing"
)

func mustLookup(t *testing.T, nl *Nodelist, addr, dnsSuffix string) *NodeLookup {
	t.Helper()
	a, err := ParseAddress(addr)
	if err != nil {
		t.Fatalf("ParseAddress(%q): %v", addr, err)
	}
	res, err := nl.Lookup(a, dnsSuffix)
	if err != nil {
		t.Fatalf("Lookup(%s): %v", addr, err)
	}
	return res
}

func TestLookupNodeUnderHub(t *testing.T) {
	nl := parseTestNodelist(t)
	res := mustLookup(t, nl, "21:2/101", "")

	if res.Self == nil || res.Self.Name != "Some BBS" {
		t.Fatalf("Self = %+v, want Some BBS", res.Self)
	}
	if res.Inferred {
		t.Error("Inferred = true for a listed node")
	}
	if res.Uplink.Address.String() != "21:2/100" {
		t.Errorf("Uplink = %s, want hub 21:2/100", res.Uplink.Address)
	}
	if res.Hostname != "hub2.example.org" || res.Port != 24557 {
		t.Errorf("hub connect = %s:%d, want hub2.example.org:24557", res.Hostname, res.Port)
	}
}

func TestLookupNodeUnderHostOnly(t *testing.T) {
	nl := parseTestNodelist(t)
	res := mustLookup(t, nl, "21:4/158", "")

	if res.Self == nil || res.Self.Name != "My BBS" {
		t.Fatalf("Self = %+v, want My BBS", res.Self)
	}
	if res.Uplink.Address.String() != "21:4/0" {
		t.Errorf("Uplink = %s, want host 21:4/0", res.Uplink.Address)
	}
	if res.Hostname != "eu.example.org" || res.Port != 24556 {
		t.Errorf("hub connect = %s:%d, want eu.example.org:24556", res.Hostname, res.Port)
	}
}

func TestLookupUnlistedNodeInfersHubFromNet(t *testing.T) {
	// Brand-new node not in the list yet: fall back to the net's Host.
	nl := parseTestNodelist(t)
	res := mustLookup(t, nl, "21:2/999", "")

	if res.Self != nil {
		t.Fatalf("Self = %+v, want nil for unlisted node", res.Self)
	}
	if !res.Inferred {
		t.Error("Inferred = false, want true")
	}
	if res.Uplink.Address.String() != "21:2/0" {
		t.Errorf("Uplink = %s, want host 21:2/0", res.Uplink.Address)
	}
	if res.Hostname != "leisure.example.io" || res.Port != 24556 {
		t.Errorf("hub connect = %s:%d", res.Hostname, res.Port)
	}
}

func TestLookupHostWithoutHostnameFallsBackToDNSSuffix(t *testing.T) {
	// Net 5's host has no INA/IBN hostname. With a dns suffix the hostname
	// is derived; port defaults to 24554.
	nl := parseTestNodelist(t)
	res := mustLookup(t, nl, "21:5/7", "fsxnet.nz")

	if res.Uplink.Address.String() != "21:5/0" {
		t.Fatalf("Uplink = %s, want 21:5/0", res.Uplink.Address)
	}
	if res.Hostname != "f0.n5.z21.fsxnet.nz" {
		t.Errorf("Hostname = %q, want derived DNS name", res.Hostname)
	}
	if res.Port != DefaultBinkpPort {
		t.Errorf("Port = %d, want %d", res.Port, DefaultBinkpPort)
	}
}

func TestLookupHostWithoutHostnameWalksUpToZone(t *testing.T) {
	// Same net 5, but no dns suffix: the host is unusable, so the walk
	// continues up to the Zone entry.
	nl := parseTestNodelist(t)
	res := mustLookup(t, nl, "21:5/7", "")

	if res.Uplink.Keyword != "Zone" {
		t.Fatalf("Uplink = %+v, want the Zone entry", res.Uplink)
	}
	if res.Hostname != "agency.bbs.nz" || res.Port != 24556 {
		t.Errorf("hub connect = %s:%d, want agency.bbs.nz:24556", res.Hostname, res.Port)
	}
}

func TestLookupNetNotFoundIsError(t *testing.T) {
	nl := parseTestNodelist(t)
	a, _ := ParseAddress("21:9/1")
	if _, err := nl.Lookup(a, ""); err == nil {
		t.Fatal("want error for net not present in nodelist")
	}
}

func TestLookupWrongZoneIsError(t *testing.T) {
	nl := parseTestNodelist(t)
	a, _ := ParseAddress("1:2/101")
	if _, err := nl.Lookup(a, ""); err == nil {
		t.Fatal("want error for zone not present in nodelist")
	}
}

func TestLookupSelfIsHubUsesHostUplink(t *testing.T) {
	// The hub itself (21:2/100) must not be chosen as its own uplink.
	nl := parseTestNodelist(t)
	res := mustLookup(t, nl, "21:2/100", "")

	if res.Self == nil || res.Self.Name != "Hub Two" {
		t.Fatalf("Self = %+v, want Hub Two", res.Self)
	}
	if res.Uplink.Address.String() != "21:2/0" {
		t.Errorf("Uplink = %s, want host 21:2/0 (not itself)", res.Uplink.Address)
	}
}

func TestLookupDownedHubIsNeverACandidate(t *testing.T) {
	// A former hub marked Down must never be chosen as an uplink, even
	// though it still carries an INA flag: the scan only ever records
	// Hub/Host/Region/Zone lines as candidates, so a Down-keyword line
	// never becomes one in the first place.
	const fixture = "Zone,21,fsxNet,NZ,Coordinator,-Unpublished-,300,CM,INA:agency.bbs.nz,IBN:24556\r\n" +
		"Host,6,Net6_HQ,Oslo_NO,Host_Six,-Unpublished-,300,CM,INA:host6.example.org,IBN:24556\r\n" +
		"Down,100,Former_Hub,Oslo_NO,Gone_Op,-Unpublished-,300,CM,INA:downhub.example.org\r\n" +
		",101,Node_BBS,Oslo_NO,Node_Op,-Unpublished-,300,CM,IBN\r\n"
	nl, err := ParseNodelist(strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("ParseNodelist: %v", err)
	}
	res := mustLookup(t, nl, "21:6/101", "")

	if res.Uplink.Address.String() != "21:6/0" {
		t.Fatalf("Uplink = %s, want host 21:6/0 (not the downed hub 21:6/100)", res.Uplink.Address)
	}
	if res.Hostname != "host6.example.org" || res.Port != 24556 {
		t.Errorf("hub connect = %s:%d, want host6.example.org:24556", res.Hostname, res.Port)
	}
}

func TestLookupNearestPrecedingHubWins(t *testing.T) {
	// Two hubs in one net: each node's uplink is the nearest preceding
	// Hub line, not the net's first (or only) hub.
	const fixture = "Zone,21,fsxNet,NZ,Coordinator,-Unpublished-,300,CM,INA:agency.bbs.nz,IBN:24556\r\n" +
		"Host,7,Net7_HQ,Chicago_IL,Host_Seven,-Unpublished-,300,CM,INA:host7.example.org,IBN:24556\r\n" +
		"Hub,100,First_Hub,Chicago_IL,Hub_One,-Unpublished-,300,CM,INA:hub1.example.org,IBN:24556\r\n" +
		",101,Alpha_BBS,Chicago_IL,Alpha_Op,-Unpublished-,300,CM,IBN\r\n" +
		"Hub,200,Second_Hub,Denver_CO,Hub_Two,-Unpublished-,300,CM,INA:hub2b.example.org,IBN:24557\r\n" +
		",201,Beta_BBS,Denver_CO,Beta_Op,-Unpublished-,300,CM,IBN\r\n"
	nl, err := ParseNodelist(strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("ParseNodelist: %v", err)
	}

	alpha := mustLookup(t, nl, "21:7/101", "")
	if alpha.Uplink.Address.String() != "21:7/100" {
		t.Fatalf("Uplink = %s, want hub 21:7/100", alpha.Uplink.Address)
	}
	if alpha.Hostname != "hub1.example.org" || alpha.Port != 24556 {
		t.Errorf("hub connect = %s:%d, want hub1.example.org:24556", alpha.Hostname, alpha.Port)
	}

	beta := mustLookup(t, nl, "21:7/201", "")
	if beta.Uplink.Address.String() != "21:7/200" {
		t.Fatalf("Uplink = %s, want hub 21:7/200", beta.Uplink.Address)
	}
	if beta.Hostname != "hub2b.example.org" || beta.Port != 24557 {
		t.Errorf("hub connect = %s:%d, want hub2b.example.org:24557", beta.Hostname, beta.Port)
	}
}

func TestBinkpFlagPortForms(t *testing.T) {
	cases := []struct {
		flags    []string
		wantHost string
		wantPort int
	}{
		{[]string{"CM", "INA:a.example", "IBN"}, "a.example", DefaultBinkpPort},
		{[]string{"CM", "INA:a.example", "IBN:24556"}, "a.example", 24556},
		{[]string{"CM", "IBN:b.example"}, "b.example", DefaultBinkpPort},
		{[]string{"CM", "IBN:b.example:24557"}, "b.example", 24557},
		{[]string{"IBN:24555", "INA:a.example"}, "a.example", 24555}, // INA wins for host regardless of order
		{[]string{"CM"}, "", DefaultBinkpPort},
	}
	for _, c := range cases {
		e := NodelistEntry{Flags: c.flags}
		host, port := e.binkpHostPort()
		if host != c.wantHost || port != c.wantPort {
			t.Errorf("binkpHostPort(%v) = %q:%d, want %q:%d", c.flags, host, port, c.wantHost, c.wantPort)
		}
	}
}
