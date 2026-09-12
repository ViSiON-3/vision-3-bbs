// Package menueditor implements the ViSiON/3 BBS Menu Editor TUI.
// It faithfully recreates the original MENUEDIT.EXE from Vision/2 (Turbo Pascal).
package menueditor

import (
	"fmt"

	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	minWidth  = 80
	minHeight = 25

	listVisible = 15 // rows visible in the menu/command list
)

// editorMode represents the current interaction state.
type editorMode int

const (
	modeMenuList          editorMode = iota // Scrollable list of all menus
	modeMenuEdit                            // Field editor for a single menu
	modeMenuEditField                       // Active text input on a menu field
	modeCommandList                         // Scrollable list of commands for selected menu
	modeCommandEdit                         // Field editor for a single command
	modeCommandEditField                    // Active text input on a command field
	modeDeleteMenuConfirm                   // Confirm menu delete
	modeDeleteCmdConfirm                    // Confirm command delete
	modeAddMenu                             // Input dialog: enter new menu filename
	modeExitConfirm                         // Unsaved changes on exit
	modeHelp                                // Help overlay
)

// Model is the BubbleTea model for the menu editor TUI.
type Model struct {
	set menuset.Set // menu set: shipped tree plus the overlay saves go to

	// Menu list state
	menus      []menuEntry
	menuCursor int
	menuScroll int

	// Menu edit state
	menuEditIdx int        // index into menus slice
	menuFields  []fieldDef // field definitions (from menuFields())
	menuEditFld int        // currently focused field index

	// Command list state
	cmds        []CmdData // commands loaded for cmdsMenuIdx
	cmdsMenuIdx int       // which menu index the cmds belong to
	cmdCursor   int
	cmdScroll   int

	// Command edit state
	cmdEditIdx int
	cmdFields  []fieldDef
	cmdEditFld int

	// Shared text input for field editing and prompts
	textInput textinput.Model

	// Confirm dialog
	confirmYes           bool
	deleteReturnMode     editorMode // mode to return to if delete is cancelled
	pendingDeleteMenuIdx int        // index of menu to delete (-1 if none pending)

	// Dirty tracking
	dirtyMenus map[string]bool // menu names with unsaved changes
	dirtyCmds  bool            // current command set has unsaved changes

	// Help overlay
	helpReturnMode editorMode // mode to restore when help is dismissed

	// Terminal dimensions
	width  int
	height int
	mode   editorMode

	// Backdrop art painted behind every screen. backdropArt holds the raw
	// bytes chosen at startup, reused when the backdrop is rebuilt on resize.
	backdrop    *backdrop
	backdropArt []byte

	message string // flash message (cleared on next key)
}

// New creates a new menu editor model over a menu set. When the set has an
// overlay, menus are read through it and every save lands in it.
func New(set menuset.Set) (Model, error) {
	menus, err := LoadMenus(set)
	if err != nil {
		return Model{}, fmt.Errorf("loading menus: %w", err)
	}

	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 80
	ti.Width = 40

	// Choose the backdrop screen once per startup; the bytes are reused on
	// resize so the picture behind the boxes does not change as the terminal
	// is dragged.
	art := pickBackdropArt()

	// Tell the sysop where saves will land before the first one happens; the
	// flash clears on the next key.
	notice := ""
	if set.HasOverlay() {
		notice = fmt.Sprintf("Saving to overlay %s (* = overlay file)", set.Overlay)
	}

	return Model{
		message:              notice,
		set:                  set,
		menus:                menus,
		menuFields:           menuFields(),
		cmdFields:            cmdFields(),
		textInput:            ti,
		dirtyMenus:           make(map[string]bool),
		pendingDeleteMenuIdx: -1,
		width:                minWidth,
		height:               minHeight,
		backdropArt:          art,
		backdrop:             loadBackdropFrom(art, minWidth, minHeight),
		mode:                 modeMenuList,
	}, nil
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.SetWindowTitle("ViSiON/3 Menu Editor")
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
		m.backdrop = loadBackdropFrom(m.backdropArt, m.width, m.height)
		return m, nil

	case tea.KeyMsg:
		m.message = "" // clear flash on any key
		switch m.mode {
		case modeMenuList:
			return m.updateMenuList(msg)
		case modeMenuEdit:
			return m.updateMenuEdit(msg)
		case modeMenuEditField:
			return m.updateMenuEditField(msg)
		case modeCommandList:
			return m.updateCommandList(msg)
		case modeCommandEdit:
			return m.updateCommandEdit(msg)
		case modeCommandEditField:
			return m.updateCommandEditField(msg)
		case modeDeleteMenuConfirm, modeDeleteCmdConfirm, modeExitConfirm:
			return m.updateConfirm(msg)
		case modeAddMenu:
			return m.updateAddMenu(msg)
		case modeHelp:
			m.mode = m.helpReturnMode
			return m, nil
		}
	}
	return m, nil
}

func (m *Model) clampMenuScroll() {
	total := len(m.menus)
	threshold := listVisible * 2 / 3
	if m.menuCursor < m.menuScroll {
		m.menuScroll = m.menuCursor
	}
	if m.menuCursor >= m.menuScroll+threshold {
		m.menuScroll = m.menuCursor - threshold
	}
	maxOffset := total - listVisible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.menuScroll > maxOffset {
		m.menuScroll = maxOffset
	}
	if m.menuScroll < 0 {
		m.menuScroll = 0
	}
}

func (m *Model) clampCmdScroll() {
	total := len(m.cmds)
	threshold := listVisible * 2 / 3
	if m.cmdCursor < m.cmdScroll {
		m.cmdScroll = m.cmdCursor
	}
	if m.cmdCursor >= m.cmdScroll+threshold {
		m.cmdScroll = m.cmdCursor - threshold
	}
	maxOffset := total - listVisible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.cmdScroll > maxOffset {
		m.cmdScroll = maxOffset
	}
	if m.cmdScroll < 0 {
		m.cmdScroll = 0
	}
}
