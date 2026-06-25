package configeditor

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

// ftnEcholistMsg is the result of downloading and parsing an FTN echolist.
type ftnEcholistMsg struct {
	areas []ftn.EchoArea
	err   error
}

// fetchFTNEcholist returns a tea.Cmd that downloads and parses a backbone.na
// echolist, applying network-specific cleanup rules from the registry entry.
func fetchFTNEcholist(url string, reg *ftn.RegistryNetwork) tea.Cmd {
	return func() tea.Msg {
		areas, err := ftn.DownloadEcholist(context.Background(), url)
		if err != nil {
			return ftnEcholistMsg{err: err}
		}

		// Apply cleanup rules if we have registry data.
		if reg != nil {
			areas = ftn.CleanEcholist(areas, reg.AreatagExclude, reg.AreatitlePrefix)
		}

		return ftnEcholistMsg{areas: areas}
	}
}
