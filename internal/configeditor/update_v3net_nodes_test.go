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
	result, _ := m.handleFetchNodesMsg(fetchNodesMsg{nodes: []protocol.NodeInfo{{NodeID: "cccc000000000003"}}})
	rm := result.(Model)
	if rm.nodesLoading || len(rm.nodesList) != 1 || rm.nodesError != "" {
		t.Errorf("model %+v", rm)
	}
}

func TestNodesScreen_FetchErrorUsesFriendlyText(t *testing.T) {
	m := newNodesModel()
	result, _ := m.handleFetchNodesMsg(fetchNodesMsg{err: errors.New("boom")})
	rm := result.(Model)
	if rm.nodesError == "" {
		t.Error("error should be surfaced")
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
	result, _ := m.handleNodeActionMsg(nodeActionMsg{nodeID: "aaaa000000000001", action: "approve", status: "active"})
	rm := result.(Model)
	if rm.nodesList[0].Status != "active" {
		t.Errorf("status = %q, want active", rm.nodesList[0].Status)
	}
}

func TestNodesScreen_RemoveMsgDropsRow(t *testing.T) {
	m := newNodesModel()
	result, _ := m.handleNodeActionMsg(nodeActionMsg{nodeID: "aaaa000000000001", action: "remove"})
	rm := result.(Model)
	if len(rm.nodesList) != 1 || rm.nodesList[0].NodeID != "bbbb000000000002" {
		t.Errorf("list %+v", rm.nodesList)
	}
}

func TestNodesScreen_EscReturnsToRecordList(t *testing.T) {
	m := newNodesModel()
	result, _ := m.updateV3NetNodes(tea.KeyMsg{Type: tea.KeyEscape})
	if result.(Model).mode != modeRecordList {
		t.Error("ESC should return to the record list")
	}
}
