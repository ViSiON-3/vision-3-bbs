package configeditor

import (
	"strings"
	"testing"
)

// TestAreaACSFieldsRejectInvalid checks that the message area, file area and
// conference ACS fields refuse a string the BBS would deny to everyone,
// naming the column, keep the old value, and store a good one trimmed.
func TestAreaACSFieldsRejectInvalid(t *testing.T) {
	m := configuredModel()
	m.recordEditIdx = 0
	for _, tc := range []struct {
		kind   string
		fields func() []fieldDef
		label  string
		get    func() string
	}{
		{"msgarea", m.fieldsMsgArea, "ACS Read", func() string { return m.configs.MsgAreas[0].ACSRead }},
		{"msgarea", m.fieldsMsgArea, "ACS Write", func() string { return m.configs.MsgAreas[0].ACSWrite }},
	} {
		m.recordType = tc.kind
		var fld *fieldDef
		fields := tc.fields()
		for i := range fields {
			if fields[i].Label == tc.label {
				fld = &fields[i]
			}
		}
		if fld == nil {
			t.Fatalf("%s: no %s field", tc.kind, tc.label)
		}
		if err := fld.Set(" s20&fA "); err != nil || tc.get() != "s20&fA" {
			t.Fatalf("%s %s: Set(good) = %v, stored %q", tc.kind, tc.label, err, tc.get())
		}
		for _, bad := range []string{"!Q", "S20-", "s20&"} {
			err := fld.Set(bad)
			if err == nil || !strings.Contains(err.Error(), "column") {
				t.Errorf("%s %s: Set(%q) = %v, want an error naming the column", tc.kind, tc.label, bad, err)
			}
			if tc.get() != "s20&fA" {
				t.Errorf("%s %s: after Set(%q) the value is %q, want the old one kept", tc.kind, tc.label, bad, tc.get())
			}
		}
	}
}
