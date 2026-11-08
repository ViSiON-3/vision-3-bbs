package menu

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// activityNet is a network with Central on and both activity features
// offered and served.
func activityNet(name string) CentralNetworkInfo {
	on := CentralActivityInfo{Enabled: true, Supported: true}
	return CentralNetworkInfo{Network: name, Enabled: true, SupportKnown: true, Supported: true, Presence: on, LastCallers: on}
}

// activityBoardOf returns an online board's snapshot of kind with entries.
func activityBoardOf(node, bbs, kind string, entries ...protocol.CentralActivityEntry) protocol.CentralActivityBoard {
	return protocol.CentralActivityBoard{Online: true, Activity: protocol.CentralActivity{Kind: kind, NodeID: node, BBS: bbs, Entries: entries}}
}

// runPrefs runs My Preferences for u with input, returning the output.
func runPrefs(t *testing.T, fake *centralFake, u *user.User, input string) string {
	t.Helper()
	e := &MenuExecutor{V3NetCentral: fake, RootConfigPath: t.TempDir()}
	ts := newTestSession(input)
	c := &cmdCtx{e: e, s: ts, terminal: newTestTerminal(ts), currentUser: u, outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24}
	if _, _, err := runV3NetCentralPrefs(c, ""); err != nil && !errors.Is(err, errInputAborted) {
		t.Fatal(err)
	}
	return ts.output()
}

// Every switch starts off; turning one on is the caller's own act, shown
// with a preview of what other boards see; turning it off again clears it.
func TestCentralPrefsSwitches(t *testing.T) {
	fake := &centralFake{nets: []CentralNetworkInfo{activityNet("testnet")}}
	u := &user.User{Handle: "|12Caller"}
	out := runPrefs(t, fake, u, "1\r2\r3\rq\r")
	if !u.CentralPresence || u.CentralAvailability != protocol.CentralAvailable || u.CentralLastCallsSince == nil {
		t.Fatalf("switches not set: %+v", u)
	}
	if since := *u.CentralLastCallsSince; time.Since(since) > time.Minute {
		t.Errorf("last-calls consent should start now, got %s", since)
	}
	if fake.notifies != 3 {
		t.Errorf("each change should republish: %d notifies", fake.notifies)
	}
	if !strings.Contains(out, "Nothing about you is shared") || !strings.Contains(out, "What other boards would see") {
		t.Errorf("missing statement or preview: %q", out)
	}
	if !strings.Contains(ansi.StripAnsi(out), "Who's Online: Caller on") {
		t.Errorf("preview should show the handle as published, colour codes out: %q", ansi.StripAnsi(out))
	}

	// Availability cycles available, busy, not shown; the switches turn off.
	runPrefs(t, fake, u, "2\r2\r1\r3\rq\r")
	if u.CentralPresence || u.CentralAvailability != "" || u.CentralLastCallsSince != nil {
		t.Errorf("switches not cleared: %+v", u)
	}
}

// A feature the sysop does not offer cannot be switched on.
func TestCentralPrefsRefusesUnofferedFeature(t *testing.T) {
	fake := &centralFake{nets: []CentralNetworkInfo{{Network: "testnet", Enabled: true, SupportKnown: true, Supported: true}}}
	u := &user.User{Handle: "Caller"}
	out := runPrefs(t, fake, u, "1\r3\rq\r")
	if u.CentralPresence || u.CentralLastCallsSince != nil || fake.notifies != 0 {
		t.Errorf("unoffered feature switched on: %+v", u)
	}
	if !strings.Contains(out, "does not offer network Who's Online") {
		t.Errorf("no refusal shown: %q", out)
	}
}

// A caller shared only by a network's Callers Default sees both switches
// on, and pressing each turns it off as their own choice.
func TestCentralPrefsCallersDefault(t *testing.T) {
	net := activityNet("testnet")
	net.Presence.CallersDefault, net.LastCallers.CallersDefault = true, true
	fake := &centralFake{nets: []CentralNetworkInfo{net}}
	u := &user.User{Handle: "Caller"}
	out := ansi.StripAnsi(runPrefs(t, fake, u, "q\r"))
	if strings.Contains(out, "Nothing about you is shared") || strings.Count(out, "On  (testnet)") != 2 {
		t.Errorf("default-on caller not shown as sharing: %q", out)
	}
	runPrefs(t, fake, u, "1\r3\rq\r")
	if u.CentralPresence || !u.CentralPresenceChosen || u.CentralLastCallsSince != nil || !u.CentralLastCallsChosen {
		t.Fatalf("switches not turned off as the caller's choice: %+v", u)
	}
	if out := runPrefs(t, fake, u, "q\r"); !strings.Contains(out, "Nothing about you is shared") {
		t.Errorf("opted-out caller still shown as sharing: %q", ansi.StripAnsi(out))
	}
	// Pressing again opts back in, calls from now.
	runPrefs(t, fake, u, "1\r3\rq\r")
	if !u.CentralPresence || u.CentralLastCallsSince == nil {
		t.Errorf("switches not turned back on: %+v", u)
	}
}

// A caller can still withdraw an opt-in once no network offers the
// feature.
func TestCentralPrefsWithdrawsUnofferedOptIn(t *testing.T) {
	fake := &centralFake{nets: []CentralNetworkInfo{{Network: "testnet", Enabled: true, SupportKnown: true, Supported: true}}}
	agreed := time.Now().UTC().Add(-time.Hour)
	u := &user.User{Handle: "Caller", CentralPresence: true, CentralLastCallsSince: &agreed}
	runPrefs(t, fake, u, "1\r3\rq\r")
	if u.CentralPresence || u.CentralLastCallsSince != nil {
		t.Errorf("opt-in not withdrawn: %+v", u)
	}
}

// The Central menu lists Who's Online and Last Callers only where the
// sysop offers them and the hub serves them.
func TestCentralMenuOffersActivityOnlyWhenAvailable(t *testing.T) {
	off := &centralFake{nets: []CentralNetworkInfo{{Network: "testnet", Enabled: true, SupportKnown: true, Supported: true}}}
	if out := runCentralScreen(t, off, "q\r", runV3NetCentral, ""); strings.Contains(out, "ho's Online") || strings.Contains(out, "ast Callers") {
		t.Errorf("activity offered without the sysop's switch: %q", out)
	}
	on := &centralFake{nets: []CentralNetworkInfo{activityNet("testnet")}}
	out := runCentralScreen(t, on, "q\r", runV3NetCentral, "")
	if !strings.Contains(out, "ho's Online") || !strings.Contains(out, "ast Callers") {
		t.Errorf("activity not offered: %q", out)
	}
}

// Who's Online groups a caller's sessions on one board into one row.
func TestCentralWhoGroupsSessions(t *testing.T) {
	fake := &centralFake{
		nets: []CentralNetworkInfo{activityNet("testnet")},
		acts: map[string]map[string][]protocol.CentralActivityBoard{"testnet": {
			protocol.CentralKindPresence: {
				activityBoardOf("aaaa", "Alpha BBS", protocol.CentralKindPresence,
					protocol.CentralActivityEntry{Handle: "Alice", Session: "s1", Availability: protocol.CentralAvailable},
					protocol.CentralActivityEntry{Handle: "Alice", Session: "s2"}),
				activityBoardOf("bbbb", "Beta BBS", protocol.CentralKindPresence,
					protocol.CentralActivityEntry{Handle: "Bob", Session: "s3", Availability: protocol.CentralBusy}),
			},
		}},
	}
	out := ansi.StripAnsi(runCentralScreen(t, fake, "\r", runV3NetCentralWho, ""))
	if strings.Count(out, "Alice") != 1 || !strings.Contains(out, "(2 nodes)") || !strings.Contains(out, "busy") {
		t.Errorf("who's online: %q", out)
	}
}

// LASTCALLERS reads an optional count and a --network or --all flag.
func TestParseLastCallersArgs(t *testing.T) {
	for args, want := range map[string]struct {
		limit int
		mode  lastCallersMode
	}{
		"":             {defaultLastCallerRows, lastCallersLocal},
		"25":           {25, lastCallersLocal},
		"20 --network": {20, lastCallersNetwork},
		"--all":        {defaultLastCallerRows, lastCallersAll},
		"--ALL 15":     {15, lastCallersAll},
		"junk":         {defaultLastCallerRows, lastCallersLocal},
	} {
		limit, mode := parseLastCallersArgs(args)
		if limit != want.limit || mode != want.mode {
			t.Errorf("%q: %d %d, want %+v", args, limit, mode, want)
		}
	}
}

// --all merges this board's calls with the network's, oldest first, each
// flagged, and leaves out this board's own published snapshot, which
// repeats its local calls.
func TestCentralCallRowsMergeAndFlag(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	at := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }
	feeds := map[string][]protocol.CentralActivityBoard{"testnet": {
		activityBoardOf("self", "Home", protocol.CentralKindLastCallers, protocol.CentralActivityEntry{Handle: "Me", At: at(time.Hour)}),
		activityBoardOf("bbbb", "Beta", protocol.CentralKindLastCallers,
			protocol.CentralActivityEntry{Handle: "Bob", At: at(3 * time.Hour)},
			protocol.CentralActivityEntry{Handle: "Bea", At: at(30 * time.Minute)},
			// Past the limit since the feed was fetched: not drawn.
			protocol.CentralActivityEntry{Handle: "Gone", At: at(25 * time.Hour)}),
	}}
	local := []user.CallRecord{{Handle: "Me", GroupLocation: "Akron, OH", ConnectTime: now.Add(-time.Hour)}, {Handle: "Lou", ConnectTime: now.Add(-2 * time.Hour)}}

	rows := centralCallRows(feeds, "self", local, true, 10)
	var got []string
	for _, r := range rows {
		flag := r.network
		if r.local {
			flag = "local"
		}
		got = append(got, flag+":"+r.handle)
	}
	if strings.Join(got, ",") != "testnet:Bob,local:Lou,local:Me,testnet:Bea" {
		t.Errorf("merged rows %v", got)
	}
	// Location: the board for a network call, the caller's own for a local one.
	if rows[0].location != "Beta" || rows[2].location != "Akron, OH" || rows[1].location != "" {
		t.Errorf("locations %+v", rows)
	}
	// Network only: this board's snapshot is shown as the network has it.
	rows = centralCallRows(feeds, "self", local, false, 2)
	if len(rows) != 2 || rows[0].handle != "Me" || rows[1].handle != "Bea" || rows[0].local {
		t.Errorf("network rows %+v", rows)
	}
}

// LASTCALLERS --all shows the flag column and the toggle; --network says
// so when no network offers it.
func TestLastCallersNetworkScreens(t *testing.T) {
	fake := &centralFake{
		nets: []CentralNetworkInfo{activityNet("testnet")},
		acts: map[string]map[string][]protocol.CentralActivityBoard{"testnet": {
			protocol.CentralKindLastCallers: {activityBoardOf("bbbb", "Beta BBS", protocol.CentralKindLastCallers,
				protocol.CentralActivityEntry{Handle: "Bob", At: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)})},
		}},
	}
	out := ansi.StripAnsi(runCentralScreen(t, fake, "q", runLastCallers, "20 --all"))
	for _, want := range []string{"From", "local = this board", "testnet", "Bob", "Beta BBS", "Hide this board's callers"} {
		if !strings.Contains(out, want) {
			t.Errorf("--all screen lacks %q: %q", want, out)
		}
	}
	none := &centralFake{nets: []CentralNetworkInfo{{Network: "testnet", Enabled: true, SupportKnown: true, Supported: true}}}
	if out := runCentralScreen(t, none, "\r", runLastCallers, "--network"); !strings.Contains(out, "not available on any network") {
		t.Errorf("--network without the feature: %q", out)
	}
}
