package configeditor

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// newLeafWizardModel returns a Model in modeWizardForm with a leaf wizard ready.
func newLeafWizardModel() Model {
	ti := textinput.New()
	m := Model{
		width:      80,
		height:     25,
		textInput:  ti,
		wizard:     &wizardState{},
		recordType: "v3netleaf",
		mode:       modeRecordList,
		configs:    &allConfigs{},
		topItems:   []topMenuItem{{"Q", "Quit"}},
	}
	// Simulate pressing Insert on v3netleaf record list.
	result, _ := m.updateRecordList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	return result.(Model)
}

// newHubWizardModel returns a Model in modeWizardForm with a hub wizard ready.
func newHubWizardModel() Model {
	ti := textinput.New()
	m := Model{
		width:      80,
		height:     25,
		textInput:  ti,
		wizard:     &wizardState{},
		recordType: "v3nethub",
		mode:       modeRecordList,
		configs:    &allConfigs{},
		topItems:   []topMenuItem{{"Q", "Quit"}},
	}
	result, _ := m.updateRecordList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	return result.(Model)
}

func TestLeafWizard_InsertOpensForm(t *testing.T) {
	m := newLeafWizardModel()
	if m.mode != modeWizardForm {
		t.Errorf("expected modeWizardForm, got %v", m.mode)
	}
	if m.wizard.flow != "leaf" {
		t.Errorf("expected flow=leaf, got %q", m.wizard.flow)
	}
	if len(m.wizardFields) == 0 {
		t.Error("expected wizard fields to be populated")
	}
}

func TestHubWizard_InsertOpensForm(t *testing.T) {
	m := newHubWizardModel()
	if m.mode != modeWizardForm {
		t.Errorf("expected modeWizardForm, got %v", m.mode)
	}
	if m.wizard.flow != "hub" {
		t.Errorf("expected flow=hub, got %q", m.wizard.flow)
	}
}

func TestLeafWizardForm_EscReturnsToList(t *testing.T) {
	m := newLeafWizardModel()
	result, _ := m.updateWizardForm(tea.KeyMsg{Type: tea.KeyEscape})
	got := result.(Model)
	if got.mode != modeRecordList {
		t.Errorf("expected modeRecordList, got %v", got.mode)
	}
}

func TestLeafWizardForm_HubURLValidation(t *testing.T) {
	m := newLeafWizardModel()
	m.wizard.hubURL = "notaurl"
	m.wizardFields = m.fieldsLeafWizard()
	result, _ := m.updateWizardForm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	got := result.(Model)
	if got.message == "" {
		t.Error("expected a validation error message")
	}
	if got.mode != modeWizardForm {
		t.Errorf("expected to stay in modeWizardForm, got %v", got.mode)
	}
}

func TestLeafWizardForm_ValidSubmit(t *testing.T) {
	m := newLeafWizardModel()
	m.wizard.hubURL = "https://hub.example.com"
	m.wizard.networkName = "testnet"
	m.wizard.boardTag = "testnet"
	m.wizard.pollInterval = "5m"
	m.wizardFields = m.fieldsLeafWizard()
	result, _ := m.updateWizardForm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	got := result.(Model)
	if got.mode != modeRecordList {
		t.Errorf("expected modeRecordList after save, got %v", got.mode)
	}
	if len(got.configs.V3Net.Leaves) != 1 {
		t.Errorf("expected 1 leaf, got %d", len(got.configs.V3Net.Leaves))
	}
}

func TestHubWizardForm_PortValidation(t *testing.T) {
	m := newHubWizardModel()
	m.wizard.port = "99999"
	m.wizard.netName = "testnet"
	m.wizardFields = m.fieldsHubWizard()
	result, _ := m.updateWizardForm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	got := result.(Model)
	if got.message == "" {
		t.Error("expected a validation error message for invalid port")
	}
}

func TestHubWizardForm_AutoApproveToggle(t *testing.T) {
	m := newHubWizardModel()
	// Navigate to auto-approve field (index 3).
	m.editField = 3
	if m.wizardFields[3].Label != "Auto-Approve" {
		t.Fatalf("expected Auto-Approve field at index 3, got %q", m.wizardFields[3].Label)
	}
	result, _ := m.updateWizardForm(tea.KeyMsg{Type: tea.KeySpace})
	got := result.(Model)
	if !got.wizard.autoApprove {
		t.Error("expected autoApprove to be toggled to true")
	}
}
