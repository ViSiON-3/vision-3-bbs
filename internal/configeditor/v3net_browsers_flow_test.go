package configeditor

import (
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// The browsers below start HTTP fetches through returned tea.Cmds. Tests
// never run those commands; they deliver the result messages directly.

// TestRegistryBrowser_FromLeafList pins the registry browser opened with B on
// the subscription list: Escape while loading backs out, errors offer a retry,
// stale results are ignored, subscribed networks are refused, and picking a
// new one opens a pre-filled leaf wizard.
func TestRegistryBrowser_FromLeafList(t *testing.T) {
	m, _ := newDiskModel(t)
	m.configs.V3Net.Leaves = []config.V3NetLeafConfig{{HubURL: "https://fel.example", Network: "felnet"}}
	m.configs.Server.BoardName = "Test BBS"
	m = openRecordList(t, m, "v3netleaf")
	m.wizard = nil // no wizard open: selection must create one

	m = press(t, m, "b")
	if m.mode != modeRegistryBrowser || !m.regBrowserLoading {
		t.Fatalf("mode=%v loading=%v", m.mode, m.regBrowserLoading)
	}
	wantScreen(t, m, "Network Registry", "Fetching registry...")
	m = press(t, m, "enter", "esc")
	if m.mode != modeRecordList || m.regBrowserLoading {
		t.Fatalf("esc while loading: mode=%v", m.mode)
	}

	m = press(t, m, "b")
	id := m.regBrowserRequestID
	m = asModel(t, first(m.Update(fetchRegistryMsg{requestID: id, err: &url.Error{Op: "Get", URL: "x", Err: errors.New("no route")}})))
	if m.regBrowserLoading || m.regBrowserError != "Could not fetch registry: no route" {
		t.Fatalf("err=%q", m.regBrowserError)
	}
	wantScreen(t, m, "no route")
	m = press(t, m, "enter") // nothing to select
	m = press(t, m, "r")
	if !m.regBrowserLoading || m.regBrowserRequestID != id+1 {
		t.Fatalf("retry: loading=%v id=%d", m.regBrowserLoading, m.regBrowserRequestID)
	}
	entries := []protocol.RegistryEntry{
		{Name: "felnet", Description: "Fel", HubURL: "https://fel.example"},
		{Name: "newnet", Description: "Brand new", HubURL: "https://new.example"},
	}
	m = asModel(t, first(m.Update(fetchRegistryMsg{requestID: id, entries: entries})))
	if !m.regBrowserLoading {
		t.Fatal("stale registry response applied")
	}
	m = asModel(t, first(m.Update(fetchRegistryMsg{requestID: m.regBrowserRequestID, entries: entries})))
	if m.regBrowserLoading || len(m.regBrowserEntries) != 2 {
		t.Fatalf("loading=%v entries=%d", m.regBrowserLoading, len(m.regBrowserEntries))
	}
	wantScreen(t, m, "felnet", "Brand new")
	m = press(t, m, "r", "enter")
	if m.message != "Already subscribed to felnet" || m.mode != modeRegistryBrowser {
		t.Fatalf("subscribed pick: mode=%v msg=%q", m.mode, m.message)
	}
	m = press(t, m, "end", "enter")
	if m.mode != modeWizardForm || m.wizard == nil || m.wizard.flow != "leaf" ||
		m.wizard.hubURL != "https://new.example" || m.wizard.networkName != "newnet" || m.wizard.origin != "Test BBS" {
		t.Fatalf("mode=%v wizard=%+v", m.mode, m.wizard)
	}
}

// TestRegistryBrowser_FromLeafWizard pins that picking from the browser
// opened on the wizard's Registry row fills that wizard and returns to it.
func TestRegistryBrowser_FromLeafWizard(t *testing.T) {
	m, _ := newDiskModel(t)
	m = press(t, m, "6", "2", "i", "enter")
	if m.mode != modeRegistryBrowser || m.regBrowserReturn != modeWizardForm {
		t.Fatalf("mode=%v return=%v", m.mode, m.regBrowserReturn)
	}
	m = asModel(t, first(m.Update(fetchRegistryMsg{requestID: m.regBrowserRequestID,
		entries: []protocol.RegistryEntry{{Name: "felnet", HubURL: "https://fel.example"}}})))
	m = press(t, m, "enter")
	if m.mode != modeWizardForm || m.wizard.networkName != "felnet" || m.wizard.hubURL != "https://fel.example" {
		t.Fatalf("mode=%v wizard=%+v", m.mode, m.wizard)
	}
	m = press(t, m, "enter", "esc")
	if m.mode != modeWizardForm {
		t.Errorf("esc: mode = %v", m.mode)
	}
}

// TestAreaBrowser_LeafWizardSubscribe pins the area browser in the leaf
// wizard: fetch errors offer a retry, Space subscribes (creating the node key
// and returning the subscribe call) or unsubscribes, hub statuses are shown,
// and Escape hands the choices back to the wizard.
func TestAreaBrowser_LeafWizardSubscribe(t *testing.T) {
	m, _ := newDiskModel(t)
	m = press(t, m, "6", "2", "i")
	m.wizard.hubURL = "https://fel.example"
	m.wizard.networkName = "felnet"
	m = press(t, m, "down", "down", "down", "enter")
	if m.mode != modeV3NetAreaBrowser || !m.areaBrowserLoading {
		t.Fatalf("mode=%v loading=%v", m.mode, m.areaBrowserLoading)
	}
	wantScreen(t, m, "Area Browser — felnet", "Fetching areas...")
	m = press(t, m, "space") // ignored while loading

	m = asModel(t, first(m.Update(fetchNALMsg{err: errors.New("boom")})))
	if m.areaBrowserLoading || !strings.HasPrefix(m.areaBrowserError, "Could not fetch areas") {
		t.Fatalf("err = %q", m.areaBrowserError)
	}
	m = press(t, m, "space", "r")
	if !m.areaBrowserLoading || m.areaBrowserError != "" {
		t.Fatalf("retry: loading=%v", m.areaBrowserLoading)
	}
	m = asModel(t, first(m.Update(fetchNALMsg{areas: []protocol.Area{
		{Tag: "fel.general", Name: "General", Description: "Talk"},
		{Tag: "fel.tech", Name: "Tech"},
	}})))
	if len(m.areaBrowserAreas) != 2 || m.areaBrowserAreas[0].Subscribed {
		t.Fatalf("areas = %+v", m.areaBrowserAreas)
	}
	wantScreen(t, m, "fel.general", "fel.tech")

	res, cmd := m.Update(keyMsg("space"))
	m = asModel(t, res)
	if cmd == nil || !m.areaBrowserAreas[0].Subscribed || m.areaBrowserAreas[0].LocalBoard != "Felnet General" {
		t.Fatalf("subscribe: cmd=%v item=%+v", cmd != nil, m.areaBrowserAreas[0])
	}
	if _, err := os.Stat(m.configs.V3Net.KeystorePath); err != nil {
		t.Errorf("subscribing did not create the node key: %v", err)
	}
	m = asModel(t, first(m.Update(subscribeAreasMsg{statuses: []protocol.AreaSubscriptionStatus{{Tag: "fel.general", Status: "pending"}}})))
	if m.areaBrowserAreas[0].Status != "PENDING" || m.message != "Subscription updated" {
		t.Errorf("status=%q msg=%q", m.areaBrowserAreas[0].Status, m.message)
	}
	m = asModel(t, first(m.Update(subscribeAreasMsg{err: errors.New("denied")})))
	if !strings.HasPrefix(m.message, "Subscribe failed") {
		t.Errorf("subscribe error msg = %q", m.message)
	}

	// Space on the second row subscribes, then again unsubscribes.
	m = press(t, m, "end", "space", "space")
	if m.areaBrowserAreas[1].Subscribed || m.areaBrowserAreas[1].LocalBoard != "" {
		t.Errorf("unsubscribe left %+v", m.areaBrowserAreas[1])
	}
	m = press(t, m, "home", "esc")
	if m.mode != modeWizardForm || len(m.wizard.selectedAreas) != 2 || !m.wizard.selectedAreas[0].Subscribed {
		t.Fatalf("mode=%v selected=%+v", m.mode, m.wizard.selectedAreas)
	}

	// Re-opening keeps the earlier choice.
	m = press(t, m, "enter")
	m = asModel(t, first(m.Update(fetchNALMsg{areas: []protocol.Area{{Tag: "fel.general", Name: "General"}}})))
	if !m.areaBrowserAreas[0].Subscribed || m.areaBrowserAreas[0].Status != "SUB" {
		t.Errorf("reopen: %+v", m.areaBrowserAreas[0])
	}
	m = press(t, m, "esc")
	wantScreen(t, m, "1 area(s) selected")
}

// TestAreaBrowser_LeafRecordSavesBoards pins the browser opened from a
// subscription record's Browse Areas row: existing boards start ticked, and
// Escape writes the new board list and local areas to disk.
func TestAreaBrowser_LeafRecordSavesBoards(t *testing.T) {
	m, dir := newDiskModel(t)
	m.configs.V3Net.Leaves = []config.V3NetLeafConfig{{HubURL: "https://fel.example", Network: "felnet", Boards: []string{"fel.general"}}}
	m = press(t, openRecordList(t, m, "v3netleaf"), "enter")
	m = press(t, gotoField(t, m, "Browse Areas"), "enter")
	if m.mode != modeV3NetAreaBrowser || m.areaBrowserReturn != modeRecordEdit {
		t.Fatalf("mode=%v return=%v", m.mode, m.areaBrowserReturn)
	}
	m = asModel(t, first(m.Update(fetchNALMsg{areas: []protocol.Area{
		{Tag: "fel.general", Name: "General"}, {Tag: "fel.tech", Name: "Tech"},
	}})))
	if !m.areaBrowserAreas[0].Subscribed || m.areaBrowserAreas[1].Subscribed {
		t.Fatalf("preselect = %+v", m.areaBrowserAreas)
	}
	m = press(t, m, "down", "space", "esc")
	if m.mode != modeRecordEdit || m.dirty {
		t.Fatalf("mode=%v dirty=%v msg=%q", m.mode, m.dirty, m.message)
	}
	ac := reloadConfigs(t, dir)
	if b := ac.V3Net.Leaves[0].Boards; strings.Join(b, ",") != "fel.general,fel.tech" {
		t.Errorf("boards = %v", b)
	}
	var tags []string
	for _, a := range ac.MsgAreas {
		tags = append(tags, a.EchoTag)
	}
	if strings.Join(tags, ",") != "fel.general,fel.tech" {
		t.Errorf("areas = %v", tags)
	}
	if len(ac.Conferences) != 1 || ac.Conferences[0].Tag != "FELNET" {
		t.Errorf("conferences = %+v", ac.Conferences)
	}
}

// TestNodeManagement_OpensFromHubList pins that N on a hosted network opens
// node management for it (with the fetch pending) and Escape returns.
func TestNodeManagement_OpensFromHubList(t *testing.T) {
	m, _ := newDiskModel(t)
	m.configs.V3Net.Hub.Networks = []config.V3NetHubNetwork{{Name: "felnet"}}
	m = openRecordList(t, m, "v3nethub")
	res, cmd := m.Update(keyMsg("n"))
	m = asModel(t, res)
	if m.mode != modeV3NetNodes || m.nodesNetwork != "felnet" || !m.nodesLoading || cmd == nil {
		t.Fatalf("mode=%v net=%q loading=%v", m.mode, m.nodesNetwork, m.nodesLoading)
	}
}
