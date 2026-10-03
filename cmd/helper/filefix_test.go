package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

func TestFilefixSeedLines(t *testing.T) {
	areas := []file.FileArea{
		{Tag: "NODES", Network: "fsxnet", FileEcho: "FSX_NODE"},
		{Tag: "LOCAL"}, // not a file echo
		{Tag: "OTHER", Network: "tqwnet", FileEcho: "TQW_GEN"},
		{Tag: "INFO", Network: "FSXNet", FileEcho: "FSX_INFO"},   // network matched case-insensitively
		{Tag: "NODES2", Network: "fsxnet", FileEcho: "fsx_node"}, // same echo fed to a second area
		{Tag: "HALF", Network: "fsxnet"},                         // network but no echo
	}
	got := filefixSeedLines(areas, "fsxnet")
	want := []string{"+FSX_NODE", "+FSX_INFO"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("filefixSeedLines = %q, want %q", got, want)
	}
	if got := filefixSeedLines(areas, "nosuchnet"); len(got) != 0 {
		t.Errorf("unknown network gave %q", got)
	}
}

func TestRobotBody(t *testing.T) {
	if got, want := robotBody([]string{"+A", "-B\r\n"}), "+A\r-B\r---\r"; got != want {
		t.Errorf("robotBody = %q, want %q", got, want)
	}
}

// The FileFix netmail goes to the link's robot, with its password in the
// subject and the commands in the body, in a packet for the hub.
func TestWriteRobotNetmailFilefix(t *testing.T) {
	out := t.TempDir()
	link := config.FTNLinkConfig{Address: "21:1/100", PacketPassword: "pkt", TICPassword: "ticpw", FilefixName: "AllFix"}
	hub := hubTarget{
		ftnCfg: ftnConfig{OutboundPath: out},
		netKey: "fsxnet",
		netCfg: ftnNetworkConfig{OwnAddress: "21:4/158.2"},
		link:   &link,
	}
	path, err := writeRobotNetmail(t.TempDir(), hub, link.FilefixRobot(), link.FilefixPass(), robotBody([]string{"+FSX_NODE"}), "filefix_*.pkt")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != out || !strings.HasPrefix(filepath.Base(path), "filefix_") {
		t.Errorf("packet written to %s, want filefix_*.pkt in %s", path, out)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }() // read-only
	hdr, msgs, err := ftn.ReadPacket(f)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.DestNet != 1 || hdr.DestNode != 100 {
		t.Errorf("packet addressed to %d/%d, want 1/100", hdr.DestNet, hdr.DestNode)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	m := msgs[0]
	if m.To != "AllFix" || m.Subject != "ticpw" {
		t.Errorf("To %q Subject %q, want AllFix / ticpw", m.To, m.Subject)
	}
	body := ftn.ParsePackedMessageBody(m.Body)
	if !strings.Contains(body.Text, "+FSX_NODE\r---") {
		t.Errorf("body text %q lacks the command", body.Text)
	}
	var hasFMPT bool
	for _, k := range body.Kludges {
		if k == "FMPT 2" {
			hasFMPT = true
		}
	}
	if !hasFMPT {
		t.Errorf("point origin without FMPT kludge: %q", body.Kludges)
	}
}
