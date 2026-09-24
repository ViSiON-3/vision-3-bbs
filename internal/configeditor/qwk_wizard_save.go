package configeditor

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
)

// confirmQWKWizard writes the network, its conference group, one message
// area per chosen conference and the poll event, then saves everything.
func (m Model) confirmQWKWizard() (Model, tea.Cmd) {
	w := m.qwkWizard
	key := w.networkKey
	editing := w.editing()
	if editing {
		key = w.editingKey
	}
	if m.configs.QWKNet.Networks == nil {
		m.configs.QWKNet.Networks = make(map[string]config.QWKNetworkConfig)
	}
	if !editing {
		if _, exists := m.configs.QWKNet.Networks[key]; exists {
			m.message = fmt.Sprintf("QWK network %q already exists — edit it under QWK Networks instead", key)
			return m, nil
		}
	}
	m.configs.QWKNet.ApplyDefaults()

	nc := m.configs.QWKNet.Networks[key] // zero value when adding
	nc.Enabled = true
	nc.Name = w.networkName
	nc.HubID = w.hubID
	nc.Host = w.host
	nc.Port = w.port
	nc.Username = w.loginName
	nc.Password = w.password
	nc.Tagline = w.tagline
	m.configs.QWKNet.Networks[key] = nc

	// Conference group named after the network, shared by its areas.
	confID := m.findOrCreateNetworkConference(key)
	for i, c := range m.configs.Conferences {
		if c.ID == confID && c.Description == key+" message network" {
			m.configs.Conferences[i].Name = w.networkName
			m.configs.Conferences[i].Description = w.networkName + " (QWK network via " + w.hubID + ")"
		}
	}

	created := 0
	for i, sel := range w.selected {
		if !sel || i >= len(w.available) {
			continue
		}
		if m.createQWKMsgAreaIfNeeded(key, w.available[i], confID, w.autoJoin) {
			created++
		}
	}

	wireQWKEvents(&m.configs.Events, key, w.hubID, w.schedule)

	m.dirty = true
	if !m.saveAll() {
		return m, nil
	}

	switch {
	case editing:
		m.message = fmt.Sprintf("QWK network %q updated — %d conference area(s) added. The next scheduled poll picks it up.", key, created)
	case created == 0:
		m.message = fmt.Sprintf("QWK network %q saved with no conferences — add areas under Message Areas or re-run the wizard.", key)
	default:
		m.message = fmt.Sprintf("QWK network %q saved — %d conference area(s) created; polling %s on schedule %s.", key, created, w.hubID, w.schedule)
	}
	m.qwkWizard = nil
	m.mode = modeCategoryMenu
	return m, nil
}

// createQWKMsgAreaIfNeeded adds the area for a hub conference unless one on
// this network already mirrors that number. It reports whether it added one.
func (m *Model) createQWKMsgAreaIfNeeded(netKey string, conf qwk.ConferenceInfo, confID int, autoJoin bool) bool {
	for _, a := range m.configs.MsgAreas {
		if a.IsQWKNet() && strings.EqualFold(a.Network, netKey) && a.QWKConference == conf.Number {
			return false
		}
	}
	slug := qwkAreaSlug(conf.Name, conf.Number)
	tag := strings.ToUpper(netKey) + "_" + strings.ToUpper(slug)
	tag = uniqueAreaTag(m.configs.MsgAreas, tag)

	newID, maxPos := 1, 0
	for _, a := range m.configs.MsgAreas {
		if a.ID >= newID {
			newID = a.ID + 1
		}
		if a.Position > maxPos {
			maxPos = a.Position
		}
	}
	name := conf.Name
	if name == "" {
		name = fmt.Sprintf("Conference %d", conf.Number)
	}
	m.configs.MsgAreas = append(m.configs.MsgAreas, message.MessageArea{
		ID:            newID,
		Position:      maxPos + 1,
		Tag:           tag,
		Name:          name,
		Description:   fmt.Sprintf("%s conference %d", strings.ToUpper(netKey), conf.Number),
		AreaType:      message.AreaTypeQWKNet,
		Network:       netKey,
		EchoTag:       conf.Name,
		QWKConference: conf.Number,
		AutoJoin:      autoJoin,
		ACSRead:       "s10",
		ACSWrite:      "s20",
		// The number keeps the path unique: two conferences can slug alike
		// ("Programming (Baja)" / "(Basic)" both cut to the same 24 chars).
		BasePath:     filepath.Join("msgbases", fmt.Sprintf("qwk.%s_%d_%s", strings.ToLower(netKey), conf.Number, strings.ToLower(slug))),
		ConferenceID: confID,
	})
	return true
}

// qwkAreaSlug reduces a conference name to a tag-safe word; a name with
// nothing usable falls back to the number.
func qwkAreaSlug(name string, number int) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if b.Len() > 0 && !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	slug := strings.Trim(b.String(), "_")
	if len(slug) > 24 {
		slug = strings.Trim(slug[:24], "_")
	}
	if slug == "" {
		return fmt.Sprintf("c%d", number)
	}
	return slug
}

// uniqueAreaTag appends a counter until the tag is unused.
func uniqueAreaTag(areas []message.MessageArea, tag string) string {
	taken := func(t string) bool {
		for _, a := range areas {
			if strings.EqualFold(a.Tag, t) {
				return true
			}
		}
		return false
	}
	if !taken(tag) {
		return tag
	}
	for i := 2; ; i++ {
		if c := fmt.Sprintf("%s%d", tag, i); !taken(c) {
			return c
		}
	}
}
