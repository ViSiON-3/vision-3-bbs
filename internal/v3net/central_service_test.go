package v3net

import (
	"reflect"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
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
	}
	for _, c := range cases {
		if got := CentralAddressesFor(c.host, c.sshOn, c.sshPort, c.telnetOn, c.telnetPort); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCentralListingSpecFollowsConfig(t *testing.T) {
	s := &Service{BBSName: "|04My|07 Board", CentralAddresses: []string{"ssh://d.example:22"}, CentralSoftware: "ViSiON/3 x"}
	if spec := s.centralListingSpec(config.V3NetLeafConfig{}); spec != nil {
		t.Errorf("no central block: want no listing, got %+v", spec)
	}
	if spec := s.centralListingSpec(config.V3NetLeafConfig{Central: &config.V3NetCentralConfig{Enabled: true}}); spec != nil {
		t.Errorf("enabled without publish: want no listing, got %+v", spec)
	}
	spec := s.centralListingSpec(config.V3NetLeafConfig{Central: &config.V3NetCentralConfig{PublishListing: true, Description: "Hi"}})
	if spec == nil || spec.Name != "/04My/07 Board" || spec.Description != "Hi" || spec.Software != "ViSiON/3 x" {
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
