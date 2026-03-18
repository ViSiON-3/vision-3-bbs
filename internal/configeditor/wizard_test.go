package configeditor

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// newWizardModel returns a minimal Model in modeV3NetSetupFork for testing.
func newWizardModel() Model {
	ti := textinput.New()
	m := Model{
		mode:      modeV3NetSetupFork,
		width:     80,
		height:    25,
		textInput: ti,
		topItems: []topMenuItem{
			{"Q", "Quit"},
		},
	}
	return m
}

func TestWizardFork_LeafSelected(t *testing.T) {
	m := newWizardModel()
	result, _ := m.updateV3NetSetupFork(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	got := result.(Model)
	if got.mode != modeV3NetWizardStep {
		t.Errorf("expected modeV3NetWizardStep, got %v", got.mode)
	}
	if got.wizard.flow != "leaf" {
		t.Errorf("expected flow=leaf, got %q", got.wizard.flow)
	}
	if got.wizard.step != 0 {
		t.Errorf("expected step=0, got %d", got.wizard.step)
	}
}

func TestWizardFork_HubSelected(t *testing.T) {
	m := newWizardModel()
	result, _ := m.updateV3NetSetupFork(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	got := result.(Model)
	if got.mode != modeV3NetWizardStep {
		t.Errorf("expected modeV3NetWizardStep, got %v", got.mode)
	}
	if got.wizard.flow != "hub" {
		t.Errorf("expected flow=hub, got %q", got.wizard.flow)
	}
}

func TestWizardFork_EscBack(t *testing.T) {
	m := newWizardModel()
	result, _ := m.updateV3NetSetupFork(tea.KeyMsg{Type: tea.KeyEscape})
	got := result.(Model)
	if got.mode != modeTopMenu {
		t.Errorf("expected modeTopMenu, got %v", got.mode)
	}
}

func TestLeafWizard_HubURLValidation(t *testing.T) {
	m := newWizardModel()
	m.mode = modeV3NetWizardStep
	m.wizard = wizardState{flow: "leaf", step: 0, hubURL: "notaurl"}
	// Trying to advance with an invalid URL should stay on step 0.
	result, _ := m.updateV3NetWizardStep(tea.KeyMsg{Type: tea.KeyEnter})
	got := result.(Model)
	if got.wizard.step != 0 {
		t.Errorf("expected to stay on step 0 with invalid URL, got step %d", got.wizard.step)
	}
	if got.message == "" {
		t.Error("expected a validation error message")
	}
}

func TestLeafWizard_ValidURLAdvances(t *testing.T) {
	m := newWizardModel()
	m.mode = modeV3NetWizardStep
	m.wizard = wizardState{flow: "leaf", step: 0, hubURL: "https://hub.example.com"}
	result, cmd := m.updateV3NetWizardStep(tea.KeyMsg{Type: tea.KeyEnter})
	got := result.(Model)
	if got.wizard.step != 1 {
		t.Errorf("expected step=1, got %d", got.wizard.step)
	}
	if cmd == nil {
		t.Error("expected a tea.Cmd for auto-fetch")
	}
}

func TestHubWizard_PortValidation(t *testing.T) {
	m := newWizardModel()
	m.mode = modeV3NetWizardStep
	m.wizard = wizardState{flow: "hub", step: 1, port: "99999"}
	result, _ := m.updateV3NetWizardStep(tea.KeyMsg{Type: tea.KeyEnter})
	got := result.(Model)
	if got.wizard.step != 1 {
		t.Errorf("expected to stay on step 1 with invalid port, got step %d", got.wizard.step)
	}
}

func TestHubWizard_AutoApproveToggle(t *testing.T) {
	m := newWizardModel()
	m.mode = modeV3NetWizardStep
	m.wizard = wizardState{flow: "hub", step: 2, autoApprove: false}
	result, _ := m.updateV3NetWizardStep(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	got := result.(Model)
	if !got.wizard.autoApprove {
		t.Error("expected autoApprove to be toggled to true")
	}
}
