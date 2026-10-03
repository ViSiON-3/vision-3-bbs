package configeditor

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

// ftnEcholistMsg is the result of downloading and parsing an FTN echolist,
// or with fileEchoes set, a file echo list.
type ftnEcholistMsg struct {
	url        string // the URL this result was fetched from, for staleness checks
	generation uint64 // identifies the download attempt
	fileEchoes bool
	areas      []ftn.EchoArea
	err        error
}

// fetchFTNEcholist returns a tea.Cmd that downloads and parses a backbone.na
// echolist, applying network-specific cleanup rules from the registry entry.
func fetchFTNEcholist(url string, reg *ftn.RegistryNetwork, generation uint64) tea.Cmd {
	return func() tea.Msg {
		areas, err := ftn.DownloadEcholist(context.Background(), url)
		if err != nil {
			return ftnEcholistMsg{url: url, generation: generation, err: err}
		}

		// Apply cleanup rules if we have registry data.
		if reg != nil {
			areas = ftn.CleanEcholist(areas, reg.AreatagExclude, reg.AreatitlePrefix)
		}

		return ftnEcholistMsg{url: url, generation: generation, areas: areas}
	}
}

// ftnNodelistMsg is the result of downloading and parsing an FTN nodelist.
type ftnNodelistMsg struct {
	url        string // the URL this result was fetched from, for staleness checks
	generation uint64 // the lookupGeneration this fetch was dispatched under
	nodelist   *ftn.Nodelist
	err        error
}

// fetchFTNNodelist returns a tea.Cmd that downloads and parses a nodelist.
// The result is stamped with url and generation so a late/stale result can
// be identified — url against whatever network the wizard has since moved
// on to, and generation against a cancelled-then-retried fetch against that
// same URL — and ctx allows the caller to cancel the in-flight download
// (e.g. on ESC).
func fetchFTNNodelist(ctx context.Context, url string, generation uint64) tea.Cmd {
	return func() tea.Msg {
		nl, err := ftn.DownloadNodelist(ctx, url)
		return ftnNodelistMsg{url: url, generation: generation, nodelist: nl, err: err}
	}
}

// fetchFTNFileEchoList returns a tea.Cmd that downloads and parses a
// network's file echo list.
func fetchFTNFileEchoList(url string, generation uint64) tea.Cmd {
	return func() tea.Msg {
		echoes, err := ftn.DownloadFileEchoList(context.Background(), url)
		return ftnEcholistMsg{url: url, generation: generation, fileEchoes: true, areas: echoes, err: err}
	}
}
