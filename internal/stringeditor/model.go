// Package stringeditor is the full-screen Bubble Tea editor for the BBS's
// customizable prompt and message strings (strings.json), modelled on the
// original ViSiON/2 STRINGS.EXE. New builds the Model that the ./strings
// command runs; it lists every string in the catalog (StringEntries) with a
// live color preview, and lets the sysop edit, search, restore factory
// defaults and save. RenderColorString and PlainTextLength, which render and
// measure pipe-coded text, are also used by the menu editor.
package stringeditor

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/stringformat"
	"github.com/ViSiON-3/vision-3-bbs/internal/stringhistory"
	"github.com/ViSiON-3/vision-3-bbs/internal/tuiart"
	"github.com/ViSiON-3/vision-3-bbs/internal/tuiart/colorpick"
)

const (
	numWidth = 3  // Right-aligned item number
	labelCol = 30 // Column where values start (dcol in Pascal)
	// labelWidth is the room left for the label between the item number and
	// the value column, after the two bracket characters around it.
	labelWidth = labelCol - numWidth - 2
	// markerWidth is the single column between the label and the value that
	// flags the entry's state.
	markerWidth = 1
	minWidth    = 80 // Minimum terminal width
	minHeight   = 25 // Minimum terminal height (matching 80x25 DOS)

	// The DOS list panel is 80 columns at the minimum terminal size and fills a
	// larger one, leaving artMargin columns of background as a border on each
	// side. The border is deliberately thin: a wide band of fill around a flat
	// list reads as wasted screen rather than as framing, and the extra columns
	// are better spent on the value preview.
	artMargin = 1

	// chromeRows counts the rows the list cannot use: the global header bar,
	// the status bar, the column header, and the message, description and help
	// bars.
	chromeRows = 6

	// minItemsPerPage is the page size at the minimum 80x25 terminal.
	minItemsPerPage = minHeight - chromeRows

	// maxItemsPerPage caps how far a page grows on a tall terminal. Beyond
	// this the description bar is too far from the selection to read as its
	// caption, and a page stops being a useful navigation unit.
	maxItemsPerPage = 60
)

// valueWidthFor returns the cells available for a value preview on a terminal
// of the given width: everything in the panel right of the label column.
func valueWidthFor(width int) int {
	return max(10, panelWidthFor(width)-labelCol-markerWidth)
}

// pageSizeFor returns the number of list rows a terminal of the given height
// can show, clamped to the documented minimum and maximum.
func pageSizeFor(height int) int {
	size := height - chromeRows
	if size < minItemsPerPage {
		return minItemsPerPage
	}
	if size > maxItemsPerPage {
		return maxItemsPerPage
	}
	return size
}

// editorMode represents the current interaction state.
type editorMode int

const (
	modeNavigate editorMode = iota
	modeEdit
	modeAbortConfirm
	modeRevertConfirm
	modeDefaultConfirm
	modeSearch
	// modePicker is the colour picker, open over the edit input (picker.go).
	modePicker
)

// Model is the BubbleTea model for the string editor TUI.
type Model struct {
	// Data
	catalog         []StringEntry     // Full ordered metadata catalog
	entries         []StringEntry     // Catalog entries currently listed
	showReserved    bool              // Whether reserved placeholders are listed
	values          map[string]string // Current string values (key -> value)
	origValues      map[string]string // Values as loaded from disk (for revert)
	shippedDefaults map[string]string // Factory defaults (for F4 restore)
	filePath        string            // Path to strings.json
	dirty           bool              // Whether values have been modified

	// extra holds the file's entries that are not strings (an older file's
	// numeric defColorN), kept as they are on save. bases is the
	// config.StringBasesKey entry: per customised string, the fingerprint of
	// the shipped default it replaced. kept marks strings whose fingerprint
	// a save refreshes: edited this session, or kept with F5 after their
	// default changed.
	extra map[string]json.RawMessage
	bases map[string]string
	kept  map[string]bool

	// Navigation
	cursor   int // Current item index (0-based, across all pages)
	page     int // Current page (0-based)
	pageSize int // List rows on the current terminal
	numPages int

	// UI state
	mode   editorMode
	width  int
	height int

	// Background painted in the margins around the list panel. The config
	// editor's ANSI art is 80 columns wide and centered, so a list panel that
	// is itself at least 80 wide would hide it completely; this editor uses
	// the shared shaded fill instead.
	backdrop *tuiart.Backdrop

	// Editing
	textInput textinput.Model
	editKey   string // The key being edited
	editErr   string // Escape-syntax error blocking the current edit

	// Confirm dialog
	confirmYes bool // true = Yes selected in confirm dialog

	// Colour picker (picker.go): the chosen foreground 0-15, the chosen
	// background 0-7 or pickerNoBG, and which row the cursor is on.
	pick colorpick.Picker

	// Search
	searchInput textinput.Model

	// Message (flash message shown briefly)
	message string

	// warnedFormat records that the save-time format warning has been shown,
	// so a second F10 saves rather than repeating it forever.
	warnedFormat bool
}

// New creates a new string editor model.
// shippedDefaults, if non-nil, provides factory default values for F4 restore.
func New(filePath string, shippedDefaults map[string]string) (Model, error) {
	catalog := StringEntries()
	values, extra, err := loadStringsFile(filePath)
	if err != nil {
		return Model{}, fmt.Errorf("loading strings: %w", err)
	}
	// A key spelt with other capitals sets the same string for the BBS
	// (encoding/json ignores case), so the editor shows it under its own
	// key, and a save writes that spelling.
	for k, v := range values {
		if canon, ok := config.CanonicalStringKey(k); ok && canon != k {
			if _, exact := values[canon]; !exact {
				values[canon] = v
			}
			delete(values, k)
		}
	}
	// A string still holding text it shipped with, in this release or an
	// older one, was never edited by the sysop: it is the default, and a
	// save leaves the key out so later wording reaches it, as doctor --fix
	// does.
	for k, v := range values {
		if _, ok := config.SupersededString(k, v); ok || stringhistory.Shipped(k, v) {
			delete(values, k)
		}
	}
	var bases map[string]string
	if raw, ok := extra[config.StringBasesKey]; ok {
		bases = config.StringBases(map[string]json.RawMessage{config.StringBasesKey: raw})
		delete(extra, config.StringBasesKey)
	}
	if bases == nil {
		bases = map[string]string{}
	}

	ti := textinput.New()
	// No CharLimit: a limit here silently truncates the sysop's value, and the
	// escaped form of a long string is longer than the string itself.
	ti.CharLimit = 0
	// The input is drawn inline in the value column, so it carries no prompt of
	// its own; Width leaves one cell for the cursor. The first WindowSizeMsg
	// replaces this, but a key can arrive before it, so the initial value is
	// the value column at the minimum terminal size rather than a placeholder.
	ti.Prompt = ""
	ti.Width = valueWidthFor(minWidth) - 1

	si := textinput.New()
	si.Placeholder = "Search..."
	si.CharLimit = 40
	si.Width = 30

	pageSize := pageSizeFor(minHeight)

	// Snapshot original values for revert support
	origValues := make(map[string]string, len(values))
	for k, v := range values {
		origValues[k] = v
	}

	m := Model{
		catalog:         catalog,
		values:          values,
		origValues:      origValues,
		shippedDefaults: shippedDefaults,
		filePath:        filePath,
		extra:           extra,
		bases:           bases,
		kept:            map[string]bool{},
		cursor:          0,
		page:            0,
		pageSize:        pageSize,
		mode:            modeNavigate,
		width:           minWidth,
		height:          minHeight,
		textInput:       ti,
		searchInput:     si,
		confirmYes:      false,
		pick:            colorpick.New(dosWhite, false),
		backdrop:        tuiart.Shaded(minWidth, minHeight),
	}
	m.rebuildEntries()
	return m, nil
}

// rebuildEntries refreshes the listed entries from the catalog, applying the
// reserved filter, and keeps the cursor on the same string where it can.
func (m *Model) rebuildEntries() {
	var keepKey string
	if m.cursor >= 0 && m.cursor < len(m.entries) {
		keepKey = m.entries[m.cursor].Key
	}

	m.entries = m.entries[:0]
	for _, e := range m.catalog {
		if !m.showReserved && isReservedKey(e.Key) {
			continue
		}
		m.entries = append(m.entries, e)
	}
	if len(m.entries) == 0 {
		// Never present an empty list: with every entry filtered out there
		// would be nothing to select and no way back.
		m.entries = append(m.entries, m.catalog...)
	}

	m.numPages = (len(m.entries) + m.pageSize - 1) / m.pageSize
	m.cursor = 0
	if keepKey != "" {
		for i, e := range m.entries {
			if e.Key == keepKey {
				m.cursor = i
				break
			}
		}
	}
	m.clampCursor()
}

// clampCursor keeps the cursor in range and the page showing it.
func (m *Model) clampCursor() {
	if m.cursor >= len(m.entries) {
		m.cursor = len(m.entries) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.page = m.cursor / m.pageSize
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.SetWindowTitle("ViSiON/3 String Configuration")
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
		m.backdrop = tuiart.Shaded(m.width, m.height)
		m.textInput.Width = m.valueWidth() - 1
		// Re-page around the cursor so the selection stays on screen when the
		// page size changes.
		m.pageSize = pageSizeFor(m.height)
		m.numPages = (len(m.entries) + m.pageSize - 1) / m.pageSize
		m.clampCursor()
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case modeNavigate:
			return m.updateNavigate(msg)
		case modeEdit:
			return m.updateEdit(msg)
		case modeAbortConfirm, modeRevertConfirm, modeDefaultConfirm:
			return m.updateConfirm(msg)
		case modeSearch:
			return m.updateSearch(msg)
		case modePicker:
			return m.updatePicker(msg)
		}
	}
	return m, nil
}

// updateNavigate handles keys in navigation mode.
func (m Model) updateNavigate(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
			m.page = m.cursor / m.pageSize
		}
	case tea.KeyDown:
		if m.cursor < len(m.entries)-1 {
			m.cursor++
			m.page = m.cursor / m.pageSize
		}
	case tea.KeyPgUp:
		m.page--
		if m.page < 0 {
			m.page = 0
		}
		m.cursor = m.page * m.pageSize
	case tea.KeyPgDown:
		m.page++
		if m.page >= m.numPages {
			m.page = m.numPages - 1
		}
		m.cursor = m.page * m.pageSize
		if m.cursor >= len(m.entries) {
			m.cursor = len(m.entries) - 1
		}
	case tea.KeyHome:
		m.cursor = 0
		m.page = 0
	case tea.KeyEnd:
		m.cursor = len(m.entries) - 1
		m.page = m.numPages - 1
	case tea.KeyEnter:
		return m.startEdit("")
	case tea.KeyEscape:
		m.mode = modeAbortConfirm
		m.confirmYes = false
		return m, nil
	case tea.KeyF1:
		// Edit with pre-filled value
		entry := m.entries[m.cursor]
		return m.startEdit(m.editBase(entry.Key))
	case tea.KeyF3:
		// Revert current item to original (on-disk) value
		entry := m.entries[m.cursor]
		if isReservedKey(entry.Key) {
			m.message = "This field is reserved"
			return m, nil
		}
		orig, inOrig := m.origValues[entry.Key]
		cur, inCur := m.values[entry.Key]
		if inOrig == inCur && orig == cur && !m.kept[entry.Key] {
			m.message = "Nothing to revert"
			return m, nil
		}
		m.mode = modeRevertConfirm
		m.confirmYes = false
		return m, nil
	case tea.KeyF4:
		// Restore current item to ViSiON/3 default
		entry := m.entries[m.cursor]
		if isReservedKey(entry.Key) {
			m.message = "This field is reserved"
			return m, nil
		}
		if _, set := m.values[entry.Key]; !set {
			m.message = "Already the ViSiON/3 default"
			return m, nil
		}
		if _, ok := m.defaultFor(entry.Key); !ok {
			m.message = "No ViSiON/3 default for this string; set it blank to print nothing"
			return m, nil
		}
		m.mode = modeDefaultConfirm
		m.confirmYes = false
		return m, nil
	case tea.KeyF5:
		// Keep a customised string whose shipped default has changed.
		entry := m.entries[m.cursor]
		if m.stateOf(entry.Key) != stateChanged {
			m.message = "F5 keeps your text when the shipped default changes under it"
			return m, nil
		}
		m.kept[entry.Key] = true
		m.message = fmt.Sprintf("Keeping yours: %s", entry.Label)
		m.recomputeDirty()
		return m, nil
	case tea.KeyF10:
		// Save and exit. Report format mismatches once before writing, but do
		// not block: an older file may already contain one, and refusing to
		// save would trap every unrelated edit behind someone else's mistake.
		if problems := m.formatProblems(); len(problems) > 0 && !m.warnedFormat {
			m.warnedFormat = true
			m.message = fmt.Sprintf("%d string(s) do not match their arguments (%s%s) - F10 again to save anyway",
				len(problems), problems[0].Key, plural(len(problems)))
			return m, nil
		}
		if err := m.save(); err != nil {
			m.message = fmt.Sprintf("ERROR: %v", err)
			return m, nil
		}
		return m, tea.Quit

	default:
		switch msg.String() {
		case "ctrl+r":
			// Toggle listing of reserved placeholder entries.
			m.showReserved = !m.showReserved
			m.rebuildEntries()
			if m.showReserved {
				m.message = "Showing reserved placeholders"
			} else {
				m.message = "Hiding reserved placeholders"
			}
			return m, nil
		case "/":
			// Enter search mode
			m.mode = modeSearch
			m.searchInput.SetValue("")
			m.searchInput.Focus()
			return m, textinput.Blink
		default:
			// If a printable character is typed, start editing with that char
			if len(msg.Runes) == 1 && msg.Runes[0] >= 32 {
				return m.startEdit(string(msg.Runes))
			}
		}
	}
	return m, nil
}

// startEdit enters edit mode for the currently selected item. prefill is a raw
// (unescaped) value; it is converted to the editable escaped form so control
// characters survive a round trip through the single-line input.
func (m Model) startEdit(prefill string) (tea.Model, tea.Cmd) {
	entry := m.entries[m.cursor]
	if isReservedKey(entry.Key) {
		// Can't edit placeholder entries
		m.message = "This field is reserved and cannot be edited"
		return m, nil
	}
	m.mode = modeEdit
	m.editKey = entry.Key
	m.editErr = ""
	m.textInput.SetValue(EscapeForEdit(prefill))
	m.textInput.CursorEnd()
	m.textInput.Focus()
	m.message = ""
	return m, textinput.Blink
}

// updateEdit handles keys in edit mode.
func (m Model) updateEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		// Confirm edit. A malformed escape keeps the sysop in the input with
		// their text intact rather than writing a guess to disk.
		newVal, err := UnescapeFromEdit(m.textInput.Value())
		if err != nil {
			m.editErr = err.Error()
			return m, nil
		}
		if newVal != m.editBase(m.editKey) {
			m.values[m.editKey] = newVal
			m.kept[m.editKey] = true
			m.recomputeDirty()
		}
		m.mode = modeNavigate
		m.editErr = ""
		m.textInput.Blur()
		// Warn but accept. The sysop may be mid-way through a rewrite, and
		// refusing the edit would lose the text they just typed; the warning
		// stays visible on the message bar and the save check repeats it.
		if err := m.formatProblem(m.editKey, newVal); err != nil {
			m.message = "WARNING: " + err.Error()
		}
		return m, nil
	case tea.KeyEscape:
		// Cancel edit
		m.mode = modeNavigate
		m.editErr = ""
		m.textInput.Blur()
		return m, nil
	case tea.KeyF2:
		return m.openPicker()
	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		m.editErr = ""
		return m, cmd
	}
}

// updateConfirm handles keys in any confirmation dialog (abort, revert, default).
func (m Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyLeft, tea.KeyRight:
		m.confirmYes = !m.confirmYes
	case tea.KeyEnter:
		if m.confirmYes {
			return m.executeConfirm()
		}
		m.mode = modeNavigate
	case tea.KeyEscape:
		m.mode = modeNavigate
	default:
		switch msg.String() {
		case "y", "Y":
			m.confirmYes = true
			return m.executeConfirm()
		case "n", "N":
			m.mode = modeNavigate
		}
	}
	return m, nil
}

// executeConfirm performs the confirmed action based on the current mode.
func (m Model) executeConfirm() (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeAbortConfirm:
		return m, tea.Quit

	case modeRevertConfirm:
		entry := m.entries[m.cursor]
		if orig, ok := m.origValues[entry.Key]; ok {
			m.values[entry.Key] = orig
		} else {
			delete(m.values, entry.Key)
		}
		delete(m.kept, entry.Key)
		m.message = fmt.Sprintf("Reverted: %s", entry.Label)
		m.recomputeDirty()
		m.mode = modeNavigate
		return m, nil

	case modeDefaultConfirm:
		entry := m.entries[m.cursor]
		delete(m.values, entry.Key)
		delete(m.kept, entry.Key)
		m.message = fmt.Sprintf("Restored default: %s", entry.Label)
		m.recomputeDirty()
		m.mode = modeNavigate
		return m, nil
	}

	m.mode = modeNavigate
	return m, nil
}

// updateSearch handles keys in search mode.
func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		// Find first match
		query := strings.ToLower(m.searchInput.Value())
		if query != "" {
			// Search from current position forward, wrapping. offset runs to
			// len(m.entries), so the cursor's own entry is examined last
			// rather than skipped.
			found := false
			for offset := 0; offset < len(m.entries); offset++ {
				idx := (m.cursor + offset + 1) % len(m.entries)
				entry := m.entries[idx]
				if strings.Contains(strings.ToLower(entry.Label), query) ||
					strings.Contains(strings.ToLower(entry.Key), query) ||
					strings.Contains(strings.ToLower(entry.Description), query) {
					m.cursor = idx
					m.page = idx / m.pageSize
					m.message = fmt.Sprintf("Found: %s", entry.Label)
					found = true
					break
				}
			}
			if !found {
				// Say so. Returning to the list unchanged and silent reads as
				// the search key not having worked.
				m.message = fmt.Sprintf("No entry matching %q", m.searchInput.Value())
			}
		}
		m.mode = modeNavigate
		m.searchInput.Blur()
		return m, nil
	case tea.KeyEscape:
		m.mode = modeNavigate
		m.searchInput.Blur()
		return m, nil
	default:
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		return m, cmd
	}
}

// defaultFor returns what the BBS prints for key when strings.json leaves it
// out: the shipped value, or for a string the template doesn't carry, the
// runtime's compiled-in default. ok is false when there is none at all.
func (m Model) defaultFor(key string) (string, bool) {
	_, shipped := m.shippedDefaults[key]
	_, fallback := config.StringFallbacks[key]
	_, missing := config.StringMissingDefaults[key]
	return m.effective(key, "", false), shipped || fallback || missing
}

// effective is what the BBS prints for key with the given value, as
// config.EffectiveString works it out, against the editor's defaults.
func (m Model) effective(key, value string, present bool) string {
	return config.EffectiveStringWith(m.shippedDefaults, key, value, present)
}

// isDefault reports whether key's value prints the same as leaving it out,
// so a save need not write it.
func (m Model) isDefault(key string) bool {
	v, present := m.values[key]
	if !present {
		return true
	}
	if isReservedKey(key) {
		return false
	}
	if _, known := m.defaultFor(key); !known && v != "" {
		return false
	}
	return m.effective(key, v, true) == m.effective(key, "", false)
}

// save writes strings.json with only what is the sysop's: values that print
// differently from the default, keys the editor doesn't know (doctor lists
// those), the entries that are not strings, and a fingerprint of the
// current default for each customised string, so a later release can tell
// when its default changes.
func (m Model) save() error {
	out := map[string]string{}
	bases := map[string]string{}
	for k, v := range m.values {
		if m.isDefault(k) {
			continue
		}
		out[k] = v
		if !config.IsStringKey(k) {
			continue
		}
		if base, ok := m.bases[k]; ok && !m.kept[k] {
			bases[k] = base
		} else {
			bases[k] = stringhistory.Hash(m.effective(k, "", false))
		}
	}
	extra := maps.Clone(m.extra)
	if len(bases) > 0 {
		data, err := json.Marshal(bases)
		if err != nil {
			return err
		}
		extra[config.StringBasesKey] = data
	}
	return saveStringsFile(m.filePath, out, extra)
}

// formatProblem checks one value's directives against the shipped default's.
func (m Model) formatProblem(key, value string) error {
	def, ok := m.defaultFor(key)
	if !ok {
		return nil
	}
	if _, canSwitchOff := config.StringMissingDefaults[key]; canSwitchOff && value == "" {
		return nil // saved blank, this string is off; its call sites check for it
	}
	return stringformat.ValidateValue(key, value, config.StringFallbacks[key], def)
}

// formatProblems reports every configured string whose directives no longer
// match the arguments its call site passes. It checks the strings as the
// BBS prints them, the defaults with the file's values over them, and
// reports only those the file sets.
func (m Model) formatProblems() []stringformat.Problem {
	defaults := stringformat.MergeDefaults(config.StringFallbacks, m.shippedDefaults)
	merged := maps.Clone(m.shippedDefaults)
	if merged == nil {
		merged = map[string]string{}
	}
	maps.Copy(merged, m.values)
	var problems []stringformat.Problem
	for _, p := range stringformat.Validate(merged, config.StringFallbacks, config.StringMissingDefaults, defaults) {
		if _, set := m.values[p.Key]; set {
			problems = append(problems, p)
		}
	}
	return problems
}

// plural renders a "and N more" suffix for a warning naming one example.
func plural(n int) string {
	if n <= 1 {
		return ""
	}
	return fmt.Sprintf(" and %d more", n-1)
}

// recomputeDirty re-derives the dirty flag by comparing every current value
// against the snapshot loaded from disk, so undoing an edit clears it.
func (m *Model) recomputeDirty() {
	m.dirty = len(m.values) != len(m.origValues)
	for k, v := range m.values {
		if ov, ok := m.origValues[k]; !ok || v != ov {
			m.dirty = true
			return
		}
	}
	// Keeping a string with F5 refreshes its fingerprint, which is a change
	// to save.
	for k := range m.kept {
		base, recorded := m.bases[k]
		if _, set := m.values[k]; set && recorded && base != stringhistory.Hash(m.effective(k, "", false)) {
			m.dirty = true
			return
		}
	}
}

// editBase returns the value an edit of key starts from and is compared with:
// the file's value, or for an absent key the default the BBS uses for it. An
// absent key with a missing-key default can then be saved blank, which
// switches that string off.
func (m Model) editBase(key string) string {
	if v, ok := m.values[key]; ok {
		return v
	}
	return m.effective(key, "", false)
}

// getValue returns what the BBS prints for key: the file's value, or the
// default when the file leaves it out.
func (m Model) getValue(key string) string {
	if v, ok := m.values[key]; ok {
		return v
	}
	return m.effective(key, "", false)
}
