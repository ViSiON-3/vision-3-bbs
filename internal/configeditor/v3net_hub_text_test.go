package configeditor

import (
	"errors"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// hostileHubText holds raw CSI and OSC sequences and the 8-bit form of CSI, as
// a hub, a node or the registry could send them. Nothing between it and the
// terminal in the config editor would stop them.
const hostileHubText = "Evil|CL\x1b[2J\x1b]0;owned\x07\u009b2J"

func assertNoControls(t *testing.T, what, s string) {
	t.Helper()
	if strings.ContainsAny(s, "\x1b\x07\u009b") {
		t.Errorf("%s holds control characters: %q", what, s)
	}
}

func TestAreaBrowserCleansHubText(t *testing.T) {
	m := Model{areaBrowserNetwork: "felonynet"}
	res, _ := m.handleFetchNALMsg(fetchNALMsg{areas: []protocol.Area{
		{Tag: "fel.evil", Name: hostileHubText, Description: hostileHubText},
		{Tag: "fel.|CL\x1b[2J", Name: "Bad tag"},
	}})
	got := res.(Model)
	if len(got.areaBrowserAreas) != 1 || got.areaBrowserAreas[0].Tag != "fel.evil" {
		t.Fatalf("areas = %+v, want only the valid tag", got.areaBrowserAreas)
	}
	assertNoControls(t, "name", got.areaBrowserAreas[0].Name)
	assertNoControls(t, "description", got.areaBrowserAreas[0].Description)
}

// The name a subscription saves is expanded later by the BBS's screens.
func TestDefaultLocalBoardNameIsPlainText(t *testing.T) {
	got := defaultLocalBoardName("felonynet", hostileHubText)
	assertNoControls(t, "name", got)
	if strings.Contains(got, "|") {
		t.Errorf("name keeps a pipe: %q", got)
	}
}

func TestHubErrorTextCleansTheHubsWords(t *testing.T) {
	got := hubErrorText("Could not fetch areas", errors.New("hub returned status 500: "+hostileHubText))
	assertNoControls(t, "error", got)
}

func TestRegistryBrowserCleansEntriesWithoutChangingTheFetchedSlice(t *testing.T) {
	entries := []protocol.RegistryEntry{{Name: hostileHubText, Description: hostileHubText, HubURL: hostileHubText}}
	res, _ := Model{}.handleFetchRegistryMsg(fetchRegistryMsg{entries: entries})
	e := res.(Model).regBrowserEntries[0]
	for what, s := range map[string]string{"name": e.Name, "description": e.Description, "hub URL": e.HubURL} {
		assertNoControls(t, what, s)
	}
	if entries[0].Name != hostileHubText {
		t.Error("the slice registry.Fetch caches was changed")
	}
}

func TestSubscribeStatusIsCleaned(t *testing.T) {
	m := Model{areaBrowserAreas: []areaBrowserItem{{Tag: "fel.a"}}}
	res, _ := m.handleSubscribeAreasMsg(subscribeAreasMsg{statuses: []protocol.AreaSubscriptionStatus{{Tag: "fel.a", Status: hostileHubText}}})
	assertNoControls(t, "status", res.(Model).areaBrowserAreas[0].Status)
}
