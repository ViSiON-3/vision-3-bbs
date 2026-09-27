package tosser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// A hub sending an echo tag in lower case ("fsx_test") must reach the area
// configured in upper case ("FSX_TEST"). FTN tags are case-insensitive; before
// this, the message failed as "unknown area" and the whole packet was moved
// to temp_in.
func TestTossMatchesEchoTagIgnoringCase(t *testing.T) {
	env := setupTestEnv(t)
	tsr, err := New("testnet", env.netCfg, env.globalCfg, env.dupeDB, env.msgMgr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pktData := makePktSimple(t, "fsx_test", "Sender", "All", "Lower", "lower-case tag\r", "21:4/100 CA5E0001")
	pktPath := filepath.Join(env.inboundDir, "lower001.pkt")
	if err := os.WriteFile(pktPath, pktData, 0644); err != nil {
		t.Fatal(err)
	}

	result := tsr.ProcessInbound()
	if result.MessagesImported != 1 || len(result.Errors) != 0 {
		t.Fatalf("imported %d, errors %v; want 1 imported and no errors", result.MessagesImported, result.Errors)
	}
	if _, err := os.Stat(pktPath); !os.IsNotExist(err) {
		t.Error("packet should be removed from inbound after a clean toss")
	}

	base, err := env.msgMgr.GetBase(1)
	if err != nil {
		t.Fatalf("GetBase: %v", err)
	}
	defer base.Close()
	if n, _ := base.GetMessageCount(); n != 1 {
		t.Errorf("FSX_TEST holds %d messages, want 1", n)
	}
}

func TestOrphanFTNAreas(t *testing.T) {
	cfg := config.FTNConfig{Networks: map[string]config.FTNNetworkConfig{"zeronet": {}, "fsxNet": {}}}
	areas := []*message.MessageArea{
		{Tag: "zer0net_netmail", AreaType: "netmail", Network: "zer0net"}, // digit zero: orphan
		{Tag: "0N-BBS", AreaType: "echomail", Network: "zeronet"},
		{Tag: "FSX_GEN", AreaType: "echomail", Network: "FSXNET"}, // case differs: fine
		{Tag: "DOVE", AreaType: "qwknet", Network: "dovenet"},     // not FTN: ignored
		{Tag: "LOCAL", AreaType: "local"},
	}
	got := OrphanFTNAreas(cfg, areas)
	if len(got) != 1 || got[0].Tag != "zer0net_netmail" {
		tags := make([]string, len(got))
		for i, a := range got {
			tags[i] = a.Tag
		}
		t.Fatalf("orphans = %v, want [zer0net_netmail]", tags)
	}
}
