package ftn

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func TestFormatNodeLineAddressFamily(t *testing.T) {
	for _, tc := range []struct{ fam, want string }{
		{config.IPFamilyAuto, "node 21:1/100@fsxnet hub.example:24554 pw"},
		{config.IPFamilyIPv4, "node 21:1/100@fsxnet -4 hub.example:24554 pw"},
		{config.IPFamilyIPv6, "node 21:1/100@fsxnet -6 hub.example:24554 pw"},
	} {
		if got := formatNodeLine("21:1/100@fsxnet", "hub.example:24554", "pw", tc.fam); got != tc.want {
			t.Errorf("formatNodeLine(%q) = %q, want %q", tc.fam, got, tc.want)
		}
	}
}

func TestApplyIPFamily(t *testing.T) {
	for _, tc := range []struct {
		name, line, fam string
		authoritative   bool
		want            string
	}{
		{"pin adds the flag after the address", "node 1:2/3@fido h:1 pw", config.IPFamilyIPv4, false, "node 1:2/3@fido -4 h:1 pw"},
		{"pin replaces the other family", "node 1:2/3@fido h:1 pw -6", config.IPFamilyIPv4, false, "node 1:2/3@fido -4 h:1 pw"},
		{"pin replaces a fallback option", "node 1:2/3@fido -64 h:1 pw", config.IPFamilyIPv4, false, "node 1:2/3@fido -4 h:1 pw"},
		{"pin is idempotent", "node 1:2/3@fido -4 h:1 pw", config.IPFamilyIPv4, true, "node 1:2/3@fido -4 h:1 pw"},
		{"pin finds the address past a leading option", "node -nomd 1:2/3@fido h:1 pw", config.IPFamilyIPv6, false, "node -nomd 1:2/3@fido -6 h:1 pw"},
		{"pin keeps other options", "node 1:2/3@fido -md h:1 pw -ip", config.IPFamilyIPv6, false, "node 1:2/3@fido -6 -md h:1 pw -ip"},
		{"unset auto keeps a hand-added flag", "node 1:2/3@fido -4 h:1 pw", config.IPFamilyAuto, false, "node 1:2/3@fido -4 h:1 pw"},
		{"chosen auto clears the flag", "node 1:2/3@fido -4 h:1 pw", config.IPFamilyAuto, true, "node 1:2/3@fido h:1 pw"},
		{"chosen auto keeps a fallback option", "node 1:2/3@fido -46 h:1 pw", config.IPFamilyAuto, true, "node 1:2/3@fido -46 h:1 pw"},
		{"a -bw argument is not a flag", "node 1:2/3@fido -bw -4 h:1 pw", config.IPFamilyAuto, true, "node 1:2/3@fido -bw -4 h:1 pw"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(applyIPFamily(strings.Fields(tc.line), tc.fam, tc.authoritative), " ")
			if got != tc.want {
				t.Errorf("applyIPFamily(%q, %q, %v) =\n  %q\nwant\n  %q", tc.line, tc.fam, tc.authoritative, got, tc.want)
			}
		})
	}
}

// The sync behind ./config and the mailer supervisor: a pinned family is
// written, a hand-added flag survives a link that never set one, and only the
// editor's authoritative auto takes it off.
func TestSyncBinkdConfAddressFamily(t *testing.T) {
	const handFixed = "node 21:1/100@tqwnet -4 hub.example:24554 pw\n"
	for _, tc := range []struct {
		name string
		link BinkdLinkSync
		want string
	}{
		{"unset leaves the hand-added -4",
			BinkdLinkSync{SessionPwd: "pw", HostPort: "hub.example:24554"},
			handFixed},
		{"IPv6 replaces it",
			BinkdLinkSync{SessionPwd: "pw", HostPort: "hub.example:24554", IPFamily: config.IPFamilyIPv6},
			"node 21:1/100@tqwnet -6 hub.example:24554 pw\n"},
		{"auto chosen in the editor clears it",
			BinkdLinkSync{SessionPwd: "pw", HostPort: "hub.example:24554", IPFamilyAuthoritative: true},
			"node 21:1/100@tqwnet hub.example:24554 pw\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := writeConf(t, handFixed)
			if err := SyncBinkdConf(p, BinkdIdentity{}, map[string]BinkdLinkSync{"21:1/100@tqwnet": tc.link}); err != nil {
				t.Fatal(err)
			}
			if got := readConf(t, p); got != tc.want {
				t.Errorf("binkd.conf =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

// binkd matches the "node" keyword and a node's address case-insensitively,
// so a hand-written line in other case is still that link's line: synced in
// place, never duplicated, and its flag read back.
func TestBinkdNodeLineMatchingIgnoresCase(t *testing.T) {
	const hand = "NODE 21:1/100@TQWNet -4 hub.example:24554 pw\n"
	p := writeConf(t, hand)
	links := map[string]BinkdLinkSync{
		"21:1/100@tqwnet": {SessionPwd: "pw", HostPort: "hub.example:24554", IPFamily: config.IPFamilyIPv4, IPFamilyAuthoritative: true},
	}
	if err := SyncBinkdConf(p, BinkdIdentity{}, links); err != nil {
		t.Fatal(err)
	}
	got := readConf(t, p)
	if n := strings.Count(strings.ToLower(got), "21:1/100@tqwnet"); n != 1 {
		t.Errorf("binkd.conf has %d lines for the node, want 1:\n%s", n, got)
	}
	if !strings.Contains(got, "-4") {
		t.Errorf("binkd.conf lost the -4:\n%s", got)
	}

	fams, err := ReadBinkdIPFamilies(writeConf(t, hand))
	if err != nil {
		t.Fatal(err)
	}
	if fams["21:1/100@tqwnet"] != config.IPFamilyIPv4 {
		t.Errorf("ReadBinkdIPFamilies() = %v, want the lower-cased address mapped to ipv4", fams)
	}
	if !nodeExists(hand, "21:1/100@tqwnet") {
		t.Error("nodeExists missed the mixed-case line")
	}
}

// binkd ends a line at a word starting with '#'. A "-6" in the comment is not
// an option: it must not be read as the node's family, and the comment must
// come through a rewrite unchanged. A '#' inside a word, as in a password,
// starts no comment.
func TestBinkdNodeLineComments(t *testing.T) {
	const line = "node 1337:3/100@tqwnet -4 hub.example:24554 pw # -6 does not work here\n"
	fams, err := ReadBinkdIPFamilies(writeConf(t, line))
	if err != nil {
		t.Fatal(err)
	}
	if fams["1337:3/100@tqwnet"] != config.IPFamilyIPv4 {
		t.Errorf("family read as %q, want ipv4 — the -6 in the comment was taken for an option", fams["1337:3/100@tqwnet"])
	}

	for _, tc := range []struct {
		name string
		link BinkdLinkSync
		want string
	}{
		{"unchanged line is left alone",
			BinkdLinkSync{SessionPwd: "pw", HostPort: "hub.example:24554", IPFamily: config.IPFamilyIPv4, IPFamilyAuthoritative: true},
			line},
		{"a rewrite keeps the comment",
			BinkdLinkSync{SessionPwd: "newpw", HostPort: "hub.example:24554", IPFamily: config.IPFamilyIPv4, IPFamilyAuthoritative: true},
			"node 1337:3/100@tqwnet -4 hub.example:24554 newpw # -6 does not work here\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := writeConf(t, line)
			if err := SyncBinkdConf(p, BinkdIdentity{}, map[string]BinkdLinkSync{"1337:3/100@tqwnet": tc.link}); err != nil {
				t.Fatal(err)
			}
			if got := readConf(t, p); got != tc.want {
				t.Errorf("binkd.conf =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}

	// A short directive is grown ahead of its comment, not after it.
	p := writeConf(t, "node 1337:3/100@tqwnet # hub\n")
	link := BinkdLinkSync{SessionPwd: "pw", HostPort: "hub.example:24554"}
	if err := SyncBinkdConf(p, BinkdIdentity{}, map[string]BinkdLinkSync{"1337:3/100@tqwnet": link}); err != nil {
		t.Fatal(err)
	}
	if got, want := readConf(t, p), "node 1337:3/100@tqwnet hub.example:24554 pw # hub\n"; got != want {
		t.Errorf("binkd.conf = %q, want %q", got, want)
	}

	// '#' inside a password is part of it.
	fields, comment, ok := nodeDirective("node 1:2/3@fido h:1 pa#ss")
	if !ok || comment != "" || fields[len(fields)-1] != "pa#ss" {
		t.Errorf("nodeDirective split a '#' inside a word: fields %q, comment %q", fields, comment)
	}
}

func TestSyncBinkdConfAppendsPinnedNode(t *testing.T) {
	p := writeConf(t, "# conf\n")
	links := map[string]BinkdLinkSync{
		"21:1/100@tqwnet": {SessionPwd: "pw", HostPort: "hub.example:24554", IPFamily: config.IPFamilyIPv4},
	}
	if err := SyncBinkdConf(p, BinkdIdentity{}, links); err != nil {
		t.Fatal(err)
	}
	if got := readConf(t, p); !strings.Contains(got, "node 21:1/100@tqwnet -4 hub.example:24554 pw\n") {
		t.Errorf("binkd.conf =\n%s\nwant the new node line pinned to IPv4", got)
	}
}

// The wizard writes the family it asks about, both for a new hub and when it
// is re-run over one already in binkd.conf.
func TestUpdateBinkdConfAddressFamily(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "binkd.conf")
	cfg := BinkdConfig{
		BBSRoot:   root,
		Domains:   map[string]int{"tqwnet": 1337},
		Addresses: []string{"1337:3/999@tqwnet"},
		Node: BinkdNode{
			Address: "1337:3/100@tqwnet", Hostname: "hub.example:24554", SessionPwd: "pw",
			NetworkName: "tqwnet", IPFamily: config.IPFamilyIPv4,
		},
	}
	if err := UpdateBinkdConf(p, cfg); err != nil {
		t.Fatal(err)
	}
	if got := readConf(t, p); !strings.Contains(got, "\nnode 1337:3/100@tqwnet -4 hub.example:24554 pw\n") {
		t.Fatalf("new conf has no IPv4-pinned node line:\n%s", got)
	}

	cfg.Node.IPFamily = config.IPFamilyAuto
	if err := UpdateBinkdConf(p, cfg); err != nil {
		t.Fatal(err)
	}
	if got := readConf(t, p); !strings.Contains(got, "\nnode 1337:3/100@tqwnet hub.example:24554 pw\n") {
		t.Errorf("re-run with Auto kept the flag:\n%s", got)
	}
}

func TestReadBinkdIPFamilies(t *testing.T) {
	p := writeConf(t, strings.Join([]string{
		"node 21:1/100@fsxnet -4 a.example:24554 pw",
		"node 1:2/3@fidonet b.example:24554 pw -6",
		"node 3:4/5@other c.example:24554 pw",
		"node 3:4/6@other -46 d.example:24554 pw",
		"# node 9:9/9@x -4 commented.example pw",
		"",
	}, "\n"))
	got, err := ReadBinkdIPFamilies(p)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"21:1/100@fsxnet": config.IPFamilyIPv4, "1:2/3@fidonet": config.IPFamilyIPv6}
	if len(got) != len(want) {
		t.Fatalf("ReadBinkdIPFamilies() = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}

	if got, err := ReadBinkdIPFamilies(filepath.Join(t.TempDir(), "missing.conf")); err != nil || len(got) != 0 {
		t.Errorf("missing file = %v, %v; want empty and no error", got, err)
	}
}
