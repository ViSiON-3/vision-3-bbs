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
	client     admin.AdminClient
	opts       Options
	snapshot   *admin.SystemSnapshot
	events     []admin.Event
	eventCh    <-chan admin.Event
	selected   int
	mode       viewMode
	width      int
	height     int
	lastErr    error
	showLogs   bool
	subscribed bool // true when an active Subscribe channel is live
}

// New builds a Model. client may be nil in tests that drive Update directly.
func New(client admin.AdminClient, opts Options) Model {
	if opts.MaxEvents <= 0 {
		opts.MaxEvents = 200
	}
	return Model{client: client, opts: opts, mode: modeList, showLogs: true}
}

// Init kicks off the first snapshot fetch, event subscription, and the
// single sustaining poll chain. If client is nil (test mode), Init returns nil.
//
// fetchSnapshot provides an immediate initial paint (fromPoll=false, no re-arm).
// pollCmd starts the one and only self-sustaining tick loop (fromPoll=true, re-arms).
// R-key and reconnect calls use fetchSnapshot — one-shot, no additional chains.
func (m Model) Init() tea.Cmd {
	if m.client == nil {
		return nil
	}
	return tea.Batch(m.fetchSnapshot(), m.subscribeCmd(), pollCmd(m.client, time.Second))
}

// fetchSnapshot fetches a snapshot once (fromPoll=false). Used by Init's
// initial paint and the R-key manual refresh. Does NOT re-arm pollCmd.
func (m Model) fetchSnapshot() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		snap, err := client.Snapshot(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return snapshotMsg{snap: snap, fromPoll: false}
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
// It sets fromPoll=true so the Update handler re-arms the next tick,
// sustaining exactly one poll chain started by Init.
func pollCmd(client admin.AdminClient, d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		snap, err := client.Snapshot(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return snapshotMsg{snap: snap, fromPoll: true}
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
		// Only clear disconnected when we also have a live subscription channel.
		// A poll snapshot alone must NOT fake "connected" — the event feed may be dead.
		if m.mode == modeDisconnected && m.subscribed {
			m.mode = modeList
		}
		// Only re-arm pollCmd when this message came from the poll chain itself.
		// One-shot fetches (Init's initial paint, R-key manual refresh) must NOT
		// spawn a new chain — doing so would multiply poll goroutines unboundedly.
		var cmd tea.Cmd
		if msg.fromPoll && m.client != nil {
			cmd = pollCmd(m.client, time.Second)
		}
		return m, cmd

	case subscribedMsg:
		if msg.err != nil {
			m.subscribed = false
			m.mode = modeDisconnected
			m.lastErr = msg.err
			return m, nil
		}
		m.eventCh = msg.ch
		m.subscribed = true
		// Successfully subscribed — if we were disconnected, return to list now.
		if m.mode == modeDisconnected {
			m.mode = modeList
		}
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
		m.subscribed = false
		m.mode = modeDisconnected
		m.lastErr = msg.err
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}
