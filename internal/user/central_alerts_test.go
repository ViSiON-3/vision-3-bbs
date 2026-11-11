package user

import (
	"encoding/json"
	"strings"
	"testing"
)

// Each kind has its own Share and See switch, and setting one touches no
// other; an unknown kind reads off and sets nothing.
func TestCentralAlertPrefsSwitches(t *testing.T) {
	kinds := []string{CentralAlertLogon, CentralAlertLogoff, CentralAlertChatJoin, CentralAlertPost}
	for _, k := range kinds {
		var p CentralAlertPrefs
		p.SetShares(k, true)
		for _, other := range kinds {
			if p.Shares(other) != (other == k) || p.Sees(other) {
				t.Errorf("share %s: %+v", k, p)
			}
		}
		p = CentralAlertPrefs{}
		p.SetSees(k, true)
		for _, other := range kinds {
			if p.Sees(other) != (other == k) || p.Shares(other) {
				t.Errorf("see %s: %+v", k, p)
			}
		}
	}
	var p CentralAlertPrefs
	p.SetShares("message_body", true)
	p.SetSees("message_body", true)
	if p != (CentralAlertPrefs{}) || p.Shares("message_body") || p.Sees("message_body") {
		t.Errorf("unknown kind: %+v", p)
	}
}

// A record from before alerts reads every switch off and is written back
// without them.
func TestCentralAlertPrefsDefaultOff(t *testing.T) {
	var u User
	if err := json.Unmarshal([]byte(`{"id":1,"handle":"Alice","centralPresence":true,"centralChat":true}`), &u); err != nil {
		t.Fatal(err)
	}
	if u.CentralAlerts != (CentralAlertPrefs{}) {
		t.Errorf("old record has alerts: %+v", u.CentralAlerts)
	}
	data, _ := json.Marshal(u)
	if strings.Contains(string(data), "centralAlerts") {
		t.Errorf("unset alerts written: %s", data)
	}
}

// Alert choices are consent: only SetCentralConsent changes them, so a
// stale session copy saved for something else cannot restore an alert the
// caller turned off on another node.
func TestCentralAlertPrefsSurviveStaleSaves(t *testing.T) {
	um, _, u := newDeviceTestMgr(t)
	stale, _ := um.GetUserByID(u.ID)
	stale.CentralAlerts.ShareLogons = true // as if this node had it on

	if _, err := um.SetCentralConsent(u.ID, func(x *User) { x.CentralAlerts.SeePosts = true }); err != nil {
		t.Fatal(err)
	}
	stale.HotKeys = true
	if err := um.UpdateUser(stale); err != nil {
		t.Fatal(err)
	}
	got, _ := um.GetUserByID(u.ID)
	if got.CentralAlerts != (CentralAlertPrefs{SeePosts: true}) || !got.HotKeys {
		t.Errorf("stored %+v, hotkeys %v", got.CentralAlerts, got.HotKeys)
	}
}
