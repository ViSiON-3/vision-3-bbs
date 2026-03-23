package configeditor

import "github.com/ViSiON-3/vision-3-bbs/internal/ftn"

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

	// Area selection (populated after echolist download).
	availableAreas []ftn.EchoArea // parsed from downloaded echolist
	selectedAreas  []bool         // parallel array, true = subscribed
	areasFetched   bool
	areasFetchErr  string

	// Registry data (for pre-fill).
	registryEntry *ftn.RegistryNetwork // nil if manual/custom
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

// hasData returns true if any wizard field has been filled in.
func (s *ftnWizardState) hasData() bool {
	if s == nil {
		return false
	}
	return s.networkName != "" || s.ownAddress != "" ||
		s.hubAddress != "" || s.hubHostname != "" ||
		s.selectedAreaCount() > 0
}
