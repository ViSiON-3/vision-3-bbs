package configeditor

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// longACS is a compound expression with readable names, longer than any ACS
// field is wide on screen.
const longACS = "(LEVEL>=20&VALIDATED&CALLS>=10)|(FLAG:M&NOTE:moderator)|SYSOP"

// Every ACS field takes an expression longer than it is wide (#1038). The
// conference field was 10 characters, too short for LEVEL>=20&V.
func TestACSFieldsTakeLongExpressions(t *testing.T) {
	m := newFTNAreasModel(map[string]config.FTNNetworkConfig{"fsxnet": {OwnAddress: "21:4/158"}},
		[]message.MessageArea{{Tag: "GENERAL"}})
	m.configs.FileAreas = []file.FileArea{{Tag: "UTILS"}}
	m.configs.Conferences = []conference.Conference{{ID: 1, Tag: "LOCAL"}}
	checked := 0
	for name, fields := range map[string][]fieldDef{
		"message area": m.fieldsMsgArea(),
		"file area":    m.fieldsFileArea(),
		"conference":   m.fieldsConference(),
		"ftn network":  m.fieldsFTNLink(),
	} {
		for _, f := range fields {
			if !strings.Contains(f.Label, "ACS") {
				continue
			}
			checked++
			if f.charLimit() < len(longACS) {
				t.Errorf("%s %q accepts %d characters, want at least %d", name, strings.TrimSpace(f.Label), f.charLimit(), len(longACS))
			}
			if err := f.Set(longACS); err != nil {
				t.Errorf("%s %q: Set: %v", name, strings.TrimSpace(f.Label), err)
			} else if got := f.Get(); got != longACS {
				t.Errorf("%s %q stored %q", name, strings.TrimSpace(f.Label), got)
			}
		}
	}
	if checked != 8 {
		t.Errorf("checked %d ACS fields, want 8", checked)
	}
}
