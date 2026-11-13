package configeditor

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// The same name rebinds an area bound to nobody (that is how it's put right
// once the account exists) but keeps a working binding, so Enter on the
// field can't hand the area to someone who took a renamed sponsor's old
// handle. A new name is always bound afresh (#1020).
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
	if err := set("late"); err != nil || a.SponsorID != 4 {
		t.Errorf("same name on a binding: id %d, err %v; want 4 kept", a.SponsorID, err)
	}
	if err := set("New"); err != nil || a.Sponsor != "New" || a.SponsorID != 0 {
		t.Errorf("new name: sponsor %q, id %d, err %v", a.Sponsor, a.SponsorID, err)
	}
}
