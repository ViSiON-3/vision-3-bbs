package configeditor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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

// commitField drives the same Set-then-AfterSet sequence the field editor
// uses, so index bookkeeping done in AfterSet is exercised.
func commitField(t *testing.T, m *Model, label, val string) {
	t.Helper()
	for _, f := range m.buildRecordFields() {
		if f.Label != label {
			continue
		}
		if err := f.Set(val); err != nil {
			t.Fatalf("Set(%q, %q): %v", label, val, err)
		}
		if f.AfterSet != nil {
			f.AfterSet(m, val)
		}
		m.recordFields = m.buildRecordFields()
		return
	}
	t.Fatalf("field %q not found", label)
}

// Renaming a network re-sorts the list, so AfterSet has to move recordEditIdx
// to wherever the network landed. It searched the raw input while Set stored
// the key lower-cased, so any capital in the typed name missed and the index
// stayed on the row the network used to occupy — leaving the editor showing a
// different network, or nothing at all.
func TestNetworkRenameWithCapitalsKeepsEditingSameNetwork(t *testing.T) {
	m := newFTNModel(map[string]config.FTNNetworkConfig{
		"fsxnet":      {OwnAddress: "21:4/158.1"},
		"zz_newnet_1": {OwnAddress: "1337:3/123.1"},
	})
	m.recordType = "ftn"
	// The placeholder sorts last of the two.
	m.recordEditIdx = 1

	commitField(t, m, "Network Name", "TQWnet")

	if _, ok := m.configs.FTN.Networks["tqwnet"]; !ok {
		t.Fatalf("rename did not store the lowercased key, have %v", m.ftnNetworkKeys())
	}
	keys := m.ftnNetworkKeys() // [fsxnet tqwnet]
	if m.recordEditIdx < 0 || m.recordEditIdx >= len(keys) {
		t.Fatalf("recordEditIdx %d out of range for %v", m.recordEditIdx, keys)
	}
	if got := keys[m.recordEditIdx]; got != "tqwnet" {
		t.Errorf("still editing %q, want the renamed network tqwnet", got)
	}
	if m.recordCursor != m.recordEditIdx {
		t.Errorf("list cursor %d out of step with edit index %d", m.recordCursor, m.recordEditIdx)
	}
	// The rebuilt form must actually show the renamed network, not be empty.
	fields := m.buildRecordFields()
	if len(fields) == 0 {
		t.Fatal("edit screen has no fields after the rename")
	}
	for _, f := range fields {
		if f.Label == "Network Name" && f.Get() != "tqwnet" {
			t.Errorf("Network Name shows %q, want tqwnet", f.Get())
		}
	}
}

// The all-lowercase case worked before and must keep working.
func TestNetworkRenameLowercaseKeepsEditingSameNetwork(t *testing.T) {
	m := newFTNModel(map[string]config.FTNNetworkConfig{
		"fsxnet":      {OwnAddress: "21:4/158.1"},
		"zz_newnet_1": {OwnAddress: "1337:3/123.1"},
	})
	m.recordType = "ftn"
	m.recordEditIdx = 1

	commitField(t, m, "Network Name", "tqwnet")

	keys := m.ftnNetworkKeys()
	if m.recordEditIdx >= len(keys) || keys[m.recordEditIdx] != "tqwnet" {
		t.Errorf("recordEditIdx %d does not point at tqwnet in %v", m.recordEditIdx, keys)
	}
}

// The placeholder is not reliably the last sorted key: an existing network
// that sorts after "zz_newnet_" (or a tenth placeholder, which sorts before
// the ninth) put the cursor on the wrong row, so the edit screen opened — and
// the rename that followed hit — an existing network.
func TestInsertNetworkOpensTheInsertedNetworkNotTheLastRow(t *testing.T) {
	m := newFTNModel(map[string]config.FTNNetworkConfig{
		"fsxnet": {OwnAddress: "21:4/158.1"},
		"zzznet": {OwnAddress: "99:1/1.1"},
	})
	m.recordType = "ftn"
	m.mode = modeRecordList

	res, _ := m.updateRecordList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	got := res.(Model)

	keys := got.ftnNetworkKeys()
	if got.recordEditIdx < 0 || got.recordEditIdx >= len(keys) {
		t.Fatalf("recordEditIdx %d out of range for %v", got.recordEditIdx, keys)
	}
	if keys[got.recordEditIdx] != "zz_newnet_1" {
		t.Errorf("opened %q, want zz_newnet_1 (keys %v)", keys[got.recordEditIdx], keys)
	}
	if got.recordCursor != got.recordEditIdx {
		t.Errorf("recordCursor %d does not match recordEditIdx %d", got.recordCursor, got.recordEditIdx)
	}
}

// Inserting a network drops the sysop straight into its edit screen: the
// placeholder name it is created under is the binkd domain every address and
// area hangs off, so leaving it on the list makes an unusable record look
// configured.
func TestInsertNetworkOpensEditScreen(t *testing.T) {
	m := newFTNModel(map[string]config.FTNNetworkConfig{
		"fsxnet": {OwnAddress: "21:4/158.1"},
	})
	m.recordType = "ftn"
	m.mode = modeRecordList

	res, _ := m.updateRecordList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	got := res.(Model)

	if got.mode != modeRecordEdit {
		t.Fatalf("mode = %v, want modeRecordEdit", got.mode)
	}
	if len(got.configs.FTN.Networks) != 2 {
		t.Fatalf("want 2 networks after insert, have %v", got.ftnNetworkKeys())
	}
	keys := got.ftnNetworkKeys()
	if got.recordEditIdx < 0 || got.recordEditIdx >= len(keys) {
		t.Fatalf("recordEditIdx %d out of range for %v", got.recordEditIdx, keys)
	}
	if !strings.HasPrefix(keys[got.recordEditIdx], "zz_newnet_") {
		t.Errorf("opened %q, want the newly inserted network", keys[got.recordEditIdx])
	}
	if got.editField != 0 {
		t.Errorf("editField = %d, want the first field (Network Name)", got.editField)
	}
	if len(got.recordFields) == 0 {
		t.Error("edit screen opened with no fields")
	}
}
