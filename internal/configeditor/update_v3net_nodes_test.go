package configeditor

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

func newNodesModel() Model {
	return Model{
		width: 80, height: 25,
		mode:         modeV3NetNodes,
		nodesNetwork: "testnet",
		nodesList: []protocol.NodeInfo{
			{NodeID: "aaaa000000000001", BBSName: "Alpha BBS", Status: "pending"},
			{NodeID: "bbbb000000000002", BBSName: "Beta BBS", Status: "active"},
		},
		configs: &allConfigs{V3Net: config.V3NetConfig{
			KeystorePath: "data/v3net.key",
			Hub:          config.V3NetHubConfig{Port: 8765},
		}},
	}
}

func TestNodesScreen_FetchMsgPopulatesList(t *testing.T) {
	m := newNodesModel()
	m.nodesList = nil
	m.nodesLoading = true
	result, _ := m.handleFetchNodesMsg(fetchNodesMsg{network: "testnet", nodes: []protocol.NodeInfo{{NodeID: "cccc000000000003"}}})
	rm := result.(Model)
	if rm.nodesLoading || len(rm.nodesList) != 1 || rm.nodesError != "" {
		t.Errorf("model %+v", rm)
	}
}

func TestNodesScreen_FetchErrorUsesFriendlyText(t *testing.T) {
	m := newNodesModel()
	result, _ := m.handleFetchNodesMsg(fetchNodesMsg{network: "testnet", err: errors.New("boom")})
	rm := result.(Model)
	if rm.nodesError == "" {
		t.Error("error should be surfaced")
	}
}

func TestNodesScreen_FetchErrorWithExistingListSurfacesMessage(t *testing.T) {
	m := newNodesModel()
	original := m.nodesList
	result, _ := m.handleFetchNodesMsg(fetchNodesMsg{network: "testnet", err: errors.New("boom")})
	rm := result.(Model)
	if rm.message == "" {
		t.Error("message row should surface the fetch error when the list is non-empty")
	}
	if len(rm.nodesList) != len(original) {
		t.Errorf("nodesList should remain unchanged on fetch error, got %+v", rm.nodesList)
	}
}

func TestNodesScreen_FetchMsgFromOtherNetworkIgnored(t *testing.T) {
	m := newNodesModel()
	original := m.nodesList
	m.nodesLoading = true
	result, cmd := m.handleFetchNodesMsg(fetchNodesMsg{network: "othernet", nodes: []protocol.NodeInfo{{NodeID: "zzzz000000000009"}}})
	rm := result.(Model)
	if cmd != nil {
		t.Error("stale fetch message should not produce a command")
	}
	if !rm.nodesLoading {
		t.Error("stale fetch message should not clear nodesLoading")
	}
	if len(rm.nodesList) != len(original) {
		t.Errorf("stale fetch message should not replace nodesList, got %+v", rm.nodesList)
	}
}

func TestNodesScreen_ActionMsgFromOtherNetworkIgnored(t *testing.T) {
	m := newNodesModel()
	result, _ := m.handleNodeActionMsg(nodeActionMsg{network: "othernet", nodeID: "aaaa000000000001", action: "approve", status: "active"})
	rm := result.(Model)
	if rm.nodesList[0].Status != "pending" {
		t.Errorf("stale action message should not update status, got %q", rm.nodesList[0].Status)
	}
	if rm.message != "" {
		t.Errorf("stale action message should not set message, got %q", rm.message)
	}
}

func TestNodesScreen_ApproveKeyFiresActionForPendingNode(t *testing.T) {
	m := newNodesModel()
	m.nodesCursor = 0 // pending node
	_, cmd := m.updateV3NetNodes(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd == nil {
		t.Fatal("A on a pending node should return a tea.Cmd")
	}
}

func TestNodesScreen_ActionKeysIgnoredWhileLoading(t *testing.T) {
	m := newNodesModel()
	m.nodesCursor = 0 // pending node
	m.nodesLoading = true

	if _, cmd := m.updateV3NetNodes(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}); cmd != nil {
		t.Error("A while loading should do nothing")
	}
	if _, cmd := m.updateV3NetNodes(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}}); cmd != nil {
		t.Error("B while loading should do nothing")
	}
	result, cmd := m.updateV3NetNodes(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if cmd != nil || result.(Model).nodesConfirmDelete {
		t.Error("D while loading should do nothing")
	}

	// A pending confirm-delete's 'y' should also be ignored while loading.
	m.nodesConfirmDelete = true
	result, cmd = m.updateV3NetNodes(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd != nil {
		t.Error("confirm Y while loading should do nothing")
	}
	if !result.(Model).nodesConfirmDelete {
		t.Error("confirm Y while loading should leave confirmation armed")
	}
}

func TestNodesScreen_ApproveKeyIgnoredForActiveNode(t *testing.T) {
	m := newNodesModel()
	m.nodesCursor = 1 // active node
	_, cmd := m.updateV3NetNodes(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd != nil {
		t.Error("A on an active node should do nothing")
	}
}

func TestNodesScreen_DeleteNeedsConfirm(t *testing.T) {
	m := newNodesModel()
	m.nodesCursor = 0
	result, cmd := m.updateV3NetNodes(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	rm := result.(Model)
	if cmd != nil || !rm.nodesConfirmDelete {
		t.Fatal("D should arm confirmation, not fire the action")
	}
	result, cmd = rm.updateV3NetNodes(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	rm = result.(Model)
	if cmd == nil || rm.nodesConfirmDelete {
		t.Error("Y should fire remove and disarm confirmation")
	}
}

func TestNodesScreen_ActionMsgUpdatesRowStatus(t *testing.T) {
	m := newNodesModel()
	result, _ := m.handleNodeActionMsg(nodeActionMsg{network: "testnet", nodeID: "aaaa000000000001", action: "approve", status: "active"})
	rm := result.(Model)
	if rm.nodesList[0].Status != "active" {
		t.Errorf("status = %q, want active", rm.nodesList[0].Status)
	}
	if rm.message != "Node approved" {
		t.Errorf("message = %q, want %q", rm.message, "Node approved")
	}
}

func TestNodesScreen_BanActionMsgUsesCorrectGrammar(t *testing.T) {
	m := newNodesModel()
	result, _ := m.handleNodeActionMsg(nodeActionMsg{network: "testnet", nodeID: "bbbb000000000002", action: "ban", status: "banned"})
	rm := result.(Model)
	if rm.message != "Node banned" {
		t.Errorf("message = %q, want %q", rm.message, "Node banned")
	}
}

func TestNodesScreen_RemoveMsgDropsRow(t *testing.T) {
	m := newNodesModel()
	result, _ := m.handleNodeActionMsg(nodeActionMsg{network: "testnet", nodeID: "aaaa000000000001", action: "remove"})
	rm := result.(Model)
	if len(rm.nodesList) != 1 || rm.nodesList[0].NodeID != "bbbb000000000002" {
		t.Errorf("list %+v", rm.nodesList)
	}
}

func TestNodesScreen_RemoveMsgClampsScrollToShorterList(t *testing.T) {
	m := newNodesModel()
	nodes := make([]protocol.NodeInfo, 11)
	for i := range nodes {
		nodes[i] = protocol.NodeInfo{NodeID: string(rune('a' + i)), Status: "active"}
	}
	m.nodesList = nodes
	// Cursor/scroll pinned at the bottom of an 11-row list with a
	// nodesListVisible(10)-row window: scroll = 1 shows rows [1..10].
	m.nodesCursor = 10
	m.nodesScroll = 1

	result, _ := m.handleNodeActionMsg(nodeActionMsg{network: "testnet", nodeID: nodes[10].NodeID, action: "remove"})
	rm := result.(Model)

	if len(rm.nodesList) != 10 {
		t.Fatalf("nodesList len = %d, want 10", len(rm.nodesList))
	}
	// With only 10 rows left and a 10-row window, scroll must be 0 or the
	// window renders past the end of the list (blank rows).
	maxScroll := len(rm.nodesList) - nodesListVisible
	if maxScroll < 0 {
		maxScroll = 0
	}
	if rm.nodesScroll > maxScroll {
		t.Errorf("nodesScroll = %d, want <= %d after removal shrank the list", rm.nodesScroll, maxScroll)
	}
}

func TestNodesScreen_CursorMoveScrollsList(t *testing.T) {
	m := newNodesModel()
	nodes := make([]protocol.NodeInfo, 15)
	for i := range nodes {
		nodes[i] = protocol.NodeInfo{NodeID: string(rune('a' + i)), Status: "active"}
	}
	m.nodesList = nodes
	m.nodesCursor = 0
	m.nodesScroll = 0

	for i := 0; i < 10; i++ {
		result, _ := m.updateV3NetNodes(tea.KeyMsg{Type: tea.KeyDown})
		m = result.(Model)
	}
	if m.nodesCursor != 10 {
		t.Fatalf("cursor = %d, want 10", m.nodesCursor)
	}
	if m.nodesScroll == 0 {
		t.Error("nodesScroll should follow the cursor past the visible window")
	}

	for i := 0; i < 10; i++ {
		result, _ := m.updateV3NetNodes(tea.KeyMsg{Type: tea.KeyUp})
		m = result.(Model)
	}
	if m.nodesCursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.nodesCursor)
	}
	if m.nodesScroll != 0 {
		t.Errorf("nodesScroll should return to 0 when cursor moves back above it, got %d", m.nodesScroll)
	}
}

func TestNodesScreen_EscReturnsToRecordList(t *testing.T) {
	m := newNodesModel()
	result, _ := m.updateV3NetNodes(tea.KeyMsg{Type: tea.KeyEscape})
	if result.(Model).mode != modeRecordList {
		t.Error("ESC should return to the record list")
	}
}
