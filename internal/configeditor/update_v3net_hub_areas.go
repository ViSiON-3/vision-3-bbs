package configeditor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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
		case "R":
			if total > 0 {
				idx := indices[m.hubAreaCursor]
				m.hubAreaTargetIdx = idx
				m.hubAreaRenameStep = 0
				m.hubAreaNewName = m.configs.MsgAreas[idx].Name
				m.hubAreaNewBase = m.configs.MsgAreas[idx].BasePath
				m.textInput.SetValue(m.configs.MsgAreas[idx].Name)
				m.textInput.CharLimit = 60
				m.textInput.Width = 40
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
		// User confirmed removal from message area config.
		idx := m.hubAreaTargetIdx
		basePath := m.configs.MsgAreas[idx].BasePath
		areaName := m.configs.MsgAreas[idx].Name

		// Remove from MsgAreas.
		m.configs.MsgAreas = append(m.configs.MsgAreas[:idx], m.configs.MsgAreas[idx+1:]...)
		m.dirty = true

		// Fix cursor if it went past the end.
		indices := m.hubNetworkAreaIndices()
		if m.hubAreaCursor >= len(indices) && m.hubAreaCursor > 0 {
			m.hubAreaCursor--
		}

		// Offer to delete JAM files if they exist.
		if m.jamFilesExist(basePath) {
			m.hubAreaTargetIdx = -1 // no longer valid index
			m.hubAreaNewBase = basePath
			m.confirmYes = false
			m.mode = modeV3NetAreaDeleteJAM
			return m, nil
		}

		m.message = fmt.Sprintf("Area %q removed from config", areaName)
		m.mode = modeV3NetHubAreas
		return m, nil

	case modeV3NetAreaDeleteJAM:
		// User confirmed JAM file deletion.
		abs := m.resolveJAMBase(m.hubAreaNewBase)
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
		// User declined JAM deletion — area already removed from config.
		m.message = "Area removed (JAM files kept)"
	}
	m.mode = modeV3NetHubAreas
	return m, nil
}

// --- Area Rename ---

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
		if m.hubAreaRenameStep == 0 {
			// Name field — validate and advance to base path.
			if val == "" {
				m.message = "Area name cannot be empty"
				return m, nil
			}
			m.hubAreaNewName = val
			m.hubAreaRenameStep = 1
			m.textInput.SetValue(m.hubAreaNewBase)
			m.textInput.CursorEnd()
			m.textInput.Focus()
			m.message = ""
			return m, nil
		}
		// Base path field — apply rename.
		if val == "" {
			m.message = "Base path cannot be empty"
			return m, nil
		}
		m.hubAreaNewBase = val
		m.textInput.Blur()
		return m.applyAreaRename()

	case tea.KeyUp, tea.KeyShiftTab:
		if m.hubAreaRenameStep == 1 {
			m.hubAreaNewBase = strings.TrimSpace(m.textInput.Value())
			m.hubAreaRenameStep = 0
			m.textInput.SetValue(m.hubAreaNewName)
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

func (m Model) applyAreaRename() (Model, tea.Cmd) {
	idx := m.hubAreaTargetIdx
	if idx < 0 || idx >= len(m.configs.MsgAreas) {
		m.mode = modeV3NetHubAreas
		return m, nil
	}

	area := &m.configs.MsgAreas[idx]
	oldBase := area.BasePath

	// Apply name change.
	area.Name = m.hubAreaNewName
	m.dirty = true

	// If base path changed, offer to rename JAM files on disk.
	if m.hubAreaNewBase != oldBase {
		area.BasePath = m.hubAreaNewBase
		if m.jamFilesExist(oldBase) {
			// hubAreaNewBase already holds the new path.
			m.hubAreaNewName = oldBase // reuse field to store old path
			m.confirmYes = true
			m.mode = modeV3NetAreaRenameJAM
			return m, nil
		}
	}

	m.message = fmt.Sprintf("Area %q renamed", area.Name)
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
		m.message = "Area renamed (JAM files not moved)"
		m.mode = modeV3NetHubAreas
		return m, nil
	case tea.KeyEscape:
		m.message = "Area renamed (JAM files not moved)"
		m.mode = modeV3NetHubAreas
		return m, nil
	default:
		switch msg.String() {
		case "y", "Y":
			m.confirmYes = true
			return m.executeJAMRename()
		case "n", "N":
			m.message = "Area renamed (JAM files not moved)"
			m.mode = modeV3NetHubAreas
			return m, nil
		}
	}
	return m, nil
}

func (m Model) executeJAMRename() (Model, tea.Cmd) {
	oldAbs := m.resolveJAMBase(m.hubAreaNewName) // old path stored in NewName
	newAbs := m.resolveJAMBase(m.hubAreaNewBase)

	// Ensure target directory exists.
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
