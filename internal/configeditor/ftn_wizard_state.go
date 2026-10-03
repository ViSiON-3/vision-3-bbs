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
	hubIPFamily     string // config.IPFamily*: the family binkd calls the hub over
	areafixPassword string
	sessionPassword string
	packetPassword  string

	// Echomail.
	originLine  string
	echolistURL string // from registry, may be overridden

	// Newscan default for created areas (wizard Y/n, default yes).
	autoJoinAreas bool

	// Create sysop-only bad and dupe areas on save and point ftn.json at
	// them, for whichever of the two is not already set to a real area.
	rejectAreas bool

	// Area selection (populated after echolist download).
	availableAreas []ftn.EchoArea // parsed from downloaded echolist
	selectedAreas  []bool         // parallel array, true = subscribed
	areasFetched   bool
	areasFetchErr  string

	// File echo selection (populated after the file echo list download).
	// Optional: only a network whose registry entry names a file echo list
	// offers it, and nothing is created unless an echo is ticked.
	fileEchoListURL     string         // from registry; empty = not offered
	availableFileEchoes []ftn.EchoArea // parsed from downloaded file echo list
	selectedFileEchoes  []bool         // parallel array, true = carried
	fileEchoesFetched   bool

	// File echo tags (upper-cased) this network already feeds to a file
	// area, read from the configured file areas. The file-echo counterpart
	// of subscribedTags.
	carriedFileEchoes map[string]bool

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
	return countSelected(s.selectedAreas)
}

// selectedFileEchoCount returns how many file echoes are currently selected.
func (s *ftnWizardState) selectedFileEchoCount() int {
	return countSelected(s.selectedFileEchoes)
}

func countSelected(selected []bool) int {
	n := 0
	for _, sel := range selected {
		if sel {
			n++
		}
	}
	return n
}

// ftnWizardList is one of the wizard's two downloadable lists — echo areas
// or file echoes — as pointers into the wizard state, so the area browser
// can serve either.
type ftnWizardList struct {
	available *[]ftn.EchoArea
	selected  *[]bool
	fetched   *bool
	existing  map[string]bool // tags already configured, upper-cased
}

// listURL returns the echolist URL, or the file echo list URL if fileEchoes.
func (s *ftnWizardState) listURL(fileEchoes bool) string {
	if fileEchoes {
		return s.fileEchoListURL
	}
	return s.echolistURL
}

// list returns the echo area list, or the file echo list if fileEchoes.
func (s *ftnWizardState) list(fileEchoes bool) ftnWizardList {
	if fileEchoes {
		return ftnWizardList{&s.availableFileEchoes, &s.selectedFileEchoes, &s.fileEchoesFetched, s.carriedFileEchoes}
	}
	return ftnWizardList{&s.availableAreas, &s.selectedAreas, &s.areasFetched, s.subscribedTags}
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
	if s == nil {
		return 0
	}
	return s.list(false).droppedCount()
}

// uncarriedFileEchoCount is unsubscribedTagCount for file echoes: linked file
// areas whose echo is no longer ticked, which stay in place.
func (s *ftnWizardState) uncarriedFileEchoCount() int {
	if s == nil {
		return 0
	}
	return s.list(true).droppedCount()
}

// droppedCount returns how many already-configured tags the list offers but
// are no longer ticked.
func (l ftnWizardList) droppedCount() int {
	if len(l.existing) == 0 {
		return 0
	}
	// Without a downloaded list nothing was reviewed, so nothing dropped.
	if !*l.fetched {
		return 0
	}

	available := make(map[string]bool, len(*l.available))
	stillSelected := make(map[string]bool, len(*l.available))
	for i, area := range *l.available {
		tag := strings.ToUpper(area.Tag)
		available[tag] = true
		if i < len(*l.selected) && (*l.selected)[i] {
			stillSelected[tag] = true
		}
	}

	n := 0
	for tag := range l.existing {
		// A tag the echolist no longer offers could not have been unticked,
		// so it was not a decision the operator made and must not be
		// reported as one. The area is still configured and still works.
		if !available[tag] {
			continue
		}
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
		s.selectedAreaCount() > 0 || s.selectedFileEchoCount() > 0
}
