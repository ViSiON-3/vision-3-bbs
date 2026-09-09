package configeditor

import "testing"

// TestAllowAnonymousField covers the new per-area Allow Anonymous Y/N field:
// an unset value reads as No (the default), and setting Y/N writes an explicit
// pointer the posting path honors.
func TestAllowAnonymousField(t *testing.T) {
	m := configuredModel()
	m.recordType = "msgarea"
	m.recordEditIdx = 0

	field := func() fieldDef {
		for _, f := range m.fieldsMsgArea() {
			if f.Label == "Allow Anonymous" {
				return f
			}
		}
		t.Fatal("Allow Anonymous field not found")
		return fieldDef{}
	}

	// Unset -> "N".
	m.configs.MsgAreas[0].AllowAnon = nil
	if got := field().Get(); got != "N" {
		t.Errorf("unset AllowAnon renders %q, want N (default no)", got)
	}
	// Set Y -> explicit true.
	if err := field().Set("Y"); err != nil {
		t.Fatal(err)
	}
	if a := m.configs.MsgAreas[0].AllowAnon; a == nil || !*a {
		t.Errorf("Set(Y) should write &true, got %v", a)
	}
	if got := field().Get(); got != "Y" {
		t.Errorf("after Set(Y), Get = %q, want Y", got)
	}
	// Set N -> explicit false.
	if err := field().Set("N"); err != nil {
		t.Fatal(err)
	}
	if a := m.configs.MsgAreas[0].AllowAnon; a == nil || *a {
		t.Errorf("Set(N) should write &false, got %v", a)
	}
}
