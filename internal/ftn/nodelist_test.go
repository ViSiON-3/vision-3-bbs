package ftn

import (
	"strings"
	"testing"
)

// testNodelist is a small FTS-5000 fixture exercising every keyword the
// parser handles. Shared with the lookup tests in Task 2.
const testNodelist = ";A fsxNet test nodelist\r\n" +
	";S with comment and CRLF line endings\r\n" +
	"Zone,21,fsxNet,New_Zealand,Zone_Coordinator,-Unpublished-,300,CM,INA:agency.bbs.nz,IBN:24556\r\n" +
	"Host,1,Net1_HQ,Dunedin_NZ,Paul_Hayton,-Unpublished-,300,CM,INA:agency.bbs.nz,IBN:24556\r\n" +
	",100,Agency_BBS,Dunedin_NZ,Paul_Hayton,-Unpublished-,300,CM,INA:agency.bbs.nz,IBN\r\n" +
	"Host,2,Net2_HQ,Boston_MA,Host_Two,-Unpublished-,300,CM,INA:leisure.example.io,IBN:24556\r\n" +
	"Hub,100,Hub_Two,Chicago_IL,Hub_Op,-Unpublished-,300,CM,INA:hub2.example.org,IBN:24557\r\n" +
	",101,Some_BBS,Portland_OR,Some_Sysop,-Unpublished-,300,CM,IBN:some.example.org\r\n" +
	"Down,102,Dead_BBS,Nowhere,Dead_Op,-Unpublished-,300,CM\r\n" +
	"Host,4,Net4_HQ,Berlin_DE,Host_Four,-Unpublished-,300,CM,INA:eu.example.org,IBN:24556\r\n" +
	",158,My_BBS,Berlin_DE,My_Sysop,-Unpublished-,300,CM,IBN\r\n" +
	"Host,5,Net5_HQ,Oslo_NO,Host_Five,-Unpublished-,300,CM\r\n" +
	",7,Quiet_BBS,Oslo_NO,Quiet_Sysop,-Unpublished-,300,CM\r\n"

func parseTestNodelist(t *testing.T) *Nodelist {
	t.Helper()
	nl, err := ParseNodelist(strings.NewReader(testNodelist))
	if err != nil {
		t.Fatalf("ParseNodelist: %v", err)
	}
	return nl
}

func TestParseNodelistResolvesAddresses(t *testing.T) {
	nl := parseTestNodelist(t)

	want := []struct {
		keyword string
		addr    string
	}{
		{"Zone", "21:21/0"},
		{"Host", "21:1/0"},
		{"", "21:1/100"},
		{"Host", "21:2/0"},
		{"Hub", "21:2/100"},
		{"", "21:2/101"},
		{"Down", "21:2/102"},
		{"Host", "21:4/0"},
		{"", "21:4/158"},
		{"Host", "21:5/0"},
		{"", "21:5/7"},
	}
	if len(nl.Entries) != len(want) {
		t.Fatalf("entries = %d, want %d", len(nl.Entries), len(want))
	}
	for i, w := range want {
		e := nl.Entries[i]
		if e.Keyword != w.keyword || e.Address.String() != w.addr {
			t.Errorf("entry %d = %s %q, want %s %q", i, e.Address, e.Keyword, w.addr, w.keyword)
		}
	}
}

func TestParseNodelistFieldsAndFlags(t *testing.T) {
	nl := parseTestNodelist(t)

	// 21:2/101 = Some_BBS line.
	e := nl.Entries[5]
	if e.Name != "Some BBS" {
		t.Errorf("Name = %q, want underscores translated to spaces", e.Name)
	}
	if e.Location != "Portland OR" {
		t.Errorf("Location = %q", e.Location)
	}
	if e.Sysop != "Some Sysop" {
		t.Errorf("Sysop = %q", e.Sysop)
	}
	wantFlags := []string{"CM", "IBN:some.example.org"}
	if len(e.Flags) != len(wantFlags) || e.Flags[0] != wantFlags[0] || e.Flags[1] != wantFlags[1] {
		t.Errorf("Flags = %v, want %v", e.Flags, wantFlags)
	}
}

func TestParseNodelistSkipsMalformedAndPreZoneLines(t *testing.T) {
	in := "garbage line with no commas\n" +
		",5,Orphan_Before_Zone,Nowhere,Nobody,-Unpublished-,300\n" +
		"Zone,1,FidoNet,World,Coordinator,-Unpublished-,300,CM\n" +
		"NotAKeyword,9,Bogus,Nowhere,Nobody,-Unpublished-,300\n" +
		",1,Real_Node,Somewhere,Someone,-Unpublished-,300,CM\n"
	nl, err := ParseNodelist(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseNodelist: %v", err)
	}
	// Only the Zone line and the node after it survive: the pre-zone node has
	// no zone context and the unknown keyword is skipped.
	if len(nl.Entries) != 2 {
		t.Fatalf("entries = %d, want 2 (%+v)", len(nl.Entries), nl.Entries)
	}
	if nl.Entries[1].Address.String() != "1:1/1" {
		t.Errorf("node address = %s, want 1:1/1", nl.Entries[1].Address)
	}
}

func TestParseNodelistEmptyIsError(t *testing.T) {
	if _, err := ParseNodelist(strings.NewReader(";only comments\n")); err == nil {
		t.Fatal("want error for nodelist with no entries")
	}
}

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
