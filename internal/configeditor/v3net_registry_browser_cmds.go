package configeditor

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/registry"
)

// fetchRegistryMsg is the result of fetching the V3Net network registry.
type fetchRegistryMsg struct {
	requestID uint64
	entries   []protocol.RegistryEntry
	err       error
}

// fetchRegistry returns a tea.Cmd that fetches the V3Net network registry.
func fetchRegistry(ctx context.Context, requestID uint64, url string) tea.Cmd {
	return func() tea.Msg {
		entries, err := registry.Fetch(ctx, url)
		return fetchRegistryMsg{requestID: requestID, entries: entries, err: err}
	}
}
