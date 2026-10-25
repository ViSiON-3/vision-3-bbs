package configeditor

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// areaSyncPrompt is a pending offer to copy a network's changed Auto-Create
// setting onto the echomail areas it already has. Auto-Create settings only
// shape areas the tosser creates later, so without the offer a sysop who
// turns on Real Names or lowers Max Messages sees nothing change (#690).
type areaSyncPrompt struct {
	network string
	setting string // field label, for the result message
	count   int    // existing areas whose value differs
	differs func(a *message.MessageArea) bool
	set     func(a *message.MessageArea)
	note    string // shown after the prompt either way (e.g. purge not scheduled)
}

// offerAreaSync queues a prompt to apply a changed Auto-Create setting to
// network's existing echomail areas. It is shown once the field edit
// finishes; nothing is queued when no area differs.
func (m *Model) offerAreaSync(network, setting string, differs func(*message.MessageArea) bool, set func(*message.MessageArea), note string) {
	p := &areaSyncPrompt{network: network, setting: setting, differs: differs, set: set, note: note}
	for i := range m.configs.MsgAreas {
		if p.matches(&m.configs.MsgAreas[i]) {
			p.count++
		}
	}
	if p.count == 0 {
		if note != "" {
			m.message = note
		}
		return
	}
	m.areaSync = p
}

// matches reports whether a is one of the network's echomail areas whose
// value differs from the new setting.
func (p *areaSyncPrompt) matches(a *message.MessageArea) bool {
	return strings.EqualFold(a.Network, p.network) &&
		strings.EqualFold(a.AreaType, "echomail") &&
		p.differs(a)
}

// question is the prompt's text, fitted to the confirm dialog.
func (p *areaSyncPrompt) question() string {
	q := fmt.Sprintf("Apply to %d existing %s area(s) too? ", p.count, p.network)
	if len(q) > 58 {
		q = fmt.Sprintf("Apply to %d existing area(s) too? ", p.count)
	}
	return q
}

// showPendingAreaSync opens the area sync prompt queued by a field's
// AfterSet, once the edit has returned to field navigation.
func (m Model) showPendingAreaSync() Model {
	if m.areaSync != nil && m.mode == modeRecordEdit {
		m.mode = modeAreaSyncConfirm
		m.confirmYes = true
	}
	return m
}

// updateAreaSyncConfirm handles keys in the area sync prompt. ESC answers
// No: the Auto-Create setting itself is already changed.
func (m Model) updateAreaSyncConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyLeft, tea.KeyRight:
		m.confirmYes = !m.confirmYes
		return m, nil
	case tea.KeyEnter:
		return m.finishAreaSync(m.confirmYes), nil
	case tea.KeyEscape:
		return m.finishAreaSync(false), nil
	}
	switch msg.String() {
	case "y", "Y":
		return m.finishAreaSync(true), nil
	case "n", "N":
		return m.finishAreaSync(false), nil
	}
	return m, nil
}

// finishAreaSync applies the pending setting to the network's areas when
// apply is set, and returns to field navigation.
func (m Model) finishAreaSync(apply bool) Model {
	p := m.areaSync
	m.areaSync = nil
	m.mode = modeRecordEdit
	if p == nil {
		return m
	}
	m.message = p.note
	if !apply {
		return m
	}
	n := 0
	for i := range m.configs.MsgAreas {
		a := &m.configs.MsgAreas[i]
		if p.matches(a) {
			p.set(a)
			n++
		}
	}
	m.dirty = true
	m.message = fmt.Sprintf("%s applied to %d existing %s area(s)", p.setting, n, p.network)
	if p.note != "" {
		m.message += " — " + p.note
	}
	return m
}

// purgeUnscheduledNote returns a warning when limit is a purge limit (max
// messages or days) and no enabled event runs "v3mail purge", the only
// thing that enforces it (#691); otherwise "".
func (m *Model) purgeUnscheduledNote(limit int) string {
	if limit <= 0 || m.configs.Events.RunsMessagePurge() {
		return ""
	}
	return "No enabled event runs 'v3mail purge', so this limit is not enforced"
}
