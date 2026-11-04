package configeditor

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// Every door type has an ACS field after Min Access Level; it round-trips the
// door's acs, trimmed, and a blank clears it.
func TestDoorACSField(t *testing.T) {
	for _, d := range []config.DoorConfig{
		{Code: "LORD", Name: "LORD"},
		{Code: "TW2", Name: "TW2", IsDOS: true},
		{Code: "VAULT", Name: "Vault", Type: "v3_script"},
		{Code: "SYNC", Name: "Sync", Type: "synchronet_js"},
		{Code: "BBSLINK", Name: "BBSLink", Type: "rlogin"},
	} {
		t.Run(d.Code, func(t *testing.T) {
			m := newDoorModel(map[string]config.DoorConfig{d.Code: d})
			m.recordEditIdx = 0
			fields := m.buildRecordFields()
			minIdx, acsIdx := -1, -1
			for i, f := range fields {
				switch f.Label {
				case "Min Access Level":
					minIdx = i
				case "ACS":
					acsIdx = i
				}
			}
			if acsIdx < 0 || acsIdx != minIdx+1 {
				t.Fatalf("ACS field at %d, Min Access Level at %d; want ACS right after it", acsIdx, minIdx)
			}
			fld := fields[acsIdx]
			if err := fld.Set("  s50&fG  "); err != nil {
				t.Fatalf("Set: %v", err)
			}
			if got := m.configs.Doors[d.Code].ACS; got != "s50&fG" {
				t.Errorf("persisted ACS = %q, want s50&fG", got)
			}
			if got := fld.Get(); got != "s50&fG" {
				t.Errorf("Get() = %q, want s50&fG", got)
			}
			if err := fld.Set(""); err != nil {
				t.Fatalf("Set(blank): %v", err)
			}
			if got := m.configs.Doors[d.Code].ACS; got != "" {
				t.Errorf("ACS after blank = %q, want empty", got)
			}
		})
	}
}

// The ACS field refuses what the BBS would deny to everyone, as Min Access
// Level refuses a non-number: a malformed string, and any term acs.Validate
// reports, negated or not. The door keeps its previous acs; blank is allowed.
func TestDoorACSFieldRejectsInvalid(t *testing.T) {
	d := config.DoorConfig{Code: "LORD", Name: "LORD", ACS: "s20"}
	m := newDoorModel(map[string]config.DoorConfig{d.Code: d})
	m.recordEditIdx = 0
	var fld *fieldDef
	fields := m.buildRecordFields()
	for i := range fields {
		if fields[i].Label == "ACS" {
			fld = &fields[i]
		}
	}
	if fld == nil {
		t.Fatal("no ACS field")
	}
	for _, bad := range []string{"S20&", "(s20", "s20 fA", "!Q", "fG|!Q", "H25", "s20@"} {
		err := fld.Set(bad)
		if err == nil {
			t.Errorf("Set(%q) accepted", bad)
			continue
		}
		if !strings.Contains(err.Error(), "column") {
			t.Errorf("Set(%q) error %q does not say where", bad, err)
		}
		if got := m.configs.Doors[d.Code].ACS; got != "s20" {
			t.Errorf("after Set(%q), ACS = %q, want s20 kept", bad, got)
		}
	}
	for _, good := range []string{"", "   ", "*", "s50&!fX", "SYSOP|(s20&fG)"} {
		if err := fld.Set(good); err != nil {
			t.Errorf("Set(%q): %v", good, err)
		}
	}
}
