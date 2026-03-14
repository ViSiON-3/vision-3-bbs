package usereditor

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/crypto/bcrypt"

	"github.com/stlalpha/vision3/internal/config"
	"github.com/stlalpha/vision3/internal/user"
)

const (
	listVisible = 13 // Number of users visible in list
	minWidth    = 80
	minHeight   = 25
)

// editorMode represents the current interaction state.
type editorMode int

const (
	modeList            editorMode = iota // Main list browser
	modeEdit                              // Per-user field editor
	modeEditField                         // Actively editing a field value
	modeSearch                            // Search for user by handle
	modeDeleteConfirm                     // Confirm single user delete
	modeUndeleteConfirm                   // Confirm single user undelete
	modeMassDelete                        // Confirm mass delete of tagged
	modeValidate                          // Confirm auto-validate
	modeMassValidate                      // Confirm mass validate
	modeHelp                              // Help screen overlay
	modeFileChanged                       // File modified externally warning
	modeExitConfirm                       // Unsaved changes exit confirm
	modePasswordEntry                     // Password entry for reset
	modeSaveConfirm                       // Confirm save before exit
	modeSaveOnLeave                       // Prompt save when leaving edit screen
	modeExitClean                         // Simple exit confirmation (no unsaved changes)
	modeInfoAlert                         // Info alert dismissed by any key
	modePurgeConfirm                      // Confirm purge after delete
	modeMassPurge                         // Confirm purge all deleted users
)

// Model is the BubbleTea model for the user editor TUI.
type Model struct {
	// Data
	users         []*user.User // All users (sorted by ID)
	origUsers     []*user.User // Snapshot at load time (for dirty tracking)
	filePath      string
	dataDir       string    // Root data directory (parent of users/, infoforms/, etc.)
	fileMtime     time.Time // mtime at load for optimistic concurrency
	dirty         bool
	retentionDays int // Deleted user retention days from config (-1 = never purge)

	// List mode state
	cursor       int          // Current position in user list (0-based)
	scrollOffset int          // First visible row in the list
	listType     int          // Column view mode (1-4)
	listAlpha    bool         // Alphabetical sort active
	tagged       map[int]bool // Tagged user indices (0-based)

	// Edit mode state
	editIndex int        // Index into users slice being edited
	editField int        // Current field index (0-based)
	editDirty bool       // Whether changes were made during current edit session
	fields    []fieldDef // Field definitions

	// Text input (shared for editing fields, search, password)
	textInput textinput.Model

	// Search
	searchInput textinput.Model

	// Confirm dialog
	confirmYes      bool
	confirmFromEdit bool // true when confirm dialog was opened from edit screen

	// Info alert dialog
	alertTitle   string
	alertMessage string
	alertReturn  editorMode // mode to return to when dismissed

	// Terminal
	width   int
	height  int
	mode    editorMode
	message string // Flash message
}

// New creates a new user editor model.
// dataDir is the root data directory (e.g., "data/") containing users/, infoforms/, etc.
func New(filePath string, dataDir ...string) (Model, error) {
	users, mtime, err := LoadUsers(filePath)
	if err != nil {
		return Model{}, fmt.Errorf("loading users: %w", err)
	}

	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 80
	ti.Width = 40

	si := textinput.New()
	si.Placeholder = "Search handle..."
	si.CharLimit = 30
	si.Width = 25

	// Snapshot original users for dirty tracking
	origUsers := make([]*user.User, len(users))
	for i, u := range users {
		origUsers[i] = CloneUser(u)
	}

	// Resolve data directory
	dd := ""
	if len(dataDir) > 0 && dataDir[0] != "" {
		dd = dataDir[0]
	} else {
		// Default: go up from users.json -> users/ -> data/
		dd = filepath.Dir(filepath.Dir(filePath))
	}

	// Load retention days from config (best effort)
	retDays := -1
	configDir := filepath.Join(dd, "..", "configs")
	if cfg, err := config.LoadServerConfig(configDir); err == nil {
		retDays = cfg.DeletedUserRetentionDays
	}

	// Sort users by ID with deleted users at the bottom
	sortUsers(users, false)

	fields := editFields()

	// Wire up dynamic display fields that need runtime context.
	for i := range fields {
		switch fields[i].Label {
		case "InfoForms":
			capturedDD := dd
			fields[i].Get = func(u *user.User) string {
				return infoformStatus(capturedDD, u.ID)
			}
		case "Auto Purge":
			capturedRet := retDays
			fields[i].Get = func(u *user.User) string {
				if !u.DeletedUser || u.DeletedAt == nil {
					return ""
				}
				if capturedRet < 0 {
					return "Never"
				}
				purgeDate := u.DeletedAt.AddDate(0, 0, capturedRet)
				remaining := int(time.Until(purgeDate).Hours()/24) + 1
				if remaining <= 0 {
					return "Eligible now"
				}
				if remaining == 1 {
					return "1 day"
				}
				return fmt.Sprintf("%d days", remaining)
			}
		}
	}

	return Model{
		users:         users,
		origUsers:     origUsers,
		filePath:      filePath,
		dataDir:       dd,
		fileMtime:     mtime,
		retentionDays: retDays,
		cursor:        0,
		listType:      1,
		tagged:        make(map[int]bool),
		fields:        fields,
		textInput:     ti,
		searchInput:   si,
		width:         minWidth,
		height:        minHeight,
		mode:          modeList,
	}, nil
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.SetWindowTitle("ViSiON/3 User Editor")
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

	case tea.KeyMsg:
		switch m.mode {
		case modeList:
			return m.updateList(msg)
		case modeEdit:
			return m.updateEdit(msg)
		case modeEditField:
			return m.updateEditField(msg)
		case modeSearch:
			return m.updateSearch(msg)
		case modeDeleteConfirm, modeUndeleteConfirm, modeMassDelete, modePurgeConfirm, modeMassPurge,
			modeValidate, modeMassValidate,
			modeExitConfirm, modeExitClean, modeFileChanged, modeSaveConfirm, modeSaveOnLeave:
			return m.updateConfirm(msg)
		case modeInfoAlert:
			// Any key dismisses the alert
			m.mode = m.alertReturn
			return m, nil
		case modeHelp:
			return m.updateHelp(msg)
		case modePasswordEntry:
			return m.updatePassword(msg)
		}
	}
	return m, nil
}

// --- List Mode ---

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	total := len(m.users)
	if total == 0 {
		if msg.Type == tea.KeyEscape {
			return m, tea.Quit
		}
		return m, nil
	}

	switch msg.Type {
	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
		}
	case tea.KeyDown:
		if m.cursor < total-1 {
			m.cursor++
		}
	case tea.KeyHome:
		m.cursor = 0
	case tea.KeyEnd:
		m.cursor = total - 1
	case tea.KeyPgUp:
		m.cursor -= listVisible
		if m.cursor < 0 {
			m.cursor = 0
		}
	case tea.KeyPgDown:
		m.cursor += listVisible
		if m.cursor >= total {
			m.cursor = total - 1
		}
	case tea.KeyEnter:
		// Open edit screen for highlighted user
		m.editIndex = m.cursor
		m.editField = 0
		m.editDirty = false
		m.mode = modeEdit
		return m, nil
	case tea.KeyEscape:
		if m.dirty {
			m.mode = modeExitConfirm
			m.confirmYes = false
			return m, nil
		}
		m.mode = modeExitClean
		m.confirmYes = false
		return m, nil
	case tea.KeyF2:
		// Toggle delete on highlighted user (protect User 1)
		if m.cursor >= 0 && m.cursor < len(m.users) {
			u := m.users[m.cursor]
			if u.ID == 1 {
				m.alertTitle = "-- Cannot Delete --"
				m.alertMessage = "Cannot Delete User 1. Edit instead."
				m.alertReturn = modeList
				m.mode = modeInfoAlert
				return m, nil
			}
			if u.DeletedUser {
				m.mode = modeUndeleteConfirm
			} else {
				m.mode = modeDeleteConfirm
			}
			m.confirmYes = false
			m.confirmFromEdit = false
			return m, nil
		}
		return m, nil
	case tea.KeyF3:
		// Toggle alphabetical sort
		m.toggleSort()
		return m, nil
	case tea.KeyF4:
		// Purge deleted user
		if m.cursor >= 0 && m.cursor < len(m.users) {
			u := m.users[m.cursor]
			if u.ID == 1 {
				m.alertTitle = "-- Cannot Purge --"
				m.alertMessage = "Cannot Purge User 1."
				m.alertReturn = modeList
				m.mode = modeInfoAlert
				return m, nil
			}
			if !u.DeletedUser {
				m.alertTitle = "-- Cannot Purge --"
				m.alertMessage = "User must be deleted before purging."
				m.alertReturn = modeList
				m.mode = modeInfoAlert
				return m, nil
			}
			m.mode = modePurgeConfirm
			m.confirmYes = false
			m.confirmFromEdit = false
			return m, nil
		}
		return m, nil
	case tea.KeyF5:
		// Auto-validate highlighted user
		m.mode = modeValidate
		m.confirmYes = false
		m.confirmFromEdit = false
		return m, nil
	case tea.KeyF10:
		// Tag all users
		for i := range m.users {
			m.tagged[i] = true
		}
		return m, nil
	case tea.KeySpace:
		// Toggle tag
		m.tagged[m.cursor] = !m.tagged[m.cursor]
		if m.cursor < total-1 {
			m.cursor++
		}
		m.clampScroll()
		return m, nil
	default:
		switch msg.String() {
		case "left":
			if m.listType > 1 {
				m.listType--
			}
		case "right":
			if m.listType < 4 {
				m.listType++
			}
		case "shift+f2":
			// Mass delete tagged
			tagCount := m.taggedCount()
			if tagCount == 0 {
				m.message = "You have not tagged anyone to Delete!"
				return m, nil
			}
			m.mode = modeMassDelete
			m.confirmYes = false
			return m, nil
		case "shift+f4":
			// Mass purge all deleted users
			deletedCount := m.deletedCount()
			if deletedCount == 0 {
				m.message = "No deleted users to purge."
				return m, nil
			}
			m.mode = modeMassPurge
			m.confirmYes = false
			return m, nil
		case "shift+f5":
			// Mass validate tagged
			tagCount := m.taggedCount()
			if tagCount == 0 {
				m.message = "You have not tagged anyone to Quick-Validate!"
				return m, nil
			}
			m.mode = modeMassValidate
			m.confirmYes = false
			return m, nil
		case "shift+f10":
			// Untag all
			m.tagged = make(map[int]bool)
			return m, nil
		case "alt+h":
			m.mode = modeHelp
			return m, nil
		}
	}
	m.clampScroll()
	return m, nil
}

// --- Edit Mode (field navigation) ---

func (m Model) updateEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyTab, tea.KeyEnter:
		f := m.fields[m.editField]
		if f.Type == ftAction {
			// Password field - enter password entry mode
			m.mode = modePasswordEntry
			m.textInput.SetValue("")
			m.textInput.Placeholder = "New password..."
			m.textInput.EchoMode = textinput.EchoPassword
			m.textInput.CharLimit = 72 // bcrypt max input length
			m.textInput.Width = 48
			m.textInput.Focus()
			return m, textinput.Blink
		}
		if f.Type == ftDisplay {
			// Skip display-only fields
			m.editField = m.nextEditableField(1)
			return m, nil
		}
		// Start editing the current field
		return m.startFieldEdit()

	case tea.KeyDown:
		m.editField = m.nextEditableField(1)

	case tea.KeyUp:
		m.editField = m.nextEditableField(-1)

	case tea.KeyEscape:
		// Only prompt to save if changes were made during this edit session
		m.saveCurrentUser()
		if m.editDirty {
			m.mode = modeSaveOnLeave
			m.confirmYes = true
			return m, nil
		}
		m.mode = modeList
		return m, nil

	case tea.KeyPgDown:
		// Save current, go to next user
		m.saveCurrentUser()
		m.editIndex++
		if m.editIndex >= len(m.users) {
			m.editIndex = 0
		}
		m.editField = 0
		m.editDirty = false
		return m, nil

	case tea.KeyPgUp:
		// Save current, go to previous user
		m.saveCurrentUser()
		m.editIndex--
		if m.editIndex < 0 {
			m.editIndex = len(m.users) - 1
		}
		m.editField = 0
		m.editDirty = false
		return m, nil

	case tea.KeyF2:
		// Toggle delete on current user (protect User 1)
		if m.editIndex >= 0 && m.editIndex < len(m.users) {
			u := m.users[m.editIndex]
			if u.ID == 1 {
				m.alertTitle = "-- Cannot Delete --"
				m.alertMessage = "Cannot Delete User 1. Edit instead."
				m.alertReturn = modeEdit
				m.mode = modeInfoAlert
				return m, nil
			}
			if u.DeletedUser {
				m.mode = modeUndeleteConfirm
			} else {
				m.mode = modeDeleteConfirm
			}
			m.confirmYes = false
			m.confirmFromEdit = true
			return m, nil
		}
		return m, nil

	case tea.KeyF4:
		// Purge deleted user
		if m.editIndex >= 0 && m.editIndex < len(m.users) {
			u := m.users[m.editIndex]
			if u.ID == 1 {
				m.alertTitle = "-- Cannot Purge --"
				m.alertMessage = "Cannot Purge User 1."
				m.alertReturn = modeEdit
				m.mode = modeInfoAlert
				return m, nil
			}
			if !u.DeletedUser {
				m.alertTitle = "-- Cannot Purge --"
				m.alertMessage = "User must be deleted before purging."
				m.alertReturn = modeEdit
				m.mode = modeInfoAlert
				return m, nil
			}
			m.mode = modePurgeConfirm
			m.confirmYes = false
			m.confirmFromEdit = true
			return m, nil
		}
		return m, nil

	case tea.KeyF5:
		// Set defaults
		m.mode = modeValidate
		m.confirmYes = false
		m.confirmFromEdit = true
		return m, nil

	case tea.KeyF10:
		// Abort - discard changes for this user
		m.mode = modeList
		return m, nil

	default:
		switch msg.String() {
		case "ctrl+home":
			m.editField = m.firstEditableField()
		case "ctrl+end":
			m.editField = m.lastEditableField()
		}
	}
	return m, nil
}

// nextEditableField finds the next non-display field in the given direction (+1 or -1).
func (m Model) nextEditableField(dir int) int {
	n := len(m.fields)
	idx := m.editField
	for i := 0; i < n; i++ {
		idx += dir
		if idx > n-1 {
			idx = 0
		} else if idx < 0 {
			idx = n - 1
		}
		if m.fields[idx].Type != ftDisplay {
			return idx
		}
	}
	return m.editField // fallback (all display, shouldn't happen)
}

// firstEditableField returns the index of the first non-display field.
func (m Model) firstEditableField() int {
	for i, f := range m.fields {
		if f.Type != ftDisplay {
			return i
		}
	}
	return 0
}

// lastEditableField returns the index of the last non-display field.
func (m Model) lastEditableField() int {
	for i := len(m.fields) - 1; i >= 0; i-- {
		if m.fields[i].Type != ftDisplay {
			return i
		}
	}
	return len(m.fields) - 1
}

// startFieldEdit enters field editing mode for the current field.
func (m Model) startFieldEdit() (Model, tea.Cmd) {
	f := m.fields[m.editField]
	if f.Type == ftDisplay {
		return m, nil
	}

	u := m.users[m.editIndex]
	val := f.Get(u)

	m.mode = modeEditField
	m.textInput.SetValue(val)
	m.textInput.CharLimit = f.Width
	m.textInput.Width = f.Width
	m.textInput.EchoMode = textinput.EchoNormal
	m.textInput.Placeholder = ""
	m.textInput.CursorEnd()
	m.textInput.Focus()

	return m, textinput.Blink
}

// --- Field Editing Mode ---

func (m Model) updateEditField(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.fields[m.editField]

	switch msg.Type {
	case tea.KeyEnter, tea.KeyTab, tea.KeyDown:
		// Confirm and move to next field
		if err := m.applyFieldValue(f); err != nil {
			m.message = fmt.Sprintf("Invalid: %v", err)
			return m, nil
		}
		m.textInput.Blur()
		m.mode = modeEdit
		m.editField = m.nextEditableField(1)
		return m, nil

	case tea.KeyUp:
		// Confirm and move to previous field
		if err := m.applyFieldValue(f); err != nil {
			m.message = fmt.Sprintf("Invalid: %v", err)
			return m, nil
		}
		m.textInput.Blur()
		m.mode = modeEdit
		m.editField = m.nextEditableField(-1)
		return m, nil

	case tea.KeyEscape:
		// Cancel edit, don't apply
		m.textInput.Blur()
		m.mode = modeEdit
		return m, nil

	default:
		// For Y/N fields, only accept Y, N, y, n
		if f.Type == ftYesNo {
			if len(msg.Runes) == 1 {
				ch := msg.Runes[0]
				if ch == 'y' || ch == 'Y' {
					m.textInput.SetValue("Y")
				} else if ch == 'n' || ch == 'N' {
					m.textInput.SetValue("N")
				}
				// Auto-confirm Y/N
				if err := m.applyFieldValue(f); err == nil {
					m.textInput.Blur()
					m.mode = modeEdit
					m.editField = m.nextEditableField(1)
				}
				return m, nil
			}
			return m, nil
		}

		// For integer fields, filter non-numeric input
		if f.Type == ftInteger {
			if len(msg.Runes) == 1 {
				ch := msg.Runes[0]
				if ch < '0' || ch > '9' {
					if ch != '-' {
						return m, nil // Reject non-numeric
					}
				}
			}
		}

		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
}

// applyFieldValue validates and applies the current text input value to the user field.
func (m *Model) applyFieldValue(f fieldDef) error {
	val := m.textInput.Value()
	u := m.users[m.editIndex]

	switch f.Type {
	case ftInteger:
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("not a number")
		}
		if n < f.Min || n > f.Max {
			return fmt.Errorf("must be %d-%d", f.Min, f.Max)
		}
	case ftYesNo:
		upper := strings.ToUpper(val)
		if upper != "Y" && upper != "N" {
			return fmt.Errorf("must be Y or N")
		}
		val = upper
	}

	if f.Set != nil {
		if err := f.Set(u, val); err != nil {
			return err
		}
		u.UpdatedAt = time.Now()
		m.dirty = true
		m.editDirty = true
		m.message = ""
	}
	return nil
}

// --- Search Mode ---

func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		query := strings.ToLower(m.searchInput.Value())
		if query != "" {
			// Search from current position forward, wrapping
			for offset := 0; offset < len(m.users); offset++ {
				idx := (m.cursor + offset + 1) % len(m.users)
				u := m.users[idx]
				if strings.Contains(strings.ToLower(u.Handle), query) {
					m.cursor = idx
					m.message = fmt.Sprintf("Found: %s", u.Handle)
					break
				}
			}
		}
		m.clampScroll()
		m.mode = modeList
		m.searchInput.Blur()
		return m, nil

	case tea.KeyEscape:
		m.mode = modeList
		m.searchInput.Blur()
		return m, nil

	default:
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		return m, cmd
	}
}

// --- Password Entry ---

func (m Model) updatePassword(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		pw := m.textInput.Value()
		if pw != "" {
			// bcrypt has a 72-byte limit (not 72 characters);
			// non-ASCII runes may exceed 72 bytes before 72 chars.
			if len([]byte(pw)) > 72 {
				m.message = "Password too long (max 72 bytes)"
				return m, nil
			}
			hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
			if err != nil {
				m.message = fmt.Sprintf("Hash error: %v", err)
			} else {
				m.users[m.editIndex].PasswordHash = string(hash)
				m.users[m.editIndex].UpdatedAt = time.Now()
				m.dirty = true
				m.editDirty = true
				m.message = "Password updated"
			}
		}
		m.textInput.Blur()
		m.textInput.EchoMode = textinput.EchoNormal
		m.mode = modeEdit
		return m, nil

	case tea.KeyEscape:
		m.textInput.Blur()
		m.textInput.EchoMode = textinput.EchoNormal
		m.mode = modeEdit
		return m, nil

	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
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
		m.mode = m.previousMode()
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

// rejectConfirm handles the "No" response for confirm dialogs.
// For exit confirm, "No" means quit without saving.
func (m Model) rejectConfirm() (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeExitConfirm:
		// "Save changes before exit? No" → quit without saving
		return m, tea.Quit
	case modeExitClean:
		// "No" → cancel exit, return to list
		m.mode = modeList
		return m, nil
	case modeSaveOnLeave:
		// "No" → return to list without saving to disk
		m.mode = modeList
		return m, nil
	default:
		m.mode = m.previousMode()
		return m, nil
	}
}

func (m Model) previousMode() editorMode {
	switch m.mode {
	case modeDeleteConfirm, modeUndeleteConfirm, modePurgeConfirm, modeValidate:
		if m.confirmFromEdit {
			return modeEdit
		}
		return modeList
	case modeMassDelete, modeMassValidate, modeMassPurge:
		return modeList
	case modeExitConfirm, modeExitClean, modeSaveConfirm:
		return modeList
	case modeSaveOnLeave:
		return modeEdit
	case modeFileChanged:
		return modeList
	}
	return modeList
}

func (m Model) executeConfirm() (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeDeleteConfirm:
		idx := m.cursor
		if m.confirmFromEdit {
			idx = m.editIndex
		}
		m.softDeleteUser(idx)
		// After soft delete, offer to purge
		m.mode = modePurgeConfirm
		m.confirmYes = false
		return m, nil

	case modePurgeConfirm:
		idx := m.cursor
		if m.confirmFromEdit {
			idx = m.editIndex
		}
		m.purgeUser(idx)
		if m.confirmFromEdit {
			m.mode = modeEdit
		} else {
			m.mode = modeList
		}
		return m, nil

	case modeUndeleteConfirm:
		idx := m.cursor
		if m.confirmFromEdit {
			idx = m.editIndex
		}
		m.undeleteUser(idx)
		if m.confirmFromEdit {
			m.mode = modeEdit
		} else {
			m.mode = modeList
		}
		return m, nil

	case modeMassPurge:
		// Purge all deleted users (iterate in reverse to keep indices stable)
		purged := 0
		for i := len(m.users) - 1; i >= 0; i-- {
			if m.users[i].DeletedUser {
				m.purgeUser(i)
				purged++
			}
		}
		m.message = fmt.Sprintf("Purged %d deleted user(s)", purged)
		m.mode = modeList
		return m, nil

	case modeMassDelete:
		for i := range m.users {
			if m.tagged[i] {
				m.softDeleteUser(i)
			}
		}
		m.tagged = make(map[int]bool)
		m.mode = modeList
		return m, nil

	case modeValidate:
		idx := m.cursor
		if m.confirmFromEdit {
			idx = m.editIndex
		}
		m.autoValidateUser(idx)
		if m.confirmFromEdit {
			m.mode = modeEdit
		} else {
			m.mode = modeList
		}
		return m, nil

	case modeMassValidate:
		for i := range m.users {
			if m.tagged[i] {
				m.autoValidateUser(i)
			}
		}
		m.tagged = make(map[int]bool)
		m.mode = modeList
		return m, nil

	case modeExitConfirm:
		// Save and quit
		m.saveAllToDisk()
		return m, tea.Quit

	case modeExitClean:
		// No unsaved changes, just quit
		return m, tea.Quit

	case modeSaveConfirm:
		m.saveAllToDisk()
		return m, tea.Quit

	case modeFileChanged:
		// Force overwrite
		m.saveAllToDisk()
		m.mode = modeList
		return m, nil

	case modeSaveOnLeave:
		// Save to disk and return to list
		m.saveAllToDisk()
		// Only go to list if saveAllToDisk didn't switch to modeFileChanged
		if m.mode == modeSaveOnLeave {
			m.mode = modeList
		}
		return m, nil
	}

	m.mode = modeList
	return m, nil
}

// --- Help Mode ---

func (m Model) updateHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Any key dismisses help
	m.mode = modeList
	return m, nil
}

// --- Helper Methods ---

func (m *Model) softDeleteUser(idx int) {
	if idx < 0 || idx >= len(m.users) {
		return
	}
	u := m.users[idx]
	if u.ID == 1 {
		m.message = "Can't delete User 1. Edit instead."
		return
	}
	u.DeletedUser = true
	now := time.Now()
	u.DeletedAt = &now
	u.UpdatedAt = now
	m.dirty = true
	m.editDirty = true
	m.message = fmt.Sprintf("Deleted: %s", u.Handle)
	m.resortAndTrack(u)
}

func (m *Model) undeleteUser(idx int) {
	if idx < 0 || idx >= len(m.users) {
		return
	}
	u := m.users[idx]
	u.DeletedUser = false
	u.DeletedAt = nil
	u.UpdatedAt = time.Now()
	m.dirty = true
	m.editDirty = true
	m.message = fmt.Sprintf("Undeleted: %s", u.Handle)
	m.resortAndTrack(u)
}

// purgeUser permanently removes a user from the list and deletes their data files
// (infoform responses, etc.). The user record is removed from the in-memory list.
func (m *Model) purgeUser(idx int) {
	if idx < 0 || idx >= len(m.users) {
		return
	}
	u := m.users[idx]
	handle := u.Handle
	userID := u.ID

	// Remove infoform responses for this user
	if m.dataDir != "" {
		responsesDir := filepath.Join(m.dataDir, "infoforms", "responses")
		pattern := filepath.Join(responsesDir, fmt.Sprintf("%d_*.json", userID))
		matches, _ := filepath.Glob(pattern)
		for _, f := range matches {
			os.Remove(f)
		}
	}

	// Remove user from the list
	m.users = append(m.users[:idx], m.users[idx+1:]...)

	// Fix tagged indices (shift down indices above removed)
	newTagged := make(map[int]bool)
	for k, v := range m.tagged {
		if k < idx {
			newTagged[k] = v
		} else if k > idx {
			newTagged[k-1] = v
		}
	}
	m.tagged = newTagged

	// Clamp cursor and editIndex
	if m.cursor >= len(m.users) {
		m.cursor = len(m.users) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.editIndex = m.cursor

	m.dirty = true
	m.editDirty = true
	m.message = fmt.Sprintf("Purged: %s", handle)
}

// resortAndTrack re-sorts the user list and updates cursor/editIndex to follow the given user.
func (m *Model) resortAndTrack(target *user.User) {
	sortUsers(m.users, m.listAlpha)
	for i, u := range m.users {
		if u == target {
			m.cursor = i
			m.editIndex = i
			break
		}
	}
}

func (m *Model) autoValidateUser(idx int) {
	if idx < 0 || idx >= len(m.users) {
		return
	}
	u := m.users[idx]
	u.AccessLevel = 10
	u.Validated = true
	u.FilePoints = 100
	u.TimeLimit = 60
	u.UpdatedAt = time.Now()
	m.dirty = true
	m.editDirty = true
	m.message = fmt.Sprintf("Validated: %s", u.Handle)
}

func (m *Model) toggleSort() {
	m.listAlpha = !m.listAlpha
	if m.listAlpha {
		m.message = "Alphabetizing User List.. Weeee!"
		// Sort by handle alphabetically
		sortUsers(m.users, true)
	} else {
		m.message = "Restoring User List to Original Order!"
		// Sort by ID
		sortUsers(m.users, false)
	}
	m.cursor = 0
	m.scrollOffset = 0
}

func (m *Model) saveCurrentUser() {
	// Nothing special needed - users slice is modified in-place
	// Dirty flag is already set by field edits
}

func (m *Model) saveAllToDisk() {
	if !m.dirty {
		return
	}

	// Check for external modification
	if CheckFileChanged(m.filePath, m.fileMtime) {
		m.mode = modeFileChanged
		m.confirmYes = false
		m.message = "File modified externally! Overwrite?"
		return
	}

	newMtime, err := SaveUsers(m.filePath, m.users)
	if err != nil {
		m.message = fmt.Sprintf("SAVE ERROR: %v", err)
		return
	}
	m.fileMtime = newMtime
	m.dirty = false
	m.message = "Saved successfully"

	// Update original snapshot
	m.origUsers = make([]*user.User, len(m.users))
	for i, u := range m.users {
		m.origUsers[i] = CloneUser(u)
	}
}

// clampScroll adjusts scrollOffset so the cursor is always visible,
// with the lightbar stopping at ~2/3 of the visible area before scrolling.
// scrollOffset is in display-row space (accounts for separator row).
func (m *Model) clampScroll() {
	totalDisplay := len(m.buildDisplayRows())
	displayPos := m.cursorToDisplayRow(m.cursor)

	// Scroll threshold: lightbar stops at this row (0-indexed) and list starts scrolling
	scrollThreshold := listVisible * 2 / 3 // ~8 for 13 visible rows

	// If cursor is above the visible window, scroll up to show it at top
	if displayPos < m.scrollOffset {
		m.scrollOffset = displayPos
	}

	// If cursor has moved past the threshold row, scroll to keep it at threshold
	if displayPos >= m.scrollOffset+scrollThreshold {
		m.scrollOffset = displayPos - scrollThreshold
	}

	// Don't scroll past the end of the list
	maxOffset := totalDisplay - listVisible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.scrollOffset > maxOffset {
		m.scrollOffset = maxOffset
	}

	// Don't scroll before the beginning
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

func (m Model) taggedCount() int {
	count := 0
	for _, tagged := range m.tagged {
		if tagged {
			count++
		}
	}
	return count
}

func (m Model) deletedCount() int {
	count := 0
	for _, u := range m.users {
		if u.DeletedUser {
			count++
		}
	}
	return count
}

// sortUsers sorts users by handle (alpha) or by ID (numeric).
// Deleted users are always sorted to the bottom of the list.
func sortUsers(users []*user.User, alpha bool) {
	if alpha {
		for i := 1; i < len(users); i++ {
			for j := i; j > 0; j-- {
				if shouldSwap(users[j], users[j-1], func(a, b *user.User) bool {
					return strings.ToLower(a.Handle) < strings.ToLower(b.Handle)
				}) {
					users[j], users[j-1] = users[j-1], users[j]
				} else {
					break
				}
			}
		}
	} else {
		for i := 1; i < len(users); i++ {
			for j := i; j > 0; j-- {
				if shouldSwap(users[j], users[j-1], func(a, b *user.User) bool {
					return a.ID < b.ID
				}) {
					users[j], users[j-1] = users[j-1], users[j]
				} else {
					break
				}
			}
		}
	}
}

// shouldSwap returns true if a should come before b, with deleted users always last.
func shouldSwap(a, b *user.User, less func(a, b *user.User) bool) bool {
	if a.DeletedUser != b.DeletedUser {
		// Non-deleted users come first
		return !a.DeletedUser && b.DeletedUser
	}
	return less(a, b)
}
