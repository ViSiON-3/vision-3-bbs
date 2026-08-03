package configeditor

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// TestHubRecordListHelp_MentionsNodesKey ensures the hosted-networks list
// advertises the N (node management) key added with the nodes screen.
func TestHubRecordListHelp_MentionsNodesKey(t *testing.T) {
	m := Model{
		width: 80, height: 25,
		mode:       modeRecordList,
		recordType: "v3nethub",
		configs: &allConfigs{V3Net: config.V3NetConfig{
			Hub: config.V3NetHubConfig{Networks: []config.V3NetHubNetwork{{Name: "felonynet"}}},
		}},
	}
	view := m.viewRecordList()
	if !strings.Contains(view, "N - Nodes") {
		t.Error("v3nethub record list help should mention N - Nodes")
	}
}
