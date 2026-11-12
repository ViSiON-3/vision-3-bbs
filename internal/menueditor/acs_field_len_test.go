package menueditor

import (
	"strings"
	"testing"
)

// The menu and command ACS fields take an expression longer than they are
// wide (#1038).
func TestACSFieldsTakeLongExpressions(t *testing.T) {
	const long = "(LEVEL>=20&VALIDATED&CALLS>=10)|(FLAG:M&NOTE:moderator)|SYSOP"
	checked := 0
	for _, f := range append(menuFields(), cmdFields()...) {
		if !strings.Contains(f.Label, "ACS") {
			continue
		}
		checked++
		if f.charLimit() < len(long) {
			t.Errorf("%q accepts %d characters, want at least %d", strings.TrimSpace(f.Label), f.charLimit(), len(long))
		}
	}
	if checked != 2 {
		t.Errorf("checked %d ACS fields, want 2", checked)
	}
}

// Opening a stored ACS longer than the field for editing keeps all of it.
func TestEditingLongACSKeepsIt(t *testing.T) {
	const long = "(LEVEL>=20&VALIDATED&CALLS>=10)|(FLAG:M&NOTE:MODERATOR)|SYSOP"
	m := newTestEditor(t)
	m.cmds = []CmdData{{Keys: "X", Command: "GOTO:ADMIN", ACS: long}}
	m.cmdFields = cmdFields()
	m.cmdEditIdx = 0
	m.cmdEditFld = -1
	for i, f := range m.cmdFields {
		if strings.Contains(f.Label, "ACS") {
			m.cmdEditFld = i
		}
	}
	m, _ = m.startCmdFieldEdit()
	if got := m.textInput.Value(); got != long {
		t.Fatalf("editing starts with %q, want %q", got, long)
	}
}
