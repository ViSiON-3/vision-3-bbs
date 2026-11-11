package config

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// Alerts are off on a config from before them, on a new subscription, and
// on one with every other Central feature on; a Central block saved
// without them writes no alerts key, so an upgrade leaves the file as it
// was.
func TestV3NetCentralAlertsDefaultOff(t *testing.T) {
	cfg, err := ParseV3NetConfig([]byte(`{"leaves":[{"hubUrl":"h","network":"n","central":{"enabled":true,"presence":true,"lastCallers":true,"callersDefault":true,"chat":true}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Leaves[0].CentralAlerts(); len(got) != 0 {
		t.Errorf("old config has alerts on: %v", got)
	}
	fresh := V3NetLeafConfig{Central: NewV3NetLeafCentral()}
	if got := fresh.CentralAlerts(); len(got) != 0 {
		t.Errorf("new subscription has alerts on: %v", got)
	}
	data, err := json.Marshal(cfg.Leaves[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "alerts") {
		t.Errorf("unset alerts written: %s", data)
	}
}

// Each switch is its own: on, it reads under its protocol kind; every kind
// needs Central, and a chat join needs network chat as well. Switches set
// in the file survive a round trip.
func TestV3NetCentralAlertsSwitches(t *testing.T) {
	off := false
	l := V3NetLeafConfig{Central: &V3NetCentralConfig{Enabled: true, Alerts: V3NetCentralAlerts{Logons: true, Logoffs: true, Chat: true, Posts: true}}}
	got := l.CentralAlerts()
	for _, k := range protocol.CentralAlertKinds {
		if !got[k] {
			t.Errorf("%s off with its switch on", k)
		}
	}
	if len(got) != len(protocol.CentralAlertKinds) {
		t.Errorf("unexpected kinds: %v", got)
	}

	l.Central.Chat = &off
	if l.CentralAlerts()[protocol.CentralAlertChatJoin] {
		t.Error("chat joins on with network chat off")
	}
	l.Central.Enabled = false
	if len(l.CentralAlerts()) != 0 {
		t.Error("alerts on with Central off")
	}

	only := V3NetLeafConfig{Central: &V3NetCentralConfig{Enabled: true, Alerts: V3NetCentralAlerts{Logoffs: true}}}
	var kinds []string
	for k := range only.CentralAlerts() {
		kinds = append(kinds, k)
	}
	if !slices.Equal(kinds, []string{protocol.CentralAlertLogoff}) {
		t.Errorf("logoffs alone: %v", kinds)
	}

	data, _ := json.Marshal(V3NetConfig{Leaves: []V3NetLeafConfig{only}})
	back, err := ParseV3NetConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if a := back.Leaves[0].Central.Alerts; a != (V3NetCentralAlerts{Logoffs: true}) {
		t.Errorf("round trip: %+v from %s", a, data)
	}
}
