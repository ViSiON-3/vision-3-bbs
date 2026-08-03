package configeditor

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// TestViewV3NetNodes_SanitizesBBSNameControlChars ensures a BBSName from a
// remote subscribe request (attacker-controlled) cannot inject ANSI/OSC
// escape sequences into the sysop's terminal.
func TestViewV3NetNodes_SanitizesBBSNameControlChars(t *testing.T) {
	m := Model{
		width: 80, height: 25,
		mode:         modeV3NetNodes,
		nodesNetwork: "testnet",
		nodesList: []protocol.NodeInfo{
			{NodeID: "aaaa000000000001", BBSName: "\x1b[2J evil", Status: "pending", CreatedAt: "2026-01-01"},
		},
		configs: &allConfigs{V3Net: config.V3NetConfig{
			KeystorePath: "data/v3net.key",
			Hub:          config.V3NetHubConfig{Port: 8765},
		}},
	}
	out := m.viewV3NetNodes()
	if strings.ContainsRune(out, 0x1b) {
		t.Errorf("rendered view contains raw ESC byte from attacker-controlled BBSName: %q", out)
	}
}
