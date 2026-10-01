package configeditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// binkdConfPath is where the editor keeps binkd.conf for a model made by
// newDiskModel.
func binkdConfPath(dir string) string {
	return filepath.Join(dir, "..", "data", "ftn", "binkd.conf")
}

// writeBinkdConf writes binkd.conf beside the config dir.
func writeBinkdConf(t *testing.T, dir, content string) {
	t.Helper()
	p := binkdConfPath(dir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readBinkdConf(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(binkdConfPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// #545: a link's address family is picked under Echomail Links, saved to
// ftn.json and written to its binkd.conf node line as -4 / -6.
func TestFTNLinkIPFamily_PinsTheNodeLine(t *testing.T) {
	m, dir := newDiskModel(t)
	m.configs.FTN.Networks = map[string]config.FTNNetworkConfig{
		"tqwnet": {OwnAddress: "1337:3/999", Links: []config.FTNLinkConfig{
			{Address: "1337:3/100", Name: "Hub", Hostname: "hub.example", SessionPassword: "pw"},
		}},
	}
	writeBinkdConf(t, dir, "node 1337:3/100@tqwnet hub.example:24554 pw\n")

	m = press(t, openRecordList(t, m, "ftnlink"), "enter")
	m = pickRecField(t, m, "IP Family", "IPv4")
	saveAndQuit(t, m)

	if got := reloadConfigs(t, dir).FTN.Networks["tqwnet"].Links[0].IPFamily; got != config.IPFamilyIPv4 {
		t.Errorf("saved ip_family = %q, want %q", got, config.IPFamilyIPv4)
	}
	if got := readBinkdConf(t, dir); !strings.Contains(got, "node 1337:3/100@tqwnet -4 hub.example:24554 pw\n") {
		t.Errorf("binkd.conf =\n%s\nwant the node line pinned to IPv4", got)
	}
}

// Setting the family back to Auto takes the flag off again.
func TestFTNLinkIPFamily_AutoClearsTheFlag(t *testing.T) {
	m, dir := newDiskModel(t)
	m.configs.FTN.Networks = map[string]config.FTNNetworkConfig{
		"tqwnet": {OwnAddress: "1337:3/999", Links: []config.FTNLinkConfig{
			{Address: "1337:3/100", Hostname: "hub.example", SessionPassword: "pw", IPFamily: config.IPFamilyIPv4},
		}},
	}
	writeBinkdConf(t, dir, "node 1337:3/100@tqwnet -4 hub.example:24554 pw\n")

	m = press(t, openRecordList(t, m, "ftnlink"), "enter")
	m = pickRecField(t, m, "IP Family", "Auto")
	saveAndQuit(t, m)

	if got := readBinkdConf(t, dir); !strings.Contains(got, "node 1337:3/100@tqwnet hub.example:24554 pw\n") {
		t.Errorf("binkd.conf =\n%s\nwant the -4 removed", got)
	}
}

// A hub given as a literal address can only be reached over its own family.
func TestFTNLinkIPFamily_RefusesTheWrongFamilyForALiteral(t *testing.T) {
	m, _ := newDiskModel(t)
	m.configs.FTN.Networks = map[string]config.FTNNetworkConfig{
		"tqwnet": {OwnAddress: "1337:3/999", Links: []config.FTNLinkConfig{
			{Address: "1337:3/100", Hostname: "203.0.113.5"},
		}},
	}
	m = press(t, openRecordList(t, m, "ftnlink"), "enter")
	m = pickRecField(t, m, "IP Family", "IPv6")
	if !strings.HasPrefix(m.message, "Invalid selection:") {
		t.Errorf("message = %q, want IPv6 refused for an IPv4 address", m.message)
	}
	if got := m.configs.FTN.Networks["tqwnet"].Links[0].IPFamily; got != config.IPFamilyAuto {
		t.Errorf("IPFamily = %q, want it left at auto", got)
	}

	// The other way round: a link pinned to IPv4 refuses an IPv6 literal.
	m = press(t, m, "esc")
	m = pickRecField(t, m, "IP Family", "IPv4")
	m = refuseRecField(t, m, "Hostname", "2001:db8::1")
}

// Before the setting existed the fix was a -4 added to binkd.conf by hand.
// The editor reads it in on load, so the link shows IPv4 and a save keeps it.
func TestFTNLinkIPFamily_AdoptsAHandAddedFlag(t *testing.T) {
	m, dir := newDiskModel(t)
	m.configs.FTN.Networks = map[string]config.FTNNetworkConfig{
		"tqwnet": {OwnAddress: "1337:3/999", Links: []config.FTNLinkConfig{
			{Address: "1337:3/100", Name: "Hub", Hostname: "hub.example", SessionPassword: "pw"},
		}},
	}
	m.dirty = true // seeded directly, so nothing has marked it for saving
	saveAndQuit(t, m)
	// The domain written in other case is the same domain to binkd.
	const handFixed = "node 1337:3/100@TQWNet -4 hub.example:24554 pw\n"
	writeBinkdConf(t, dir, handFixed)

	m2, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := m2.configs.FTN.Networks["tqwnet"].Links[0].IPFamily; got != config.IPFamilyIPv4 {
		t.Fatalf("loaded IPFamily = %q, want the hand-added -4 read in as %q", got, config.IPFamilyIPv4)
	}

	// Any save from the editor, here a rename, leaves the flag in place.
	m2 = press(t, openRecordList(t, m2, "ftnlink"), "enter")
	m2 = setRecField(t, m2, "Name", "TQW Hub")
	saveAndQuit(t, m2)
	if got := readBinkdConf(t, dir); !strings.Contains(got, handFixed) {
		t.Errorf("binkd.conf =\n%s\nwant the hand-added -4 kept", got)
	}
}
