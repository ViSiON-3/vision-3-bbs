package configeditor

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwknet"
)

// qwkWizardState holds the transient state of the QWK network wizard.
type qwkWizardState struct {
	known *qwknet.KnownNetwork // registry entry the form was filled from; nil = custom

	networkName string // display name
	networkKey  string // qwknet.json key and message-area Network value
	hubID       string
	host        string
	port        int
	loginName   string
	password    string
	tagline     string
	schedule    string // cron for the poll event
	autoJoin    bool   // newscan default for created areas

	// Editing an already-configured network. The key is fixed: renaming
	// would have to carry every area and event along, which the QWK
	// Networks editor does field by field instead.
	editingKey string

	// Conference selection.
	available     []qwk.ConferenceInfo
	selected      []bool // parallel to available
	confsFetched  bool
	confsErr      string
	existingConfs map[int]bool // already mirrored by an area (editing)
	fetchGen      uint64       // guards against a late result after ESC/retry
	fetching      bool
}

// editing reports whether this run modifies an existing network.
func (s *qwkWizardState) editing() bool { return s != nil && s.editingKey != "" }

// selectedCount is how many conferences are ticked.
func (s *qwkWizardState) selectedCount() int {
	n := 0
	for _, sel := range s.selected {
		if sel {
			n++
		}
	}
	return n
}

// newSelectedCount is how many ticked conferences are not yet mirrored by
// an area, which is what the save will create.
func (s *qwkWizardState) newSelectedCount() int {
	n := 0
	for i, sel := range s.selected {
		if sel && i < len(s.available) && !s.existingConfs[s.available[i].Number] {
			n++
		}
	}
	return n
}

// hasData reports whether anything has been typed, to decide whether
// leaving needs a confirmation.
func (s *qwkWizardState) hasData() bool {
	if s == nil {
		return false
	}
	return s.networkKey != "" || s.hubID != "" || s.host != "" || s.password != "" || s.selectedCount() > 0
}
