package configeditor

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestFTNWizardAreaHonorsAutoJoinChoice verifies that message areas created
// by the FTN wizard use the sysop's "Newscan Default" wizard choice for
// AutoJoin, rather than a hardcoded true.
func TestFTNWizardAreaHonorsAutoJoinChoice(t *testing.T) {
	for _, autoJoin := range []bool{true, false} {
		m := Model{configs: &allConfigs{}, ftnWizard: &ftnWizardState{autoJoinAreas: autoJoin}}
		m.createFTNMsgAreaIfNeeded("FSX_GEN", "General", "echomail", "fsxnet", "FSX_GEN", "21:1/100", 0, "msgbases/fsx_gen")
		if len(m.configs.MsgAreas) != 1 {
			t.Fatalf("expected 1 area, got %d", len(m.configs.MsgAreas))
		}
		if m.configs.MsgAreas[0].AutoJoin != autoJoin {
			t.Errorf("AutoJoin = %v, want %v", m.configs.MsgAreas[0].AutoJoin, autoJoin)
		}
	}
}

// newAutoJoinWizardModel builds a Model mid-wizard, in modeFTNWizardForm,
// with the cursor parked on the "Newscan Default" field.
func newAutoJoinWizardModel(t *testing.T, autoJoin bool) Model {
	t.Helper()
	m := Model{configs: &allConfigs{}, ftnWizard: &ftnWizardState{autoJoinAreas: autoJoin}}
	m.ftnWizardFields = m.fieldsFTNWizard()
	m.mode = modeFTNWizardForm

	idx := -1
	for i, f := range m.ftnWizardFields {
		if f.Label == "Newscan Default" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("Newscan Default field not found")
	}
	m.editField = idx
	return m
}

// TestFTNWizardNewscanDefaultToggleViaSpace verifies that pressing Space on
// the "Newscan Default" field flips autoJoinAreas, matching the toggle
// pattern used by every other ftYesNo field in the config editor.
func TestFTNWizardNewscanDefaultToggleViaSpace(t *testing.T) {
	m := newAutoJoinWizardModel(t, true)
	result, _ := m.updateFTNWizardForm(tea.KeyMsg{Type: tea.KeySpace})
	got := result.(Model)
	if got.ftnWizard.autoJoinAreas {
		t.Error("expected autoJoinAreas to toggle to false")
	}
	if got.mode != modeFTNWizardForm {
		t.Errorf("expected to stay in modeFTNWizardForm, got %v", got.mode)
	}
}

// TestFTNWizardNewscanDefaultToggleViaEnter verifies the same toggle behavior
// on Enter/Tab, since sibling forms (record edit, wizard form) treat both as
// equivalent toggle triggers for ftYesNo fields rather than entering text edit.
func TestFTNWizardNewscanDefaultToggleViaEnter(t *testing.T) {
	m := newAutoJoinWizardModel(t, false)
	result, _ := m.updateFTNWizardForm(tea.KeyMsg{Type: tea.KeyEnter})
	got := result.(Model)
	if !got.ftnWizard.autoJoinAreas {
		t.Error("expected autoJoinAreas to toggle to true")
	}
	if got.mode != modeFTNWizardForm {
		t.Errorf("expected to stay in modeFTNWizardForm (no text-edit mode entered), got %v", got.mode)
	}
}
