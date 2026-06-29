// Package wfcui is the transport-agnostic Bubble Tea TUI for the WFC console.
// It depends only on internal/admin (interface + types).
package wfcui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
)

type viewMode int

const (
	modeList viewMode = iota
	modeDetails
	modeDisconnected
	modeTooSmall
)

const (
	minWidth  = 80
	minHeight = 25
)

// Options configures rendering and behavior.
type Options struct {
	ASCII     bool
	NoColor   bool
	ReadOnly  bool
	MaxEvents int
}

// Model is the WFC TUI model.
type Model struct {
	client   admin.AdminClient
	opts     Options
	snapshot *admin.SystemSnapshot
	events   []admin.Event
	eventCh  <-chan admin.Event
	selected int
	mode     viewMode
	width    int
	height   int
	lastErr  error
}

// New builds a Model. client may be nil in tests that drive Update directly.
func New(client admin.AdminClient, opts Options) Model {
	if opts.MaxEvents <= 0 {
		opts.MaxEvents = 200
	}
	return Model{client: client, opts: opts, mode: modeList}
}

// Init kicks off the first snapshot fetch and event subscription.
// If client is nil (test mode), Init returns nil.
func (m Model) Init() tea.Cmd {
	if m.client == nil {
		return nil
	}
	return tea.Batch(m.fetchSnapshot(), m.subscribeCmd())
}

func (m Model) fetchSnapshot() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		snap, err := client.Snapshot(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return snapshotMsg{snap}
	}
}

// subscribeCmd calls Subscribe exactly once and returns a subscribedMsg.
func (m Model) subscribeCmd() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ch, err := client.Subscribe(context.Background())
		return subscribedMsg{ch: ch, err: err}
	}
}

// waitForEvent reads ONE value from the given channel (subscribe-once pattern).
func waitForEvent(ch <-chan admin.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return errMsg{context.Canceled}
		}
		return eventMsg{ev}
	}
}

// pollCmd re-fetches the snapshot after a delay so the screen stays live.
func pollCmd(client admin.AdminClient, d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		snap, err := client.Snapshot(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return snapshotMsg{snap}
	})
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.width < minWidth || m.height < minHeight {
			m.mode = modeTooSmall
		} else if m.mode == modeTooSmall {
			m.mode = modeList
		}
		return m, nil

	case snapshotMsg:
		m.snapshot = msg.snap
		if m.snapshot != nil && m.selected >= len(m.snapshot.Nodes) {
			m.selected = max(0, len(m.snapshot.Nodes)-1)
		}
		if m.mode == modeDisconnected {
			m.mode = modeList
		}
		var cmd tea.Cmd
		if m.client != nil {
			cmd = pollCmd(m.client, time.Second)
		}
		return m, cmd

	case subscribedMsg:
		if msg.err != nil {
			m.mode = modeDisconnected
			m.lastErr = msg.err
			return m, nil
		}
		m.eventCh = msg.ch
		return m, waitForEvent(m.eventCh)

	case eventMsg:
		m.events = append(m.events, msg.ev)
		if len(m.events) > m.opts.MaxEvents {
			m.events = m.events[len(m.events)-m.opts.MaxEvents:]
		}
		if m.eventCh != nil {
			return m, waitForEvent(m.eventCh)
		}
		return m, nil

	case errMsg:
		m.mode = modeDisconnected
		m.lastErr = msg.err
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey is a minimal stub so the package compiles for Task 11.
// Task 13 (keys.go) will delete this stub and provide the full implementation.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyDown:
		if m.snapshot != nil && m.selected < len(m.snapshot.Nodes)-1 {
			m.selected++
		}
	case tea.KeyUp:
		if m.selected > 0 {
			m.selected--
		}
	case tea.KeyEnter:
		if m.mode == modeList {
			m.mode = modeDetails
		}
	case tea.KeyEsc:
		if m.mode == modeDetails {
			m.mode = modeList
		}
	}
	return m, nil
}
