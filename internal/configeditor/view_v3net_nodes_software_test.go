package configeditor

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

func features(states ...protocol.FeatureSupport) []protocol.FeatureSupport { return states }

func TestNodeDetail(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	v3 := &protocol.SoftwareReport{Name: protocol.SoftwareNameVision3, Version: "0.9.8", Build: "a23f12a7c0de"}
	cases := []struct {
		name string
		n    protocol.NodeInfo
		want [nodesDetailRows]string
	}{
		{
			name: "current report, all supported",
			n: protocol.NodeInfo{Software: v3, SoftwareReportedAt: "2026-11-10T09:00:00Z", Online: true,
				Features: features(protocol.FeatureSupport{Title: "Network chat", State: protocol.FeatureSupported})},
			want: [3]string{
				"Software: ViSiON/3 0.9.8 (a23f12a7c0de) (self-reported)",
				"Reported 2026-11-10 09:00 UTC (3h ago); online now",
				"Features: supports all this hub serves",
			},
		},
		{
			name: "stale report on an older release",
			n: protocol.NodeInfo{Software: &protocol.SoftwareReport{Name: protocol.SoftwareNameVision3, Version: "0.9.6"},
				SoftwareReportedAt: "2026-11-01T12:00:00Z",
				Features: features(
					protocol.FeatureSupport{Title: "Central directory", State: protocol.FeatureNeedsUpgrade, UpgradeTo: "0.9.7"},
					protocol.FeatureSupport{Title: "Network chat", State: protocol.FeatureNeedsUpgrade},
					protocol.FeatureSupport{Title: "Last Callers", State: protocol.FeatureHubLacks})},
			want: [3]string{
				"Software: ViSiON/3 0.9.6 (self-reported)",
				"Reported 2026-11-01 12:00 UTC (9d ago); offline, may be stale",
				"Upgrade for: Central directory (v0.9.7+); Not in its software: Network chat; hub does not s…",
			},
		},
		{
			name: "never reported",
			n: protocol.NodeInfo{Online: true,
				Features: features(protocol.FeatureSupport{Title: "Network chat", State: protocol.FeatureUnknown})},
			want: [3]string{
				"Software: unknown (not reported)",
				"Never reported to this hub; online now",
				"Features: unknown until it reports",
			},
		},
		{
			name: "subscribed without a report",
			n:    protocol.NodeInfo{SoftwareReportedAt: "2026-11-10T11:30:00Z"},
			want: [3]string{
				"Software: unknown (not reported)",
				"No report when it last subscribed (30m ago); offline, may be stale",
				"Features: not assessed by this hub",
			},
		},
	}
	for _, c := range cases {
		got := nodeDetail(c.n, now)
		for i := range got {
			want := c.want[i]
			if len([]rune(want)) > 68 {
				want = string([]rune(want)[:67]) + "…"
			}
			if got[i] != want {
				t.Errorf("%s row %d:\n got %q\nwant %q", c.name, i, got[i], want)
			}
		}
	}
}

// The hub validates reports, but the editor still treats one as untrusted:
// a report from an older or broken hub cannot reach the terminal raw.
func TestViewV3NetNodes_SanitizesSoftware(t *testing.T) {
	m := Model{
		width: 80, height: 40,
		mode:         modeV3NetNodes,
		nodesNetwork: "testnet",
		nodesList: []protocol.NodeInfo{{
			NodeID: "aaaa000000000001", BBSName: "b", Status: "active",
			Software: &protocol.SoftwareReport{Name: "\x1b]0;x\x07ViSiON", Version: "\x1b[2J9", Build: "\u009b2J"},
			Features: features(protocol.FeatureSupport{Title: "\x1b[2Jchat", State: protocol.FeatureNeedsUpgrade, UpgradeTo: "\x1b[2J"}),
		}},
		configs: &allConfigs{V3Net: config.V3NetConfig{Hub: config.V3NetHubConfig{Port: 8765}}},
	}
	out := m.viewV3NetNodes()
	if strings.ContainsRune(out, 0x1b) || strings.ContainsRune(out, 0x9b) || strings.ContainsRune(out, 0x07) {
		t.Errorf("rendered view contains a control character from the report: %q", out)
	}
	if !strings.Contains(out, "Software:") {
		t.Error("detail rows not drawn for the node under the cursor")
	}
}
