package configeditor

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

const (
	minWidth  = 80
	minHeight = 25
)

// editorMode represents the current interaction state.
type editorMode int

const (
	modeTopMenu                editorMode = iota // Top-level menu
	modeSysConfigMenu                            // System Configuration inner menu
	modeSysConfigEdit                            // System config field navigation
	modeSysConfigField                           // System config field editing (textinput active)
	modeRecordList                               // Scrollable record list
	modeRecordEdit                               // Single record field navigation
	modeRecordField                              // Single record field editing
	modeExitConfirm                              // Unsaved changes exit confirm
	modeSaveConfirm                              // Confirm save
	modeHelp                                     // Help screen overlay
	modeDeleteConfirm                            // Confirm delete record
	modeLookupPicker                             // Lookup picker popup
	modeRecordReorder                            // Reorder mode (move record to new position)
	modeCategoryMenu                             // Generic category sub-menu
	modeWizardForm                               // Wizard form field navigation
	modeWizardField                              // Wizard form field editing (textinput active)
	modeV3NetWizardStep                          // Hub areas sub-form
	modeV3NetIdentity                            // V3Net Node Identity screen
	modeV3NetHubAreas                            // Hub area management list
	modeV3NetAreaDeleteConfirm                   // Confirm area removal from config
	modeV3NetAreaDeleteJAM                       // Confirm JAM file deletion
	modeV3NetAreaRename                          // Rename area form
	modeV3NetAreaRenameJAM                       // Confirm JAM base path rename
	modeV3NetAreaInsert                          // Insert new area form
	modeNavSaveConfirm                           // Save-and-continue confirm (does not quit)
	modeWizardExitConfirm                        // Wizard discard/save confirm
	modeV3NetAreaBrowser                         // Area browser (NAL fetch + subscribe)
	modeRegistryBrowser                          // Registry browser (discover networks)
)

// topMenuItem defines an entry in the top-level menu.
type topMenuItem struct {
	Key   string // Display key (1-9, A, Q)
	Label string // Display label
}

// categoryMenuItem defines an entry in a generic category sub-menu.
type categoryMenuItem struct {
	Label      string     // Display label
	RecordType string     // If non-empty, opens record list for this type
	Mode       editorMode // If non-zero, transitions to this mode instead
}

// sysConfigMenuItem defines an entry in the system config inner menu.
type sysConfigMenuItem struct {
	Label string
}

// wizardArea is a single area entry in the hub setup wizard.
type wizardArea struct {
	Tag         string
	Name        string
	Description string
	BasePath    string
}

// areaBrowserItem represents one area in the NAL area browser.
type areaBrowserItem struct {
	Tag         string // NAL area tag (e.g. "fel.general")
	Name        string // Display name (e.g. "General")
	Description string
	Status      string // "", "ACTIVE", "PENDING", "DENIED"
	Subscribed  bool   // toggled by Space
	LocalBoard  string // auto-generated or user-edited local board name
}

// wizardState holds all transient state for the V3Net setup wizard.
type wizardState struct {
	flow string // "leaf" or "hub"
	step int    // current step index (hub areas sub-form only)

	// Leaf wizard fields
	hubURL       string
	networkName  string
	pollInterval string
	origin       string
	fetchError   string // set if auto-fetch failed

	selectedAreas []areaBrowserItem // areas selected during wizard flow

	// Hub wizard fields (steps 0–3)
	netName       string
	netDesc       string
	port          string
	autoApprove   bool
	areas         []wizardArea
	areaEditTag   string
	areaEditName  string
	areaEditDesc  string
	areaAdding    bool // true when the area form is open
	areaCursor    int  // highlighted area in the area list
	areaEditField int  // active field in area form (0=tag, 1=name, 2=desc)
	areaEditIdx   int  // -1=adding new, >=0=editing existing area
}

// Model is the BubbleTea model for the config editor TUI.
type Model struct {
	// Config data
	configs    *allConfigs
	origServer config.ServerConfig // snapshot for dirty tracking
	configPath string
	dirty      bool

	// Top menu state
	topCursor int
	topItems  []topMenuItem

	// Category sub-menu state
	catMenuTitle  string
	catMenuItems  []categoryMenuItem
	catMenuCursor int

	// Back-navigation: where Escape in record list / wizard returns to.
	// Zero means modeTopMenu (default for items opened directly from top menu).
	returnMode editorMode

	// System config inner menu
	sysMenuCursor int
	sysMenuItems  []sysConfigMenuItem
	sysSubScreen  int        // which sub-screen (0-5)
	sysFields     []fieldDef // current sub-screen fields

	// Record list state
	recordType    string // "msgarea", "filearea", "conference", etc.
	recordCursor  int
	recordScroll  int
	recordFields  []fieldDef // fields for current record
	recordEditIdx int        // index of record being edited
	editField     int        // current field index
	fieldScroll   int        // first visible field row in edit screens
	stayOnField   bool       // if true, don't advance to next field after apply (e.g. FTN network rename)

	// Reorder state
	reorderSourceIdx int // index of record being moved (-1 when inactive)
	reorderMinIdx    int // lower bound for cursor in reorder mode (conference clamp)
	reorderMaxIdx    int // upper bound (inclusive) for cursor in reorder mode

	// Text input (shared for editing fields)
	textInput textinput.Model

	// Lookup picker state
	pickerItems      []LookupItem // items for current picker
	pickerCursor     int          // highlighted item
	pickerScroll     int          // scroll offset
	pickerReturnMode editorMode   // mode to return to on cancel/select

	// Confirm dialog
	confirmYes        bool
	navSaveDestMode   editorMode // where to go on Yes or No
	navSaveSourceMode editorMode // where to return on ESC (cancel)

	// V3Net setup wizard state
	wizard       *wizardState // pointer so field closures survive value-receiver copies
	wizardFields []fieldDef   // fields for wizard form
	wizardTitle  string       // title for wizard form box

	// V3Net Node Identity screen state
	identitySubState      int // 0=main, 1=showPhrase, 2=exportPrompt, 3=recoverInput, 4=recoverConfirm
	identityPhrase        string
	identityRecoverInput  string
	identityRecoverNodeID string

	// V3Net hub area management state
	hubAreaNetwork   string // network name for filtering areas
	hubAreaCursor    int
	hubAreaScroll    int
	hubAreaTargetIdx int // MsgAreas index of area being edited/deleted

	// Edit form state (0=tag, 1=name, 2=desc, 3=basepath)
	hubAreaEditStep int
	hubAreaEditTag  string
	hubAreaEditName string
	hubAreaEditDesc string
	hubAreaEditBase string
	hubAreaOldTag   string // original tag before editing (for self-leaf sync)
	hubAreaOldBase  string // original base path (for JAM rename detection)

	// Insert form state (0=tag, 1=name, 2=desc, 3=basepath)
	hubAreaInsertStep int
	hubAreaInsertTag  string
	hubAreaInsertName string
	hubAreaInsertDesc string
	hubAreaInsertBase string

	// V3Net area browser state
	areaBrowserHub     string            // hub URL being browsed
	areaBrowserNetwork string            // network name
	areaBrowserAreas   []areaBrowserItem // fetched areas with status
	areaBrowserCursor  int               // highlighted row
	areaBrowserScroll  int               // scroll offset
	areaBrowserLoading bool              // true while NAL fetch in flight
	areaBrowserError   string            // error from fetch/subscribe
	areaBrowserReturn  editorMode // mode to return to on ESC

	// V3Net registry browser state
	regBrowserEntries []protocol.RegistryEntry // fetched networks
	regBrowserCursor  int                      // highlighted row
	regBrowserScroll  int                      // scroll offset
	regBrowserLoading   bool                     // true while fetch in flight
	regBrowserCancel    context.CancelFunc        // cancels the in-flight fetch
	regBrowserRequestID uint64                    // monotonic; stale responses are ignored
	regBrowserError     string                   // error from fetch
	regBrowserReturn    editorMode               // mode to return to on ESC

	// Seed phrase interstitial (shown after first-time wizard save)
	showSeedInterstitial   bool
	seedInterstitialPhrase string
	seedInterstitialNodeID string
	keyExistedBeforeSave   bool

	// Terminal
	width   int
	height  int
	mode    editorMode
	message string // Flash message
}

// New creates a new config editor model.
func New(configPath string) (Model, error) {
	cfgs, err := loadAllConfigs(configPath)
	if err != nil {
		return Model{}, fmt.Errorf("loading configs: %w", err)
	}
	configs := &cfgs

	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 80
	ti.Width = 40

	topItems := []topMenuItem{
		{"1", "System Configuration"},
		{"2", "Areas and Conferences"},
		{"3", "Echomail Networking"},
		{"4", "ViSiON/3 Networking (V3Net)"},
		{"5", "Door Programs"},
		{"6", "Transfer Protocols"},
		{"7", "Archivers"},
		{"8", "Event Scheduler"},
		{"9", "Login Sequence"},
		{"Q", "Quit Program"},
	}

	sysMenuItems := []sysConfigMenuItem{
		{"BBS Registration"},
		{"Server Setup"},
		{"Connection Limits"},
		{"Access Levels"},
		{"Default Settings"},
		{"IP Blocklist/Allowlist"},
		{"New User Voting (NUV)"},
		{"DOS Emulation"},
	}

	return Model{
		configs:      configs,
		origServer:   configs.Server,
		configPath:   configPath,
		topItems:     topItems,
		sysMenuItems: sysMenuItems,
		textInput:    ti,
		wizard:       &wizardState{},
		width:        minWidth,
		height:       minHeight,
		mode:         modeTopMenu,
	}, nil
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.SetWindowTitle("ViSiON/3 Configuration Editor")
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.width < minWidth {
			m.width = minWidth
		}
		if m.height < minHeight {
			m.height = minHeight
		}
		return m, nil

	case fetchNetworksMsg:
		return m.handleFetchNetworksMsg(msg)

	case fetchNALMsg:
		return m.handleFetchNALMsg(msg)

	case subscribeAreasMsg:
		return m.handleSubscribeAreasMsg(msg)

	case fetchRegistryMsg:
		return m.handleFetchRegistryMsg(msg)

	case tea.KeyMsg:
		prevMode := m.mode
		var result tea.Model
		var cmd tea.Cmd
		switch m.mode {
		case modeTopMenu:
			result, cmd = m.updateTopMenu(msg)
		case modeCategoryMenu:
			result, cmd = m.updateCategoryMenu(msg)
		case modeSysConfigMenu:
			result, cmd = m.updateSysConfigMenu(msg)
		case modeSysConfigEdit:
			result, cmd = m.updateSysConfigEdit(msg)
		case modeSysConfigField:
			result, cmd = m.updateSysConfigField(msg)
		case modeRecordList:
			result, cmd = m.updateRecordList(msg)
		case modeRecordReorder:
			result, cmd = m.updateRecordReorder(msg)
		case modeRecordEdit:
			result, cmd = m.updateRecordEdit(msg)
		case modeRecordField:
			result, cmd = m.updateRecordField(msg)
		case modeExitConfirm, modeSaveConfirm, modeDeleteConfirm:
			result, cmd = m.updateConfirm(msg)
		case modeLookupPicker:
			result, cmd = m.updateLookupPicker(msg)
		case modeHelp:
			result, cmd = m.updateHelp(msg)
		case modeWizardForm:
			result, cmd = m.updateWizardForm(msg)
		case modeWizardField:
			result, cmd = m.updateWizardField(msg)
		case modeV3NetWizardStep:
			result, cmd = m.updateV3NetWizardStep(msg)
		case modeV3NetIdentity:
			result, cmd = m.updateV3NetIdentity(msg)
		case modeV3NetHubAreas:
			result, cmd = m.updateV3NetHubAreas(msg)
		case modeV3NetAreaDeleteConfirm, modeV3NetAreaDeleteJAM:
			result, cmd = m.updateV3NetAreaDelete(msg)
		case modeV3NetAreaInsert:
			result, cmd = m.updateV3NetAreaInsert(msg)
		case modeV3NetAreaRename, modeV3NetAreaRenameJAM:
			result, cmd = m.updateV3NetAreaRename(msg)
		case modeNavSaveConfirm:
			result, cmd = m.updateNavSaveConfirm(msg)
		case modeWizardExitConfirm:
			result, cmd = m.updateWizardExitConfirm(msg)
		case modeV3NetAreaBrowser:
			result, cmd = m.updateV3NetAreaBrowser(msg)
		case modeRegistryBrowser:
			result, cmd = m.updateRegistryBrowser(msg)
		default:
			return m, nil
		}
		// Clear non-error flash messages when navigating to a different screen.
		if nm, ok := result.(Model); ok && nm.mode != prevMode && !strings.HasPrefix(nm.message, "SAVE ERROR") {
			nm.message = ""
			return nm, cmd
		}
		return result, cmd
	}
	return m, nil
}

// --- Top Menu Mode ---

func (m Model) updateTopMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyUp:
		if m.topCursor > 0 {
			m.topCursor--
		}
	case tea.KeyDown:
		if m.topCursor < len(m.topItems)-1 {
			m.topCursor++
		}
	case tea.KeyHome:
		m.topCursor = 0
	case tea.KeyEnd:
		m.topCursor = len(m.topItems) - 1
	case tea.KeyEnter:
		return m.selectTopMenuItem()
	case tea.KeyEscape:
		return m.tryExit()
	default:
		key := strings.ToUpper(msg.String())
		for i, item := range m.topItems {
			if item.Key == key {
				m.topCursor = i
				return m.selectTopMenuItem()
			}
		}
	}
	return m, nil
}

func (m Model) selectTopMenuItem() (Model, tea.Cmd) {
	switch m.topCursor {
	case 0: // System Configuration
		m.mode = modeSysConfigMenu
		m.sysMenuCursor = 0
		return m, nil

	case 1: // Areas and Conferences
		m.catMenuTitle = "Areas and Conferences"
		m.catMenuItems = []categoryMenuItem{
			{Label: "Message Areas", RecordType: "msgarea"},
			{Label: "File Areas", RecordType: "filearea"},
			{Label: "Conferences", RecordType: "conference"},
		}
		m.catMenuCursor = 0
		m.mode = modeCategoryMenu
		return m, nil

	case 2: // Echomail Networking
		m.catMenuTitle = "Echomail Networking"
		m.catMenuItems = []categoryMenuItem{
			{Label: "Echomail Networks", RecordType: "ftn"},
			{Label: "Echomail Links", RecordType: "ftnlink"},
		}
		m.catMenuCursor = 0
		m.mode = modeCategoryMenu
		return m, nil

	case 3: // V3Net Networking
		m.catMenuTitle = "ViSiON/3 Networking (V3Net)"
		m.catMenuItems = []categoryMenuItem{
			{Label: "Node Identity", Mode: modeV3NetIdentity},
			{Label: "Subscriptions", RecordType: "v3netleaf"},
			{Label: "Hosted Networks", RecordType: "v3nethub"},
		}
		m.catMenuCursor = 0
		m.mode = modeCategoryMenu
		return m, nil

	case 4: // Door Programs (direct)
		m.recordType = "door"
		m.recordCursor = 0
		m.recordScroll = 0
		m.returnMode = modeTopMenu
		m.mode = modeRecordList
		return m, nil

	case 5: // Transfer Protocols (direct)
		m.recordType = "protocol"
		m.recordCursor = 0
		m.recordScroll = 0
		m.returnMode = modeTopMenu
		m.mode = modeRecordList
		return m, nil

	case 6: // Archivers (direct)
		m.recordType = "archiver"
		m.recordCursor = 0
		m.recordScroll = 0
		m.returnMode = modeTopMenu
		m.mode = modeRecordList
		return m, nil

	case 7: // Event Scheduler (direct)
		m.recordType = "event"
		m.recordCursor = 0
		m.recordScroll = 0
		m.returnMode = modeTopMenu
		m.mode = modeRecordList
		return m, nil

	case 8: // Login Sequence (direct)
		m.recordType = "login"
		m.recordCursor = 0
		m.recordScroll = 0
		m.returnMode = modeTopMenu
		m.mode = modeRecordList
		return m, nil

	case 9: // Quit
		return m.tryExit()
	}
	return m, nil
}

// backMode returns the mode to navigate back to. If returnMode is set, it is
// used (and cleared); otherwise falls back to the top menu.
func (m *Model) backMode() editorMode {
	if m.returnMode != 0 {
		mode := m.returnMode
		m.returnMode = 0
		return mode
	}
	return modeTopMenu
}

func (m Model) tryExit() (Model, tea.Cmd) {
	if m.dirty {
		m.mode = modeExitConfirm
		m.confirmYes = true
		return m, nil
	}
	return m, tea.Quit
}

// --- Confirm Dialog ---

func (m Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyLeft, tea.KeyRight:
		m.confirmYes = !m.confirmYes
	case tea.KeyEnter:
		if m.confirmYes {
			return m.executeConfirm()
		}
		return m.rejectConfirm()
	case tea.KeyEscape:
		return m.cancelConfirm()
	default:
		switch msg.String() {
		case "y", "Y":
			m.confirmYes = true
			return m.executeConfirm()
		case "n", "N":
			return m.rejectConfirm()
		}
	}
	return m, nil
}

// cancelConfirm handles ESC on any confirm dialog — returns to the screen
// behind the dialog without taking action.
func (m Model) cancelConfirm() (Model, tea.Cmd) {
	switch m.mode {
	case modeExitConfirm:
		m.mode = modeTopMenu
		return m, nil
	case modeDeleteConfirm:
		m.mode = modeRecordList
		return m, nil
	default:
		m.mode = modeTopMenu
		return m, nil
	}
}

func (m Model) rejectConfirm() (Model, tea.Cmd) {
	switch m.mode {
	case modeExitConfirm:
		return m, tea.Quit
	case modeDeleteConfirm:
		m.mode = modeRecordList
		return m, nil
	default:
		m.mode = modeTopMenu
		return m, nil
	}
}

func (m Model) executeConfirm() (Model, tea.Cmd) {
	switch m.mode {
	case modeExitConfirm, modeSaveConfirm:
		m.saveAll()
		return m, tea.Quit
	case modeDeleteConfirm:
		m.deleteRecord()
		m.dirty = true
		m.mode = modeRecordList
		return m, nil
	}
	m.mode = modeTopMenu
	return m, nil
}

// --- Navigation Save Confirm ---

// promptNavSave shows a save-and-continue dialog. If not dirty, navigates
// directly to dest. Otherwise shows the confirmation overlay.
// source is the mode to return to if the user presses ESC (cancel).
func (m Model) promptNavSave(dest editorMode) (Model, tea.Cmd) {
	if !m.dirty {
		m.mode = dest
		return m, nil
	}
	m.navSaveDestMode = dest
	m.navSaveSourceMode = m.mode
	m.confirmYes = true
	m.mode = modeNavSaveConfirm
	return m, nil
}

func (m Model) updateNavSaveConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyLeft, tea.KeyRight:
		m.confirmYes = !m.confirmYes
	case tea.KeyEnter:
		if m.confirmYes {
			m.saveAll()
			m.mode = m.navSaveDestMode
			return m, nil
		}
		// No — navigate away without saving.
		m.mode = m.navSaveDestMode
		return m, nil
	case tea.KeyEscape:
		// Cancel — return to where the user was.
		m.mode = m.navSaveSourceMode
		return m, nil
	default:
		switch msg.String() {
		case "y", "Y":
			m.saveAll()
			m.mode = m.navSaveDestMode
			return m, nil
		case "n", "N":
			m.mode = m.navSaveDestMode
			return m, nil
		}
	}
	return m, nil
}

// --- Help Mode ---

func (m Model) updateHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.mode = modeTopMenu
	return m, nil
}
