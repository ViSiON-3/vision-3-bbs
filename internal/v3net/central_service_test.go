package v3net

import (
	"reflect"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

func TestCentralAddressesFor(t *testing.T) {
	cases := []struct {
		name       string
		host       string
		sshOn      bool
		sshPort    int
		telnetOn   bool
		telnetPort int
		want       []string
	}{
		{"both", "bbs.example", true, 2222, true, 2323, []string{"ssh://bbs.example:2222", "telnet://bbs.example:2323"}},
		{"ssh only", "bbs.example", true, 22, false, 23, []string{"ssh://bbs.example:22"}},
		{"no host", "  ", true, 22, true, 23, nil},
		{"nothing enabled", "bbs.example", false, 22, false, 23, nil},
		{"zero port skipped", "bbs.example", true, 0, true, 23, []string{"telnet://bbs.example:23"}},
		{"wildcard v4 bind", "0.0.0.0", true, 22, true, 23, nil},
		{"wildcard v6 bind", "::", true, 22, true, 23, nil},
		{"loopback", "127.0.0.1", true, 22, false, 0, nil},
		{"loopback v6", "::1", true, 22, false, 0, nil},
		{"public v4", "203.0.113.5", true, 22, false, 0, []string{"ssh://203.0.113.5:22"}},
		{"public v6 bracketed", "2001:db8::5", true, 22, true, 23, []string{"ssh://[2001:db8::5]:22", "telnet://[2001:db8::5]:23"}},
		{"already bracketed v6", "[2001:db8::5]", true, 22, false, 0, []string{"ssh://[2001:db8::5]:22"}},
	}
	for _, c := range cases {
		if got := CentralAddressesFor(c.host, c.sshOn, c.sshPort, c.telnetOn, c.telnetPort); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCentralListingSpecFollowsConfig(t *testing.T) {
	s := &Service{BBSName: "|04My|07 Board", CentralAddresses: []string{"ssh://d.example:22"}, Software: protocol.SoftwareReport{Name: "ViSiON/3", Version: "x", Build: "abc123"}}
	if spec := s.centralListingSpec(config.V3NetLeafConfig{}); spec != nil {
		t.Errorf("no central block: want no listing, got %+v", spec)
	}
	if spec := s.centralListingSpec(config.V3NetLeafConfig{Central: &config.V3NetCentralConfig{Enabled: true}}); spec != nil {
		t.Errorf("enabled without publish: want no listing, got %+v", spec)
	}
	spec := s.centralListingSpec(config.V3NetLeafConfig{Central: &config.V3NetCentralConfig{PublishListing: true, Description: "Hi"}})
	if spec == nil || spec.Name != "/04My/07 Board" || spec.Description != "Hi" || spec.Software != "ViSiON/3 x" { // the build stays off the public listing
		t.Fatalf("spec: %+v", spec)
	}
	if !reflect.DeepEqual(spec.Addresses, []string{"ssh://d.example:22"}) {
		t.Errorf("default addresses not used: %v", spec.Addresses)
	}
	spec = s.centralListingSpec(config.V3NetLeafConfig{Central: &config.V3NetCentralConfig{PublishListing: true, Addresses: []string{"telnet://own.example:23"}}})
	if !reflect.DeepEqual(spec.Addresses, []string{"telnet://own.example:23"}) {
		t.Errorf("configured addresses not used: %v", spec.Addresses)
	}
}

// A subscription with no boards runs when it takes part in Central, which
// it does by offering network chat unless the sysop turned chat off; one
// with no boards, no Central and chat off is skipped as before.
func TestConfigureLeavesRunsCentralOnlySubscription(t *testing.T) {
	dir := t.TempDir()
	svc, err := New(config.V3NetConfig{KeystorePath: dir + "/k.key", DedupDBPath: dir + "/d.sqlite"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	chatOff := false
	svc.ConfigureLeaves([]config.V3NetLeafConfig{
		{HubURL: "http://127.0.0.1:1", Network: "browse", Central: &config.V3NetCentralConfig{Enabled: true, Chat: &chatOff}},
		{HubURL: "http://127.0.0.1:1", Network: "listed", Central: &config.V3NetCentralConfig{PublishListing: true, Chat: &chatOff}},
		{HubURL: "http://127.0.0.1:1", Network: "chat"},
		{HubURL: "http://127.0.0.1:1", Network: "nothing", Central: &config.V3NetCentralConfig{Chat: &chatOff}},
	}, nil)
	got := svc.LeafNetworks()
	if len(got) != 3 {
		t.Fatalf("LeafNetworks() = %v, want browse, listed and chat", got)
	}
	for _, n := range got {
		if n == "nothing" {
			t.Errorf("subscription with no boards, no Central and chat off was started")
		}
	}
}
