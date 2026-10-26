package qwk

import (
	"encoding/json"
)

// AreaIndexFile is the name of the ViSiON/3-specific sidecar entry in a QWK
// packet that describes how the exported conferences map onto the board's
// real message areas and their parent conferences. Standard QWK readers
// ignore unknown entries, so its presence is invisible to them; the ViSiON/3
// mobile client uses it to group areas the way the board does instead of
// guessing from CONTROL.DAT names.
const AreaIndexFile = "V3AREAS.JSON"

// AreaIndexVersion is the current schema version written to AreaIndexFile.
const AreaIndexVersion = 1

// AreaIndex is the sidecar payload. Conferences are listed in the board's
// display order and contain only those referenced by an exported area; areas
// are listed in CONTROL.DAT order.
type AreaIndex struct {
	Version     int                   `json:"version"`
	Conferences []AreaIndexConference `json:"conferences"`
	Areas       []AreaIndexArea       `json:"areas"`
}

// AreaIndexConference is a board conference (configs/conferences.json entry).
type AreaIndexConference struct {
	Tag  string `json:"tag"`
	Name string `json:"name"`
}

// AreaIndexArea maps one QWK conference number to a board message area.
type AreaIndexArea struct {
	// QWK is the conference number used in CONTROL.DAT, NDX records and
	// message headers for this area.
	QWK int `json:"qwk"`
	// Tag and Name are the area's own tag and display name.
	Tag  string `json:"tag"`
	Name string `json:"name"`
	// Conference is the tag of the parent conference, or empty when the area
	// is ungrouped.
	Conference string `json:"conference,omitempty"`
	// Type is the area type ("local", "echomail", "netmail", "v3net",
	// "qwknet"); empty when unknown.
	Type string `json:"type,omitempty"`
	// Network is the network key for networked areas (e.g. "fsxnet").
	Network string `json:"network,omitempty"`
}

// encodeAreaIndex serialises the sidecar; version is forced to the current
// schema so callers cannot emit a mismatched header.
func encodeAreaIndex(idx *AreaIndex) ([]byte, error) {
	out := *idx
	out.Version = AreaIndexVersion
	if out.Conferences == nil {
		out.Conferences = []AreaIndexConference{}
	}
	if out.Areas == nil {
		out.Areas = []AreaIndexArea{}
	}
	return json.MarshalIndent(&out, "", "  ")
}
