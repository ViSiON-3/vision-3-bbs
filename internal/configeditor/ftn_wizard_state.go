package configeditor

import (
	"context"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

// ftnWizardState holds all transient state for the FTN setup wizard.
type ftnWizardState struct {
	// Network identity (from registry or manual entry).
	zone             int
	networkName      string
	networkDesc      string
	coordinator      string
	coordinatorEmail string
	infoURL          string

	// Your node.
	ownAddress string // "21:4/158"

	// Hub configuration.
	hubAddress      string // "21:1/100"
	hubHostname     string // "agency.bbs.nz"
	hubPort         int    // 24556
	areafixPassword string
	sessionPassword string
	packetPassword  string

	// Echomail.
	originLine  string
	echolistURL string // from registry, may be overridden

	// Newscan default for created areas (wizard Y/n, default yes).
	autoJoinAreas bool

	// Area selection (populated after echolist download).
	availableAreas []ftn.EchoArea // parsed from downloaded echolist
	selectedAreas  []bool         // parallel array, true = subscribed
	areasFetched   bool
	areasFetchErr  string

	// Editing an already-configured network. Empty means this run adds a new
	// one. When set, it is the ftn.json network key being edited, and the
	// network name is fixed: renaming would have to migrate the conference,
	// every area tag and every msgbase path on disk, so the wizard does not
	// offer it.
	editingKey string

	// Echo tags (upper-cased) this network is already subscribed to, read
	// from the configured message areas. Used to tick the area browser to
	// match reality, and to report the count before any echolist download.
	subscribedTags map[string]bool

	// Registry data (for pre-fill).
	registryEntry *ftn.RegistryNetwork // nil if manual/custom

	// Nodelist lookup.
	nodelistURL   string             // from registry entry; empty = no lookup offered
	nodelist      *ftn.Nodelist      // cached parse, nil until fetched
	lookupLoading bool               // true while the nodelist download runs
	lookupResult  *ftn.NodeLookup    // last successful lookup, nil if none
	lookupErr     string             // last lookup failure, "" if none
	lookupCancel  context.CancelFunc // cancels the in-flight fetch, nil if none running
	hubAutofilled bool               // true if hub fields were last set by a lookup, not manual edit

	// lookupGeneration increments on every startFTNNodeLookup call (and on
	// every network switch). It guards against a late result from a
	// cancelled-then-retried fetch against the same URL, which the url
	// staleness check alone cannot distinguish from the current fetch.
	lookupGeneration uint64
}

// selectedAreaCount returns how many areas are currently selected.
func (s *ftnWizardState) selectedAreaCount() int {
	n := 0
	for _, sel := range s.selectedAreas {
		if sel {
			n++
		}
	}
	return n
}

// editing reports whether this wizard run is modifying an existing network
// rather than adding a new one.
func (s *ftnWizardState) editing() bool {
	return s != nil && s.editingKey != ""
}

// unsubscribedTagCount returns how many already-configured echo areas are no
// longer ticked in the area browser. Those areas stay on disk; this only
// reports them so the save message can be honest about it.
func (s *ftnWizardState) unsubscribedTagCount() int {
	if s == nil || len(s.subscribedTags) == 0 {
		return 0
	}
	stillSelected := make(map[string]bool, len(s.availableAreas))
	for i, sel := range s.selectedAreas {
		if sel && i < len(s.availableAreas) {
			stillSelected[strings.ToUpper(s.availableAreas[i].Tag)] = true
		}
	}
	// Without a downloaded echolist nothing was reviewed, so nothing dropped.
	if !s.areasFetched {
		return 0
	}
	n := 0
	for tag := range s.subscribedTags {
		if !stillSelected[tag] {
			n++
		}
	}
	return n
}

// hasData returns true if any wizard field has been filled in.
func (s *ftnWizardState) hasData() bool {
	if s == nil {
		return false
	}
	return s.networkName != "" || s.ownAddress != "" ||
		s.hubAddress != "" || s.hubHostname != "" ||
		s.selectedAreaCount() > 0
}
