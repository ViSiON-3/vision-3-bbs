package configeditor

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

var testSGR = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// stripStyle removes lipgloss colour escapes so the text can be measured.
func stripStyle(s string) string { return testSGR.ReplaceAllString(s, "") }

// A record list row is cut and padded to the box width in characters. The
// file area row has 56 ASCII columns before the path, so an odd box width
// puts a byte cut inside a two-byte "é".
func TestRecordRowCutByRune(t *testing.T) {
	const boxW = 61
	m := Model{recordType: "filearea", configs: &allConfigs{
		FileAreas: []file.FileArea{{ID: 1, Tag: "UTILS", Name: "Utilities", Path: strings.Repeat("é", 60)}},
	}}
	got := m.renderRecordRow(0, boxW)
	if !utf8.ValidString(got) {
		t.Errorf("row %q is not valid UTF-8", got)
	}
	if n := utf8.RuneCountInString(got); n != boxW {
		t.Errorf("row is %d columns, want %d: %q", n, boxW, got)
	}
}

// The hosted-network area list cuts its rows by character. Two base paths,
// offset by one ASCII byte, make sure one of them has a byte cut landing
// inside an "é" whatever the box width's parity.
func TestHubAreaRowsCutByRune(t *testing.T) {
	m := Model{width: 80, height: 25, hubAreaNetwork: "felnet", configs: &allConfigs{
		MsgAreas: []message.MessageArea{
			{ID: 1, Tag: "GEN", Name: "General", AreaType: "v3net", Network: "felnet", BasePath: strings.Repeat("é", 80)},
			{ID: 2, Tag: "COD", Name: "Coding", AreaType: "v3net", Network: "felnet", BasePath: "A" + strings.Repeat("é", 80)},
		},
	}}
	out := m.viewV3NetHubAreas()
	if !utf8.ValidString(out) {
		t.Errorf("hub area list is not valid UTF-8:\n%s", out)
	}
}

// The area browser's tag column is 16 characters: "A" then "é"s would have
// its 16th byte inside an "é".
func TestAreaBrowserTagCutByRune(t *testing.T) {
	m := v3netAreaBrowserModel()
	m.areaBrowserAreas[0].Tag = "A" + strings.Repeat("é", 20)
	out := stripStyle(m.viewV3NetAreaBrowser())
	if !utf8.ValidString(out) {
		t.Errorf("area browser is not valid UTF-8:\n%s", out)
	}
	if want := "A" + strings.Repeat("é", 15) + " "; !strings.Contains(out, want) {
		t.Errorf("area browser lacks the tag cut to 16 characters %q:\n%s", want, out)
	}
}

// The node list shows the first 10 characters of the hub's created_at
// value, which comes from the remote hub and so may be any text.
func TestV3NetNodesJoinedCutByRune(t *testing.T) {
	m := Model{
		width: 80, height: 25,
		mode:         modeV3NetNodes,
		nodesNetwork: "testnet",
		nodesList: []protocol.NodeInfo{
			{NodeID: "aaaa000000000001", BBSName: "Board", Status: "active", CreatedAt: "A" + strings.Repeat("é", 12)},
		},
		configs: &allConfigs{V3Net: config.V3NetConfig{
			KeystorePath: "data/v3net.key",
			Hub:          config.V3NetHubConfig{Port: 8765},
		}},
	}
	out := stripStyle(m.viewV3NetNodes())
	if !utf8.ValidString(out) {
		t.Errorf("node list is not valid UTF-8:\n%s", out)
	}
	if want := "A" + strings.Repeat("é", 9); !strings.Contains(out, want) || strings.Contains(out, want+"é") {
		t.Errorf("node list does not show created_at cut to 10 characters %q:\n%s", want, out)
	}
}

// The system config field being edited shows its value cut to the field
// width in characters.
func TestSysFieldActiveCutByRune(t *testing.T) {
	value := "A" + strings.Repeat("é", 30)
	f := fieldDef{Label: "Board Name", Type: ftString, Width: 20, Get: func() string { return value }}
	m := Model{mode: modeSysConfigEdit, editField: 0}
	row, _ := m.renderSysField(0, f)
	got := stripStyle(row)
	if !utf8.ValidString(got) {
		t.Errorf("field %q is not valid UTF-8", got)
	}
	if want := "A" + strings.Repeat("é", 19); !strings.Contains(got, want) || strings.Contains(got, want+"é") {
		t.Errorf("field = %q, want the value cut to 20 characters", got)
	}
}
