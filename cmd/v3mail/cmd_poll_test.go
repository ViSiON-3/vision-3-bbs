package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func TestFTNPollPlan(t *testing.T) {
	cfg := config.FTNConfig{Networks: map[string]config.FTNNetworkConfig{
		"zeronet": {InternalTosserEnabled: true, Links: []config.FTNLinkConfig{
			{Address: "99:1/1", Hostname: "hub.zeronet.example"},
		}},
		"fsxnet": {InternalTosserEnabled: true, Links: []config.FTNLinkConfig{
			{Address: "21:1/100", Hostname: "hub.fsxnet.example", Port: 24555},
			{Address: "21:1/200"}, // no hostname: the hub calls us
		}},
		"offnet": {InternalTosserEnabled: false, Links: []config.FTNLinkConfig{
			{Address: "1:1/1", Hostname: "off.example"},
		}},
	}}

	targets, uncallable := ftnPollPlan(cfg, "")
	want := []ftnPollTarget{{"fsxnet", "21:1/100"}, {"zeronet", "99:1/1"}}
	if len(targets) != len(want) {
		t.Fatalf("targets = %+v, want %+v", targets, want)
	}
	for i := range want {
		if targets[i] != want[i] {
			t.Errorf("targets[%d] = %+v, want %+v", i, targets[i], want[i])
		}
	}
	if len(uncallable) != 1 || uncallable[0] != (ftnPollTarget{"fsxnet", "21:1/200"}) {
		t.Errorf("uncallable = %+v, want the hostname-less fsxnet link", uncallable)
	}

	targets, _ = ftnPollPlan(cfg, "zeronet")
	if len(targets) != 1 || targets[0].Network != "zeronet" {
		t.Errorf("only zeronet: got %+v", targets)
	}
}

func TestCountTransfers(t *testing.T) {
	out := strings.Join([]string{
		"+ 27 Sep 10:00:00 [42] call to 21:1/100@fsxnet",
		"+ 27 Sep 10:00:01 [42] sent: /bbs/data/ftn/out/00010064.mo0 (812, 812.00 CPS, 21:1/100@fsxnet)",
		"+ 27 Sep 10:00:02 [42] rcvd: 0df9b0c6.pkt (1644, 1644.00 CPS, 21:1/100@fsxnet)",
		"+ 27 Sep 10:00:02 [42] rcvd: 5a3e11f0.we0 (20480, 20480.00 CPS, 21:1/100@fsxnet)",
		"+ 27 Sep 10:00:03 [42] done (to 21:1/100@fsxnet, OK, S/R: 1/2 (812/22124 bytes))",
	}, "\n")
	sent, rcvd := countTransfers(out)
	if sent != 1 || rcvd != 2 {
		t.Errorf("sent %d, rcvd %d; want 1 and 2", sent, rcvd)
	}
}

func TestResolvePollNetwork(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("ftn.json", `{"networks":{"fsxNet":{"own_address":"21:1/1"}}}`)
	write("qwknet.json", `{"networks":{"dovenet":{"enabled":true}}}`)

	if f, q, err := resolvePollNetwork(dir, "FSXNET"); err != nil || f != "fsxNet" || q != "" {
		t.Errorf("FSXNET: got %q, %q, %v; want fsxNet as FTN only", f, q, err)
	}
	if f, q, err := resolvePollNetwork(dir, "DoveNet"); err != nil || f != "" || q != "dovenet" {
		t.Errorf("DoveNet: got %q, %q, %v; want dovenet as QWK only", f, q, err)
	}
	if _, _, err := resolvePollNetwork(dir, "zeronet"); err == nil {
		t.Error("unknown network was accepted")
	}
}

// callHub runs binkd with the hub's address@domain and summarizes the
// session. A stand-in script plays binkd so the real one is not needed.
func TestCallHubRunsBinkd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as a stand-in for binkd")
	}
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	fake := filepath.Join(dir, "binkd")
	script := "#!/bin/sh\necho \"$@\" > " + argsFile + "\n" +
		"echo '+ 27 Sep 10:00:02 [42] rcvd: 0df9b0c6.pkt (1644, 1644.00 CPS, 99:1/1@zeronet)'\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	ok := callHub(context.Background(), fake, "/bbs/data/ftn/binkd.conf", dir,
		ftnPollTarget{Network: "zeronet", Address: "99:1/1"}, 10*time.Second, false)
	if !ok {
		t.Fatal("callHub reported failure for a clean session")
	}
	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if want := "-p -P 99:1/1@zeronet /bbs/data/ftn/binkd.conf"; strings.TrimSpace(string(got)) != want {
		t.Errorf("binkd args = %q, want %q", strings.TrimSpace(string(got)), want)
	}

	failing := filepath.Join(dir, "binkd-fail")
	if err := os.WriteFile(failing, []byte("#!/bin/sh\necho 'connect failed'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if callHub(context.Background(), failing, "x.conf", dir, ftnPollTarget{"zeronet", "99:1/1"}, 10*time.Second, false) {
		t.Error("callHub reported success for a failed binkd")
	}
}

// A hook binkd starts can hold the output pipes open after binkd exits; the
// call must still return rather than wait on it.
func TestCallHubReturnsWhenAHookHoldsOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as a stand-in for binkd")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "binkd")
	script := "#!/bin/sh\nsleep 30 &\necho '+ 27 Sep 10:00:02 [42] sent: out.pkt (1, 1.00 CPS, 99:1/1@zeronet)'\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	ok := callHub(context.Background(), fake, "x.conf", dir, ftnPollTarget{"zeronet", "99:1/1"}, time.Minute, false)
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("callHub waited %s on a hook's open output", elapsed)
	}
	if !ok {
		t.Error("callHub reported failure though binkd exited cleanly")
	}
}

// An enabled network with no links has no hub to call but still counts, so
// the poll tosses what is already in its inbound.
func TestFTNPollNetworksIncludesLinklessNetworks(t *testing.T) {
	cfg := config.FTNConfig{Networks: map[string]config.FTNNetworkConfig{
		"fsxnet":  {InternalTosserEnabled: true},
		"offline": {},
	}}
	if got := ftnPollNetworks(cfg, ""); len(got) != 1 || got[0] != "fsxnet" {
		t.Errorf("networks = %v, want [fsxnet]", got)
	}
	if targets, uncallable := ftnPollPlan(cfg, ""); len(targets)+len(uncallable) != 0 {
		t.Errorf("plan for a linkless network = %v %v, want none", targets, uncallable)
	}
}
