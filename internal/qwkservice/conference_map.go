package qwkservice

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// ConferenceKind classifies how a QWK conference maps to a local area.
type ConferenceKind string

const (
	KindPublic      ConferenceKind = "public"
	KindPrivateMail ConferenceKind = "private_mail"
)

// PrivateMailTag is the message-area tag treated as the private-mail/email
// conference. PrivateMailConference is the QWK conference number reserved for it.
const (
	PrivateMailTag        = "PRIVMAIL"
	PrivateMailConference = 0
)

// ConferenceMapEntry is the stable mapping from a local area (by tag) to a QWK
// conference number.
type ConferenceMapEntry struct {
	QWKNumber int            `json:"qwk_number"`
	AreaTag   string         `json:"area_tag"`
	Kind      ConferenceKind `json:"kind"`
}

// ConferenceMap is the persisted tag->number contract used for QWK export and
// import. Numbers, once assigned, are never changed.
type ConferenceMap struct {
	entries []ConferenceMapEntry
	byTag   map[string]ConferenceMapEntry
	byNum   map[int]ConferenceMapEntry
}

func newConferenceMap() *ConferenceMap {
	return &ConferenceMap{
		byTag: map[string]ConferenceMapEntry{},
		byNum: map[int]ConferenceMapEntry{},
	}
}

func (m *ConferenceMap) reindex() {
	m.byTag = make(map[string]ConferenceMapEntry, len(m.entries))
	m.byNum = make(map[int]ConferenceMapEntry, len(m.entries))
	for _, e := range m.entries {
		m.byTag[e.AreaTag] = e
		m.byNum[e.QWKNumber] = e
	}
}

// LoadConferenceMap reads the map from path. A missing file yields an empty map
// (not an error); a malformed file is an error.
func LoadConferenceMap(path string) (*ConferenceMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return newConferenceMap(), nil
		}
		return nil, fmt.Errorf("read conference map: %w", err)
	}
	var entries []ConferenceMapEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse conference map: %w", err)
	}
	m := newConferenceMap()
	m.entries = entries
	m.reindex()
	return m, nil
}

// Save writes the map to path atomically (temp file + rename).
func (m *ConferenceMap) Save(path string) error {
	data, err := json.MarshalIndent(m.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal conference map: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "qwk_conferences_*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp conference map: %w", err)
	}
	tmpName := tmp.Name()
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp conference map: %w", err)
	}
	if err = tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp conference map: %w", err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename conference map: %w", err)
	}
	return nil
}

// Sync ensures every area has an entry, never renumbering an existing one.
// Returns true if the map changed and should be persisted.
func (m *ConferenceMap) Sync(areas []*message.MessageArea) bool {
	changed := false
	for _, a := range areas {
		if _, ok := m.byTag[a.Tag]; ok {
			continue
		}
		var e ConferenceMapEntry
		if a.Tag == PrivateMailTag {
			e = ConferenceMapEntry{QWKNumber: PrivateMailConference, AreaTag: a.Tag, Kind: KindPrivateMail}
		} else {
			e = ConferenceMapEntry{QWKNumber: m.nextNumber(a.ID), AreaTag: a.Tag, Kind: KindPublic}
		}
		m.entries = append(m.entries, e)
		m.byTag[e.AreaTag] = e
		m.byNum[e.QWKNumber] = e
		changed = true
	}
	if changed {
		sort.Slice(m.entries, func(i, j int) bool { return m.entries[i].QWKNumber < m.entries[j].QWKNumber })
	}
	return changed
}

// nextNumber prefers the area's local ID, falling back to the next free
// positive integer when that ID is <= 0 or already taken.
func (m *ConferenceMap) nextNumber(preferred int) int {
	if preferred > 0 {
		if _, taken := m.byNum[preferred]; !taken {
			return preferred
		}
	}
	for n := 1; ; n++ {
		if _, taken := m.byNum[n]; !taken {
			return n
		}
	}
}

// EntryForTag returns the entry for an area tag.
func (m *ConferenceMap) EntryForTag(tag string) (ConferenceMapEntry, bool) {
	e, ok := m.byTag[tag]
	return e, ok
}

// EntryForNumber returns the entry for a QWK conference number.
func (m *ConferenceMap) EntryForNumber(num int) (ConferenceMapEntry, bool) {
	e, ok := m.byNum[num]
	return e, ok
}
