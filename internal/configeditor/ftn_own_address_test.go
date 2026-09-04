package configeditor

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// newFTNAreasModel returns a Model editing the sole FTN network record, with
// message areas attached so the Own Address sync can be exercised.
func newFTNAreasModel(networks map[string]config.FTNNetworkConfig, areas []message.MessageArea) *Model {
	return &Model{
		configs: &allConfigs{
			FTN:      config.FTNConfig{Networks: networks},
			MsgAreas: areas,
		},
		recordEditIdx: 0,
	}
}

func areaOriginByTag(t *testing.T, m *Model, tag string) string {
	t.Helper()
	for _, a := range m.configs.MsgAreas {
		if a.Tag == tag {
			return a.OriginAddr
		}
	}
	t.Fatalf("area %q not found", tag)
	return ""
}

func TestOwnAddressSyncsAreaOriginAddresses(t *testing.T) {
	// Echomail stamps its origin line, MSGID and header address from the
	// area's OriginAddr. Adding a point to the network address has to reach
	// the areas, or every echo keeps posting the point-less address.
	m := newFTNAreasModel(
		map[string]config.FTNNetworkConfig{"fsxnet": {OwnAddress: "21:4/158"}},
		[]message.MessageArea{
			{Tag: "FSX_GEN", Network: "fsxnet", OriginAddr: "21:4/158"},
			{Tag: "FSX_BBS", Network: "FSXNET", OriginAddr: "21:4/158"}, // case-insensitive match
			{Tag: "FSX_NEW", Network: "fsxnet"},                         // never stamped
			{Tag: "FSX_AKA", Network: "fsxnet", OriginAddr: "21:4/900"}, // deliberate AKA
			{Tag: "OTHER_GEN", Network: "othernet", OriginAddr: "1:2/3"},
			{Tag: "LOCAL", OriginAddr: ""},
		},
	)

	setField(t, m.fieldsFTNLink(), "Own Address", "21:4/158.1")

	if got := m.configs.FTN.Networks["fsxnet"].OwnAddress; got != "21:4/158.1" {
		t.Errorf("network own address = %q, want 21:4/158.1", got)
	}
	for _, tag := range []string{"FSX_GEN", "FSX_BBS", "FSX_NEW"} {
		if got := areaOriginByTag(t, m, tag); got != "21:4/158.1" {
			t.Errorf("area %s origin = %q, want 21:4/158.1", tag, got)
		}
	}
	if got := areaOriginByTag(t, m, "FSX_AKA"); got != "21:4/900" {
		t.Errorf("area pointed at a different AKA must be left alone, got %q", got)
	}
	if got := areaOriginByTag(t, m, "OTHER_GEN"); got != "1:2/3" {
		t.Errorf("another network's area must be left alone, got %q", got)
	}
	if got := areaOriginByTag(t, m, "LOCAL"); got != "" {
		t.Errorf("local area must be left alone, got %q", got)
	}
}

func TestOwnAddressReportsSyncedAreaCount(t *testing.T) {
	m := newFTNAreasModel(
		map[string]config.FTNNetworkConfig{"fsxnet": {OwnAddress: "21:4/158"}},
		[]message.MessageArea{
			{Tag: "FSX_GEN", Network: "fsxnet", OriginAddr: "21:4/158"},
			{Tag: "FSX_BBS", Network: "fsxnet", OriginAddr: "21:4/158"},
		},
	)

	fields := m.fieldsFTNLink()
	var own *fieldDef
	for i := range fields {
		if fields[i].Label == "Own Address" {
			own = &fields[i]
		}
	}
	if own == nil {
		t.Fatal("Own Address field not found")
	}
	// The field editor hands both Set and AfterSet the raw input, so the
	// notice has to report the address as stored, not as typed.
	if err := own.Set("  21:4/158.1  "); err != nil {
		t.Fatalf("Set: %v", err)
	}
	own.AfterSet(m, "  21:4/158.1  ")

	if want := "2 message area(s)"; !strings.Contains(m.message, want) {
		t.Errorf("message = %q, want it to contain %q", m.message, want)
	}
	if want := "set to 21:4/158.1 "; !strings.Contains(m.message, want) {
		t.Errorf("message = %q, want the trimmed address %q", m.message, want)
	}
}

func TestOwnAddressRejectsInvalidAddress(t *testing.T) {
	// An unparseable own_address stops the tosser from starting at all, so it
	// must not be storable, and the areas must not follow a bad value.
	m := newFTNAreasModel(
		map[string]config.FTNNetworkConfig{"fsxnet": {OwnAddress: "21:4/158"}},
		[]message.MessageArea{{Tag: "FSX_GEN", Network: "fsxnet", OriginAddr: "21:4/158"}},
	)

	fields := m.fieldsFTNLink()
	var own *fieldDef
	for i := range fields {
		if fields[i].Label == "Own Address" {
			own = &fields[i]
		}
	}
	if own == nil {
		t.Fatal("Own Address field not found")
	}
	for _, bad := range []string{"", "21:4", "not-an-address", "21:4/158.x"} {
		if err := own.Set(bad); err == nil {
			t.Errorf("Set(%q) must fail", bad)
		}
	}
	if got := m.configs.FTN.Networks["fsxnet"].OwnAddress; got != "21:4/158" {
		t.Errorf("own address = %q, want it unchanged", got)
	}
	if got := areaOriginByTag(t, m, "FSX_GEN"); got != "21:4/158" {
		t.Errorf("area origin = %q, want it unchanged", got)
	}
}
