package configeditor

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/registry"
)

// fetchRegistryMsg is the result of fetching the V3Net network registry.
type fetchRegistryMsg struct {
	entries []protocol.RegistryEntry
	err     error
}

// fetchRegistry returns a tea.Cmd that fetches the V3Net network registry.
func fetchRegistry(url string) tea.Cmd {
	return func() tea.Msg {
		entries, err := registry.Fetch(context.Background(), url)
		return fetchRegistryMsg{entries: entries, err: err}
	}
}
