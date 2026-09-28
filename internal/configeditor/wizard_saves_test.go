package configeditor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
)

// isolatedLeafWizardModel returns a leaf wizard whose config directory and
// keystore live under t.TempDir(). The config directory is a subdirectory so
// that saveAll's BBS root (configPath/..) is also inside the temp tree.
func isolatedLeafWizardModel(t *testing.T) Model {
	t.Helper()
	root := t.TempDir()
	configPath := filepath.Join(root, "configs")
	if err := os.MkdirAll(configPath, 0o755); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(root, "v3net.key")
	// An existing key skips the first-time seed interstitial, so the save
	// lands on the record list like any later leaf.
	if _, _, err := keystore.Load(keyPath); err != nil {
		t.Fatalf("create keystore: %v", err)
	}
	m := Model{
		width:      80,
		height:     25,
		textInput:  textinput.New(),
		wizard:     &wizardState{},
		recordType: "v3netleaf",
		mode:       modeRecordList,
		configPath: configPath,
		configs:    &allConfigs{},
		topItems:   []topMenuItem{{"Q", "Quit"}},
	}
	m.configs.V3Net.KeystorePath = keyPath
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = result.(Model)
	if m.mode != modeWizardForm {
		t.Fatalf("insert did not open the wizard: mode %v", m.mode)
	}
	return m
}

func pressKey(t *testing.T, m Model, k tea.KeyMsg) Model {
	t.Helper()
	result, _ := m.Update(k)
	nm, ok := result.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", result)
	}
	return nm
}

func runeKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

// TestLeafWizardPersistsCreatedAreasAndConference is issue #463 item 1: the
// wizard saved before creating the message areas and conference, which
// cleared dirty, so the second save wrote nothing and the new areas lived
// only in memory.
func TestLeafWizardPersistsCreatedAreasAndConference(t *testing.T) {
	m := isolatedLeafWizardModel(t)
	m.wizard.hubURL = "https://hub.example.com"
	m.wizard.networkName = "testnet"
	m.wizard.pollInterval = "5m"
	m.wizard.selectedAreas = []areaBrowserItem{
		{Tag: "test.general", Name: "General", Subscribed: true, LocalBoard: "Testnet General"},
		{Tag: "test.skipped", Name: "Skipped", Subscribed: false},
	}
	m.wizardFields = m.fieldsLeafWizard()

	m = pressKey(t, m, runeKey('s'))

	if m.mode != modeRecordList {
		t.Fatalf("mode = %v, want modeRecordList; message %q", m.mode, m.message)
	}
	if m.dirty {
		t.Error("model still dirty after a successful save: in-memory changes were not written")
	}

	areas, err := loadJSONSlice[message.MessageArea](m.configPath, "message_areas.json")
	if err != nil {
		t.Fatalf("load message_areas.json: %v", err)
	}
	if len(areas) != 1 || areas[0].EchoTag != "test.general" || areas[0].Network != "testnet" {
		t.Fatalf("message_areas.json = %+v, want the one subscribed testnet area", areas)
	}

	confs, err := loadJSONSlice[conference.Conference](m.configPath, "conferences.json")
	if err != nil {
		t.Fatalf("load conferences.json: %v", err)
	}
	if len(confs) != 1 || confs[0].Tag != "TESTNET" {
		t.Fatalf("conferences.json = %+v, want the TESTNET conference", confs)
	}
	if areas[0].ConferenceID != confs[0].ID {
		t.Errorf("area ConferenceID = %d, want %d", areas[0].ConferenceID, confs[0].ID)
	}

	// Issue #463 item 3: the confirmation used to be wiped by the screen
	// change it came with.
	if m.message != "Leaf saved. Restart BBS to activate." {
		t.Errorf("message = %q, want the leaf-saved confirmation", m.message)
	}
}

// TestCreateBrowserMsgAreaMarksDirty guards the helper itself: any caller
// that saves after creating areas must actually write them.
func TestCreateBrowserMsgAreaMarksDirty(t *testing.T) {
	m := Model{configs: &allConfigs{}}
	m.createBrowserMsgAreaIfNeeded("test.general", "General", "testnet")
	if !m.dirty {
		t.Fatal("creating a message area did not mark the model dirty")
	}

	m.dirty = false
	m.createBrowserMsgAreaIfNeeded("test.general", "General", "testnet")
	if m.dirty {
		t.Error("an existing area should not mark the model dirty")
	}
}

// TestWizardExitConfirmYesShowsValidationError is issue #463 item 3: Y in the
// unsaved-wizard dialog submits the form, and when that fails validation the
// form reopens; the reason must still be on screen.
func TestWizardExitConfirmYesShowsValidationError(t *testing.T) {
	m := isolatedLeafWizardModel(t)
	m.wizard.hubURL = "notaurl"
	m.wizardFields = m.fieldsLeafWizard()

	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.mode != modeWizardExitConfirm {
		t.Fatalf("mode = %v, want modeWizardExitConfirm", m.mode)
	}
	m = pressKey(t, m, runeKey('y'))

	if m.mode != modeWizardForm {
		t.Fatalf("mode = %v, want the form reopened", m.mode)
	}
	if m.message == "" {
		t.Error("form reopened without saying why the save was refused")
	}
}

// ftnWizardWithCachedAreas returns an FTN wizard form whose echolist has
// already been fetched, with the Echo Areas field selected.
func ftnWizardWithCachedAreas(t *testing.T) Model {
	t.Helper()
	m := Model{
		width:      80,
		height:     25,
		textInput:  textinput.New(),
		mode:       modeFTNWizardForm,
		configPath: filepath.Join(t.TempDir(), "configs"),
		configs:    &allConfigs{},
		ftnWizard: &ftnWizardState{
			networkName:  "fsxNet",
			areasFetched: true,
			availableAreas: []ftn.EchoArea{
				{Tag: "FSX_GEN", Description: "General"},
				{Tag: "FSX_BBS", Description: "BBS"},
			},
			selectedAreas: []bool{true, false},
		},
	}
	m.ftnWizardFields = m.fieldsFTNWizard()
	m.editField = -1
	for i, f := range m.ftnWizardFields {
		if f.Label == "Echo Areas" {
			m.editField = i
		}
	}
	if m.editField < 0 {
		t.Fatal("no Echo Areas field in the FTN wizard")
	}
	return m
}

func openCachedFTNBrowser(t *testing.T, m Model) Model {
	t.Helper()
	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeFTNAreaBrowser {
		t.Fatalf("mode = %v, want modeFTNAreaBrowser", m.mode)
	}
	return m
}

// TestFTNAreaBrowserEscDiscardsTogglesOnCachedReopen is issue #463 item 2:
// reopening the browser from cached areas shared the wizard's selection
// slice, so a toggle survived the ESC meant to discard it.
func TestFTNAreaBrowserEscDiscardsTogglesOnCachedReopen(t *testing.T) {
	m := openCachedFTNBrowser(t, ftnWizardWithCachedAreas(t))

	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeySpace}) // untick FSX_GEN
	m = pressKey(t, m, runeKey('A'))                   // select all
	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyEscape})

	if m.mode != modeFTNWizardForm {
		t.Fatalf("mode = %v, want modeFTNWizardForm", m.mode)
	}
	if got := m.ftnWizard.selectedAreas; len(got) != 2 || !got[0] || got[1] {
		t.Errorf("selectedAreas = %v after ESC, want [true false]", got)
	}
}

// TestFTNAreaBrowserEscAfterConfirmedReopen covers the confirm path: after
// Enter writes the selection back, a later reopen-and-ESC must still leave
// the confirmed selection alone.
func TestFTNAreaBrowserEscAfterConfirmedReopen(t *testing.T) {
	m := openCachedFTNBrowser(t, ftnWizardWithCachedAreas(t))
	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeySpace}) // tick FSX_BBS
	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.ftnWizard.selectedAreas; len(got) != 2 || !got[0] || !got[1] {
		t.Fatalf("selectedAreas = %v after Enter, want [true true]", got)
	}

	m = openCachedFTNBrowser(t, m)
	m = pressKey(t, m, runeKey('N')) // deselect all
	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyEscape})

	if got := m.ftnWizard.selectedAreas; len(got) != 2 || !got[0] || !got[1] {
		t.Errorf("selectedAreas = %v after ESC, want [true true]", got)
	}
}
