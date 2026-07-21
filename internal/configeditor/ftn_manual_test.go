package configeditor

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// newFTNModel returns a Model editing the first (sorted) FTN network record.
func newFTNModel(networks map[string]config.FTNNetworkConfig) *Model {
	return &Model{
		configs:       &allConfigs{FTN: config.FTNConfig{Networks: networks}},
		recordEditIdx: 0,
	}
}

func setField(t *testing.T, fields []fieldDef, label, val string) {
	t.Helper()
	for _, f := range fields {
		if f.Label == label {
			if err := f.Set(val); err != nil {
				t.Fatalf("Set(%q, %q): %v", label, val, err)
			}
			return
		}
	}
	t.Fatalf("field %q not found", label)
}

func TestNetworkNameLowercasedToMatchBinkdDomain(t *testing.T) {
	// The network key IS the binkd domain; the wizard lowercases it and hubs
	// expect lowercase (fsxnet), so a manually typed FSXNET must normalize.
	m := newFTNModel(map[string]config.FTNNetworkConfig{
		"zz_new_1": {OwnAddress: "21:4/999"},
	})
	setField(t, m.fieldsFTNLink(), "Network Name", "FSXNET")

	if _, ok := m.configs.FTN.Networks["fsxnet"]; !ok {
		t.Fatalf("want lowercased key fsxnet, have keys %v", m.ftnNetworkKeys())
	}
	if _, ok := m.configs.FTN.Networks["FSXNET"]; ok {
		t.Fatal("uppercase key must not be stored")
	}
}

func TestSyncPollEventsCreatesForManualNetworkWithHub(t *testing.T) {
	// A manually created network (never through the wizard) with a hub
	// hostname must get an enabled poll event on save — the TUI-only goal
	// means no trip to the Events editor should be required.
	ev := config.EventsConfig{}
	nets := map[string]config.FTNNetworkConfig{
		"fsxnet": {
			OwnAddress: "21:4/999",
			Links: []config.FTNLinkConfig{{
				Address: "21:4/158", Hostname: "pointhub.example.org", Port: 24556,
			}},
		},
		// No hostname on the link: nothing to poll, no event invented.
		"othernet": {OwnAddress: "1:2/3", Links: []config.FTNLinkConfig{{Address: "1:2/1"}}},
	}
	refreshPollEvents(&ev, nets)

	poll := findEvent(ev, "echomail_poll_fsxnet")
	if poll == nil {
		t.Fatal("poll event must be created for a network with a hub hostname")
	}
	if !poll.Enabled {
		t.Error("created poll event must be enabled")
	}
	if !containsArg(poll.Args, "21:4/158@fsxnet") {
		t.Errorf("poll args wrong: %v", poll.Args)
	}
	if poll.Schedule == "" {
		t.Error("created poll event must have a schedule")
	}
	if findEvent(ev, "echomail_poll_othernet") != nil {
		t.Error("no poll event for a network whose link has no hostname")
	}
}
