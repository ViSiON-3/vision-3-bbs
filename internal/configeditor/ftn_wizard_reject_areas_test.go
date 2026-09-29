package configeditor

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// TestEnsureFTNRejectAreas pins which bad/dupe settings the wizard fills in:
// one naming a real area (in any case) is kept with the area's spelling, one
// naming no area is repointed, and an area left by an earlier run is reused
// rather than duplicated.
func TestEnsureFTNRejectAreas(t *testing.T) {
	m := Model{configs: &allConfigs{}}
	m.configs.MsgAreas = []message.MessageArea{
		{ID: 1, Position: 1, Tag: "BADMAIL", AreaType: "local"},
		{ID: 7, Position: 3, Tag: "ftn_dupe", AreaType: "local"},
	}
	m.configs.FTN.BadAreaTag = "badmail"
	m.configs.FTN.DupeAreaTag = "TYPO"
	if !m.ftnRejectAreasMissing() {
		t.Fatal("a dupe tag naming no area should count as missing")
	}

	m.ensureFTNRejectAreas()
	if m.configs.FTN.BadAreaTag != "BADMAIL" || m.configs.FTN.DupeAreaTag != "ftn_dupe" {
		t.Errorf("tags = %q/%q, want BADMAIL/ftn_dupe", m.configs.FTN.BadAreaTag, m.configs.FTN.DupeAreaTag)
	}
	if n := len(m.configs.MsgAreas); n != 2 {
		t.Errorf("%d areas, want 2 (nothing created)", n)
	}
	if m.ftnRejectAreasMissing() {
		t.Error("still missing after ensure")
	}
}

// TestEnsureFTNRejectAreasCreates checks the areas made on a board with
// neither setting: local, sysop-only, ungrouped, after the existing areas.
func TestEnsureFTNRejectAreasCreates(t *testing.T) {
	m := Model{configs: &allConfigs{}}
	m.configs.MsgAreas = []message.MessageArea{{ID: 4, Position: 2, Tag: "GENERAL", AreaType: "local"}}

	m.ensureFTNRejectAreas()
	if m.configs.FTN.BadAreaTag != "ftn_bad" || m.configs.FTN.DupeAreaTag != "ftn_dupe" {
		t.Errorf("tags = %q/%q", m.configs.FTN.BadAreaTag, m.configs.FTN.DupeAreaTag)
	}
	if len(m.configs.MsgAreas) != 3 {
		t.Fatalf("%d areas, want 3", len(m.configs.MsgAreas))
	}
	for i, want := range []struct {
		tag     string
		id, pos int
	}{{"ftn_bad", 5, 3}, {"ftn_dupe", 6, 4}} {
		a := m.configs.MsgAreas[i+1]
		if a.Tag != want.tag || a.ID != want.id || a.Position != want.pos ||
			a.AreaType != "local" || a.Network != "" || a.ConferenceID != 0 ||
			a.ACSRead != "SYSOP" || a.ACSWrite != "SYSOP" || a.AutoJoin {
			t.Errorf("area %d = %+v", i+1, a)
		}
	}
}
