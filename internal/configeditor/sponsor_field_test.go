package configeditor

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// Entering the sponsor, even the same name, clears its account binding, so
// the BBS binds it again (#1020): this is how a sponsor bound to nobody is
// put right once the account exists.
func TestSponsorFieldClearsBinding(t *testing.T) {
	m := configuredModel()
	m.recordEditIdx, m.recordType = 0, "msgarea"
	a := &m.configs.MsgAreas[0]
	a.AreaType, a.Network, a.Sponsor, a.SponsorID = "echomail", "fsxnet", "Late", message.NoSponsorID
	var set func(string) error
	for _, f := range m.fieldsMsgArea() {
		if f.Label == "Sponsor" {
			set = f.Set
		}
	}
	if set == nil {
		t.Fatal("no Sponsor field")
	}
	if err := set("Late"); err != nil || a.Sponsor != "Late" || a.SponsorID != 0 {
		t.Errorf("same name: sponsor %q, id %d, err %v", a.Sponsor, a.SponsorID, err)
	}
	a.SponsorID = 4
	if err := set("New"); err != nil || a.Sponsor != "New" || a.SponsorID != 0 {
		t.Errorf("new name: sponsor %q, id %d, err %v", a.Sponsor, a.SponsorID, err)
	}
}
