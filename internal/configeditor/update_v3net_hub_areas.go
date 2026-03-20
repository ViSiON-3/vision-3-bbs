package configeditor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// hubNetworkAreaIndices returns MsgAreas indices matching the given v3net network.
func (m Model) hubNetworkAreaIndices() []int {
	var indices []int
	for i, a := range m.configs.MsgAreas {
		if a.AreaType == "v3net" && a.Network == m.hubAreaNetwork {
			indices = append(indices, i)
		}
	}
	return indices
}

// dataPath returns the data directory path derived from the config path.
// Config is typically at "<root>/configs", data at "<root>/data".
func (m Model) dataPath() string {
	return filepath.Join(filepath.Dir(m.configPath), "data")
}

// resolveJAMBase returns the absolute path prefix for a JAM base.
func (m Model) resolveJAMBase(basePath string) string {
	if filepath.IsAbs(basePath) {
		return basePath
	}
	return filepath.Join(m.dataPath(), basePath)
}

// jamFilesExist checks whether any JAM files exist for the given base path.
func (m Model) jamFilesExist(basePath string) bool {
	abs := m.resolveJAMBase(basePath)
	for _, ext := range []string{".jhr", ".jdt", ".jdx", ".jlr"} {
		if _, err := os.Stat(abs + ext); err == nil {
			return true
		}
	}
	return false
}

// enterHubAreaManager opens the hub area management screen for the given network.
func (m Model) enterHubAreaManager(network string) (Model, tea.Cmd) {
	m.hubAreaNetwork = network
	m.hubAreaCursor = 0
	m.hubAreaScroll = 0
	m.mode = modeV3NetHubAreas
	return m, nil
}

// --- Hub Area List Mode ---

func (m Model) updateV3NetHubAreas(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	indices := m.hubNetworkAreaIndices()
	total := len(indices)

	switch msg.Type {
	case tea.KeyUp:
		if m.hubAreaCursor > 0 {
			m.hubAreaCursor--
		}
	case tea.KeyDown:
		if m.hubAreaCursor < total-1 {
			m.hubAreaCursor++
		}
	case tea.KeyHome:
		m.hubAreaCursor = 0
	case tea.KeyEnd:
		if total > 0 {
			m.hubAreaCursor = total - 1
		}
	case tea.KeyEscape:
		return m.promptNavSave(modeRecordEdit)
	default:
		key := strings.ToUpper(msg.String())
		switch key {
		case "S":
			if m.dirty {
				m.saveAll()
			}
			return m, nil
		case "D":
			if total > 0 {
				m.hubAreaTargetIdx = indices[m.hubAreaCursor]
				m.confirmYes = false
				m.mode = modeV3NetAreaDeleteConfirm
			}
			return m, nil
		case "I":
			m.hubAreaInsertStep = 0
			m.hubAreaInsertTag = ""
			m.hubAreaInsertName = ""
			m.hubAreaInsertDesc = ""
			m.hubAreaInsertBase = ""
			m.textInput.Reset()
			m.textInput.CharLimit = 40
			m.textInput.Width = 30
			m.textInput.Focus()
			m.mode = modeV3NetAreaInsert
			return m, textinput.Blink
		case "E":
			if total > 0 {
				idx := indices[m.hubAreaCursor]
				a := m.configs.MsgAreas[idx]
				m.hubAreaTargetIdx = idx
				m.hubAreaEditStep = 0
				m.hubAreaEditTag = a.EchoTag
				if m.hubAreaEditTag == "" {
					m.hubAreaEditTag = a.Tag
				}
				m.hubAreaEditName = a.Name
				m.hubAreaEditDesc = a.Description
				m.hubAreaEditBase = a.BasePath
				m.hubAreaOldTag = m.hubAreaEditTag
				m.hubAreaOldBase = a.BasePath
				m.textInput.SetValue(m.hubAreaEditTag)
				m.textInput.CharLimit = 40
				m.textInput.Width = 30
				m.textInput.CursorEnd()
				m.textInput.Focus()
				m.mode = modeV3NetAreaRename
			}
			return m, textinput.Blink
		case "Q":
			return m.promptNavSave(modeRecordEdit)
		}
	}

	// Clamp scroll.
	m.clampHubAreaScroll(total)
	return m, nil
}

func (m *Model) clampHubAreaScroll(total int) {
	visible := 10
	if m.hubAreaCursor < m.hubAreaScroll {
		m.hubAreaScroll = m.hubAreaCursor
	}
	if m.hubAreaCursor >= m.hubAreaScroll+visible {
		m.hubAreaScroll = m.hubAreaCursor - visible + 1
	}
	maxOff := total - visible
	if maxOff < 0 {
		maxOff = 0
	}
	if m.hubAreaScroll > maxOff {
		m.hubAreaScroll = maxOff
	}
	if m.hubAreaScroll < 0 {
		m.hubAreaScroll = 0
	}
}

// --- Area Insert ---

func (m Model) updateV3NetAreaInsert(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEscape:
		m.textInput.Blur()
		m.message = ""
		m.mode = modeV3NetHubAreas
		return m, nil

	case tea.KeyEnter, tea.KeyTab, tea.KeyDown:
		val := strings.TrimSpace(m.textInput.Value())
		switch m.hubAreaInsertStep {
		case 0:
			if err := protocol.ValidateAreaTag(val); err != nil {
				m.message = err.Error()
				return m, nil
			}
			for _, a := range m.configs.MsgAreas {
				if a.Tag == val || a.EchoTag == val {
					m.message = "Area tag already exists"
					return m, nil
				}
			}
			m.hubAreaInsertTag = val
			m.hubAreaInsertStep = 1
			m.textInput.SetValue(m.hubAreaInsertName)
			m.textInput.CursorEnd()
			m.textInput.Focus()
			m.message = ""
			return m, nil
		case 1:
			if val == "" {
				m.message = "Area name cannot be empty"
				return m, nil
			}
			m.hubAreaInsertName = val
			m.hubAreaInsertStep = 2
			m.textInput.SetValue(m.hubAreaInsertDesc)
			m.textInput.CursorEnd()
			m.textInput.Focus()
			m.message = ""
			return m, nil
		case 2:
			m.hubAreaInsertDesc = val
			m.hubAreaInsertStep = 3
			// Default local path to msgbases/<tag> if not yet set.
			if m.hubAreaInsertBase == "" {
				m.hubAreaInsertBase = "msgbases/" + m.hubAreaInsertTag
			}
			m.textInput.SetValue(m.hubAreaInsertBase)
			m.textInput.CursorEnd()
			m.textInput.Focus()
			m.message = ""
			return m, nil
		case 3:
			if val == "" {
				m.message = "Local path cannot be empty"
				return m, nil
			}
			m.hubAreaInsertBase = val
			m.textInput.Blur()
			return m.applyAreaInsert()
		}
		return m, nil

	case tea.KeyUp, tea.KeyShiftTab:
		switch m.hubAreaInsertStep {
		case 1:
			m.hubAreaInsertName = strings.TrimSpace(m.textInput.Value())
			m.hubAreaInsertStep = 0
			m.textInput.SetValue(m.hubAreaInsertTag)
			m.textInput.CursorEnd()
			m.textInput.Focus()
			return m, nil
		case 2:
			m.hubAreaInsertDesc = strings.TrimSpace(m.textInput.Value())
			m.hubAreaInsertStep = 1
			m.textInput.SetValue(m.hubAreaInsertName)
			m.textInput.CursorEnd()
			m.textInput.Focus()
			return m, nil
		case 3:
			m.hubAreaInsertBase = strings.TrimSpace(m.textInput.Value())
			m.hubAreaInsertStep = 2
			m.textInput.SetValue(m.hubAreaInsertDesc)
			m.textInput.CursorEnd()
			m.textInput.Focus()
			return m, nil
		}
		return m, nil

	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
}

func (m Model) applyAreaInsert() (Model, tea.Cmd) {
	area := wizardArea{
		Tag:         m.hubAreaInsertTag,
		Name:        m.hubAreaInsertName,
		Description: m.hubAreaInsertDesc,
		BasePath:    m.hubAreaInsertBase,
	}

	// Create the message area entry.
	m.createHubMessageAreas(m.hubAreaNetwork, []wizardArea{area})

	// Add the tag to the hub's InitialAreas for NAL seeding on next restart.
	m.configs.V3Net.Hub.InitialAreas = append(m.configs.V3Net.Hub.InitialAreas,
		config.V3NetHubArea{Tag: area.Tag, Name: area.Name, Description: area.Description})

	// Add the board to any self-leaf subscription for this network.
	m.addBoardToSelfLeaf(m.hubAreaNetwork, area.Tag)

	m.dirty = true
	m.message = fmt.Sprintf("Area %q created", area.Name)

	// Move cursor to the new area.
	indices := m.hubNetworkAreaIndices()
	if len(indices) > 0 {
		m.hubAreaCursor = len(indices) - 1
	}

	m.mode = modeV3NetHubAreas
	return m, nil
}

// isSelfLeafURL returns true if the URL points to localhost (IPv4 or IPv6).
func isSelfLeafURL(hubURL string) bool {
	for _, host := range []string{"localhost", "127.0.0.1", "[::1]", "::1"} {
		if strings.Contains(hubURL, host) {
			return true
		}
	}
	return false
}

// addBoardToSelfLeaf adds a board tag to any leaf subscription for the given
// network that points to localhost (the hub's self-leaf).
func (m *Model) addBoardToSelfLeaf(network, tag string) {
	for i := range m.configs.V3Net.Leaves {
		l := &m.configs.V3Net.Leaves[i]
		if l.Network != network {
			continue
		}
		if !isSelfLeafURL(l.HubURL) {
			continue
		}
		for _, b := range l.Boards {
			if b == tag {
				return
			}
		}
		l.Boards = append(l.Boards, tag)
		return
	}
}

// renameBoardInSelfLeaf replaces oldTag with newTag in the self-leaf Boards list.
func (m *Model) renameBoardInSelfLeaf(network, oldTag, newTag string) {
	for i := range m.configs.V3Net.Leaves {
		l := &m.configs.V3Net.Leaves[i]
		if l.Network != network {
			continue
		}
		if !isSelfLeafURL(l.HubURL) {
			continue
		}
		for j, b := range l.Boards {
			if b == oldTag {
				l.Boards[j] = newTag
				return
			}
		}
		return
	}
}

// --- Area Delete Confirmation ---

func (m Model) updateV3NetAreaDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyLeft, tea.KeyRight:
		m.confirmYes = !m.confirmYes
	case tea.KeyEnter:
		if m.confirmYes {
			return m.executeAreaDeleteStep()
		}
		return m.cancelAreaDelete()
	case tea.KeyEscape:
		return m.cancelAreaDelete()
	default:
		switch msg.String() {
		case "y", "Y":
			m.confirmYes = true
			return m.executeAreaDeleteStep()
		case "n", "N":
			return m.cancelAreaDelete()
		}
	}
	return m, nil
}

func (m Model) executeAreaDeleteStep() (Model, tea.Cmd) {
	switch m.mode {
	case modeV3NetAreaDeleteConfirm:
		idx := m.hubAreaTargetIdx
		basePath := m.configs.MsgAreas[idx].BasePath
		areaName := m.configs.MsgAreas[idx].Name

		m.configs.MsgAreas = append(m.configs.MsgAreas[:idx], m.configs.MsgAreas[idx+1:]...)
		m.dirty = true

		indices := m.hubNetworkAreaIndices()
		if m.hubAreaCursor >= len(indices) && m.hubAreaCursor > 0 {
			m.hubAreaCursor--
		}

		if m.jamFilesExist(basePath) {
			m.hubAreaTargetIdx = -1
			m.hubAreaOldBase = basePath
			m.confirmYes = false
			m.mode = modeV3NetAreaDeleteJAM
			return m, nil
		}

		m.message = fmt.Sprintf("Area %q removed from config", areaName)
		m.mode = modeV3NetHubAreas
		return m, nil

	case modeV3NetAreaDeleteJAM:
		abs := m.resolveJAMBase(m.hubAreaOldBase)
		var deleted int
		for _, ext := range []string{".jhr", ".jdt", ".jdx", ".jlr"} {
			if err := os.Remove(abs + ext); err == nil {
				deleted++
			}
		}
		m.message = fmt.Sprintf("Deleted %d JAM file(s)", deleted)
		m.mode = modeV3NetHubAreas
		return m, nil
	}
	return m, nil
}

func (m Model) cancelAreaDelete() (Model, tea.Cmd) {
	switch m.mode {
	case modeV3NetAreaDeleteJAM:
		m.message = "Area removed (JAM files kept)"
	}
	m.mode = modeV3NetHubAreas
	return m, nil
}

// --- Area Edit ---

func (m Model) updateV3NetAreaRename(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeV3NetAreaRenameJAM {
		return m.updateAreaRenameJAMConfirm(msg)
	}

	switch msg.Type {
	case tea.KeyEscape:
		m.textInput.Blur()
		m.mode = modeV3NetHubAreas
		return m, nil

	case tea.KeyEnter, tea.KeyTab, tea.KeyDown:
		val := strings.TrimSpace(m.textInput.Value())
		switch m.hubAreaEditStep {
		case 0: // Tag
			if err := protocol.ValidateAreaTag(val); err != nil {
				m.message = err.Error()
				return m, nil
			}
			// Check for duplicate tag (excluding current area).
			for i, a := range m.configs.MsgAreas {
				if i == m.hubAreaTargetIdx {
					continue
				}
				if a.Tag == val || a.EchoTag == val {
					m.message = "Area tag already exists"
					return m, nil
				}
			}
			m.hubAreaEditTag = val
			m.hubAreaEditStep = 1
			m.textInput.SetValue(m.hubAreaEditName)
			m.textInput.CursorEnd()
			m.textInput.Focus()
			m.message = ""
			return m, nil
		case 1: // Name
			if val == "" {
				m.message = "Area name cannot be empty"
				return m, nil
			}
			m.hubAreaEditName = val
			m.hubAreaEditStep = 2
			m.textInput.SetValue(m.hubAreaEditDesc)
			m.textInput.CursorEnd()
			m.textInput.Focus()
			m.message = ""
			return m, nil
		case 2: // Description
			m.hubAreaEditDesc = val
			m.hubAreaEditStep = 3
			m.textInput.SetValue(m.hubAreaEditBase)
			m.textInput.CursorEnd()
			m.textInput.Focus()
			m.message = ""
			return m, nil
		case 3: // Base Path
			if val == "" {
				m.message = "Local path cannot be empty"
				return m, nil
			}
			m.hubAreaEditBase = val
			m.textInput.Blur()
			return m.applyAreaEdit()
		}
		return m, nil

	case tea.KeyUp, tea.KeyShiftTab:
		switch m.hubAreaEditStep {
		case 1:
			m.hubAreaEditName = strings.TrimSpace(m.textInput.Value())
			m.hubAreaEditStep = 0
			m.textInput.SetValue(m.hubAreaEditTag)
			m.textInput.CursorEnd()
			m.textInput.Focus()
			return m, nil
		case 2:
			m.hubAreaEditDesc = strings.TrimSpace(m.textInput.Value())
			m.hubAreaEditStep = 1
			m.textInput.SetValue(m.hubAreaEditName)
			m.textInput.CursorEnd()
			m.textInput.Focus()
			return m, nil
		case 3:
			m.hubAreaEditBase = strings.TrimSpace(m.textInput.Value())
			m.hubAreaEditStep = 2
			m.textInput.SetValue(m.hubAreaEditDesc)
			m.textInput.CursorEnd()
			m.textInput.Focus()
			return m, nil
		}
		return m, nil

	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
}

func (m Model) applyAreaEdit() (Model, tea.Cmd) {
	idx := m.hubAreaTargetIdx
	if idx < 0 || idx >= len(m.configs.MsgAreas) {
		m.mode = modeV3NetHubAreas
		return m, nil
	}

	area := &m.configs.MsgAreas[idx]

	// Apply all field changes.
	area.Tag = m.hubAreaEditTag
	area.EchoTag = m.hubAreaEditTag
	area.Name = m.hubAreaEditName
	area.Description = m.hubAreaEditDesc
	area.BasePath = m.hubAreaEditBase
	m.dirty = true

	// If tag changed, update the self-leaf board list.
	if m.hubAreaEditTag != m.hubAreaOldTag {
		m.renameBoardInSelfLeaf(m.hubAreaNetwork, m.hubAreaOldTag, m.hubAreaEditTag)
	}

	// If base path changed, offer to rename JAM files on disk.
	if m.hubAreaEditBase != m.hubAreaOldBase && m.jamFilesExist(m.hubAreaOldBase) {
		m.confirmYes = true
		m.mode = modeV3NetAreaRenameJAM
		return m, nil
	}

	m.message = fmt.Sprintf("Area %q updated", area.Name)
	m.mode = modeV3NetHubAreas
	return m, nil
}

func (m Model) updateAreaRenameJAMConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyLeft, tea.KeyRight:
		m.confirmYes = !m.confirmYes
	case tea.KeyEnter:
		if m.confirmYes {
			return m.executeJAMRename()
		}
		m.message = "Area updated (JAM files not moved)"
		m.mode = modeV3NetHubAreas
		return m, nil
	case tea.KeyEscape:
		m.message = "Area updated (JAM files not moved)"
		m.mode = modeV3NetHubAreas
		return m, nil
	default:
		switch msg.String() {
		case "y", "Y":
			m.confirmYes = true
			return m.executeJAMRename()
		case "n", "N":
			m.message = "Area updated (JAM files not moved)"
			m.mode = modeV3NetHubAreas
			return m, nil
		}
	}
	return m, nil
}

func (m Model) executeJAMRename() (Model, tea.Cmd) {
	oldAbs := m.resolveJAMBase(m.hubAreaOldBase)
	newAbs := m.resolveJAMBase(m.hubAreaEditBase)

	if dir := filepath.Dir(newAbs); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			m.message = fmt.Sprintf("Cannot create directory: %v", err)
			m.mode = modeV3NetHubAreas
			return m, nil
		}
	}

	var renamed int
	for _, ext := range []string{".jhr", ".jdt", ".jdx", ".jlr"} {
		if err := os.Rename(oldAbs+ext, newAbs+ext); err == nil {
			renamed++
		}
	}
	m.message = fmt.Sprintf("Renamed %d JAM file(s)", renamed)
	m.mode = modeV3NetHubAreas
	return m, nil
}
