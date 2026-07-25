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
