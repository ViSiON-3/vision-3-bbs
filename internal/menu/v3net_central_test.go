package menu

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// centralFake is a V3NetCentralProvider with scripted networks and
// directories.
type centralFake struct {
	nets    []CentralNetworkInfo
	dirs    map[string][]protocol.CentralDirectoryEntry
	errs    map[string]error
	fetches int
	// acts holds each network's activity feeds by kind; notifies counts
	// NotifyCentralActivity calls; node is this board's node ID.
	acts     map[string]map[string][]protocol.CentralActivityBoard
	notifies int
	node     string
	// alerts records SendCentralAlert calls; areaNets binds area IDs to
	// networks for NetworkForArea.
	alerts   []sentAlert
	areaNets map[int]string
}

// CentralActivity returns the scripted feed of kind on network.
func (f *centralFake) CentralActivity(_ context.Context, network, kind string, _ bool) ([]protocol.CentralActivityBoard, error) {
	f.fetches++
	if err := f.errs[network]; err != nil {
		return nil, err
	}
	return f.acts[network][kind], nil
}

// NotifyCentralActivity counts the republish requests.
func (f *centralFake) NotifyCentralActivity() { f.notifies++ }

// NodeID returns the scripted node ID.
func (f *centralFake) NodeID() string { return f.node }

// sentAlert is one SendCentralAlert call.
type sentAlert struct{ network, kind, handle, room, area string }

// SendCentralAlert records the alert.
func (f *centralFake) SendCentralAlert(network, kind, handle, room, area string) {
	f.alerts = append(f.alerts, sentAlert{network, kind, handle, room, area})
}

// NetworkForArea returns the scripted binding.
func (f *centralFake) NetworkForArea(areaID int) string { return f.areaNets[areaID] }

func (f *centralFake) CentralNetworks() []CentralNetworkInfo { return f.nets }
func (f *centralFake) CentralDirectory(_ context.Context, network string, _ bool) ([]protocol.CentralDirectoryEntry, error) {
	f.fetches++
	if err := f.errs[network]; err != nil {
		return nil, err
	}
	return f.dirs[network], nil
}

func centralEntry(name, desc, addr string, online bool, seenAgo time.Duration) protocol.CentralDirectoryEntry {
	now := time.Now().UTC()
	e := protocol.CentralDirectoryEntry{
		Listing: protocol.CentralListing{
			V3NetCentral: protocol.CentralListingVersion, Network: "testnet", NodeID: "aaaa1111bbbb2222",
			Name: name, Description: desc, Capabilities: []string{protocol.FeatureCentralDirectory},
			PublishedAt: now.Add(-time.Hour).Format(time.RFC3339), ExpiresAt: now.Add(29 * 24 * time.Hour).Format(time.RFC3339),
		},
		Online: online,
	}
	if addr != "" {
		e.Listing.Addresses = []string{addr}
	}
	if seenAgo >= 0 {
		e.LastSeenAt = now.Add(-seenAgo).Format(time.RFC3339)
	}
	return e
}

func runCentralScreen(t *testing.T, fake V3NetCentralProvider, input string, fn RunnableFunc, args string) string {
	t.Helper()
	return runCentralScreenMode(t, fake, input, fn, args, ansi.OutputModeUTF8)
}

// runCentralScreenMode is runCentralScreen for a caller in the given output
// mode.
func runCentralScreenMode(t *testing.T, fake V3NetCentralProvider, input string, fn RunnableFunc, args string, mode ansi.OutputMode) string {
	t.Helper()
	e := &MenuExecutor{V3NetCentral: fake, RootConfigPath: t.TempDir()}
	ts := newTestSession(input)
	c := &cmdCtx{
		e: e, s: ts, terminal: newTestTerminal(ts),
		currentUser: &user.User{Handle: "Caller", AccessLevel: 10},
		outputMode:  mode, termWidth: 80, termHeight: 24,
	}
	if _, _, err := fn(c, args); err != nil && !errors.Is(err, errInputAborted) {
		t.Fatalf("screen returned error: %v", err)
	}
	return ts.output()
}

func TestCentralMenuHidesBoardsWhenUnavailable(t *testing.T) {
	// Enabled, but the hub does not serve the directory: no Boards entry,
	// a note saying why, and B is refused.
	fake := &centralFake{nets: []CentralNetworkInfo{{Network: "testnet", Enabled: true, SupportKnown: true, Supported: false}}}
	out := runCentralScreen(t, fake, "b\rq\r", runV3NetCentral, "")
	if strings.Contains(out, "]oards") {
		t.Errorf("Boards offered on an unsupported network: %q", out)
	}
	if !strings.Contains(out, "does not serve the board directory") {
		t.Errorf("expected the unsupported note, got %q", out)
	}
	if !strings.Contains(out, "not available on any network") {
		t.Errorf("B should be refused, got %q", out)
	}
	if !strings.Contains(out, "]references") || !strings.Contains(out, "]tatus") {
		t.Errorf("Preferences and Status should always be offered, got %q", out)
	}
	if fake.fetches != 0 {
		t.Errorf("no directory fetch expected, got %d", fake.fetches)
	}

	// Enabled and supported: Boards is offered.
	fake = &centralFake{nets: []CentralNetworkInfo{{Network: "testnet", Enabled: true, SupportKnown: true, Supported: true}}}
	out = runCentralScreen(t, fake, "q\r", runV3NetCentral, "")
	if !strings.Contains(out, "]oards") {
		t.Errorf("Boards missing on an available network: %q", out)
	}
}

func TestCentralMenuWhenDisabled(t *testing.T) {
	// V3Net off entirely.
	out := runCentralScreen(t, nil, "\r", runV3NetCentral, "")
	if !strings.Contains(out, "V3Net is not enabled") {
		t.Errorf("expected the V3Net-off notice, got %q", out)
	}
	// Networks subscribed but Central enabled on none, even one that
	// publishes a listing: the caller side stays off.
	fake := &centralFake{nets: []CentralNetworkInfo{{Network: "testnet", Enabled: false, Publishing: true, Supported: true, SupportKnown: true}}}
	out = runCentralScreen(t, fake, "\r", runV3NetCentral, "")
	if !strings.Contains(out, "V3Net Central is not enabled") {
		t.Errorf("expected the Central-off notice, got %q", out)
	}
	if fake.fetches != 0 {
		t.Error("nothing should be fetched when Central is off")
	}
}

func TestCentralBoardsListsFreshnessAndDetails(t *testing.T) {
	fake := &centralFake{
		nets: []CentralNetworkInfo{{Network: "testnet", Enabled: true, SupportKnown: true, Supported: true}},
		dirs: map[string][]protocol.CentralDirectoryEntry{"testnet": {
			centralEntry("Old Haunt", "Back someday", "telnet://haunt.example:23", false, 30*24*time.Hour),
			centralEntry("Sector 7", "Demoscene and |04code|07", "ssh://sector7.example:2222", true, 0),
			centralEntry("The Vault", "Files since 1991", "", false, 2*24*time.Hour),
		}},
	}
	// Enter on the first (online) row opens details; any key returns; q quits.
	out := runCentralScreen(t, fake, "\rxq", runV3NetCentralBoards, "")

	// Online first, then recent, then stale.
	iS, iV, iO := strings.Index(out, "Sector 7"), strings.Index(out, "The Vault"), strings.Index(out, "Old Haunt")
	if iS < 0 || iV < 0 || iO < 0 || iS >= iV || iV >= iO {
		t.Errorf("rows not ordered online, recent, stale: S=%d V=%d O=%d", iS, iV, iO)
	}
	if !strings.Contains(out, "■ Sector 7") || !strings.Contains(out, "√ The Vault") || !strings.Contains(out, "· Old Haunt") {
		t.Errorf("freshness markers missing: %q", out)
	}
	// Pipe codes in another board's text are shown as written, not run.
	if strings.Contains(out, "\x1b[0;31m") || strings.Contains(out, "\x1b[31m") {
		t.Errorf("a board's pipe codes were expanded: %q", out)
	}
	if !strings.Contains(out, "Demoscene and |04code|07") {
		t.Errorf("description not shown as written: %q", out)
	}
	// Details of the selected (first) row.
	if !strings.Contains(out, "Board Details") || !strings.Contains(out, "ssh://sector7.example:2222") || !strings.Contains(out, "online now") {
		t.Errorf("details screen missing: %q", out)
	}
	if !strings.Contains(out, "aaaa1111bbbb2222") {
		t.Errorf("details should show the node id: %q", out)
	}
}

func TestCentralBoardsMarkersInCP437(t *testing.T) {
	// A CP437 caller must see the three freshness markers as distinct
	// CP437 glyphs: a rune with no CP437 byte is sent as '?', which made
	// online and recent boards look the same.
	fake := &centralFake{
		nets: []CentralNetworkInfo{{Network: "testnet", Enabled: true, SupportKnown: true, Supported: true}},
		dirs: map[string][]protocol.CentralDirectoryEntry{"testnet": {
			centralEntry("Old Haunt", "Back someday", "telnet://haunt.example:23", false, 30*24*time.Hour),
			centralEntry("Sector 7", "Demoscene", "ssh://sector7.example:2222", true, 0),
			centralEntry("The Vault", "Files since 1991", "telnet://vault.example:23", false, 2*24*time.Hour),
		}},
	}
	out := runCentralScreenMode(t, fake, "q", runV3NetCentralBoards, "", ansi.OutputModeCP437)
	// The cursor hide/show escapes carry a '?' of their own.
	text := strings.NewReplacer("\x1b[?25l", "", "\x1b[?25h", "").Replace(out)
	if strings.Contains(text, "?") {
		t.Errorf("a character with no CP437 byte was sent as '?': %q", out)
	}
	for _, want := range []string{"\xfe Sector 7", "\xfb The Vault", "\xfa Old Haunt", "\xfe\x1b[1;30m online", "\xfb\x1b[1;30m seen this week"} {
		if !strings.Contains(out, want) {
			t.Errorf("CP437 output missing %q: %q", want, out)
		}
	}
}

func TestCentralBoardsSearchFilters(t *testing.T) {
	fake := &centralFake{
		nets: []CentralNetworkInfo{{Network: "testnet", Enabled: true, SupportKnown: true, Supported: true}},
		dirs: map[string][]protocol.CentralDirectoryEntry{"testnet": {
			centralEntry("Sector 7", "Demoscene", "ssh://sector7.example:2222", true, 0),
			centralEntry("The Vault", "Files since 1991", "telnet://vault.example:23", true, 0),
		}},
	}
	out := runCentralScreen(t, fake, "/vault\rq", runV3NetCentralBoards, "")
	// Before the search both are drawn; after it only the match. Count the
	// item-row renderings: "Sector 7" appears once (first draw), "The
	// Vault" twice.
	if n := strings.Count(out, "Sector 7"); n != 1 {
		t.Errorf("Sector 7 drawn %d times, want 1 (filtered out after search)", n)
	}
	if n := strings.Count(out, "The Vault"); n < 2 {
		t.Errorf("The Vault drawn %d times, want at least 2", n)
	}
	if !strings.Contains(out, "/vault") {
		t.Errorf("active filter not shown in the help line: %q", out)
	}
}

func TestCentralBoardsEmptyAndErrors(t *testing.T) {
	fake := &centralFake{nets: []CentralNetworkInfo{{Network: "testnet", Enabled: true, SupportKnown: true, Supported: true}}}
	out := runCentralScreen(t, fake, "\r", runV3NetCentralBoards, "")
	if !strings.Contains(out, "No boards are listed yet") {
		t.Errorf("expected the empty notice, got %q", out)
	}
	fake.errs = map[string]error{"testnet": errors.New("hub unreachable")}
	out = runCentralScreen(t, fake, "\r", runV3NetCentralBoards, "")
	if !strings.Contains(out, "Could not fetch the directory") || !strings.Contains(out, "hub unreachable") {
		t.Errorf("expected the fetch error, got %q", out)
	}
	// A network the sysop did not enable is never fetched, even when named.
	fake = &centralFake{nets: []CentralNetworkInfo{{Network: "testnet", Enabled: false, SupportKnown: true, Supported: true}}}
	out = runCentralScreen(t, fake, "\r", runV3NetCentralBoards, "testnet")
	if !strings.Contains(out, "not available on any network") || fake.fetches != 0 {
		t.Errorf("disabled network fetched or offered: fetches=%d %q", fake.fetches, out)
	}
}

func TestCentralStatusScreen(t *testing.T) {
	fake := &centralFake{nets: []CentralNetworkInfo{
		{Network: "testnet", Enabled: true, Publishing: true, SupportKnown: true, Supported: true, Connected: true,
			LastPublished: time.Now(), DirectoryFetched: time.Now().Add(-3 * time.Minute), DirectoryCount: 4},
		{Network: "oldnet", Enabled: true, Publishing: true, SupportKnown: true, Supported: false, LastError: "publish: PUT returned 404"},
		{Network: "quiet", Enabled: false, Publishing: false, SupportKnown: false, Withdrawn: false},
	}}
	out := runCentralScreen(t, fake, "\r", runV3NetCentralStatus, "")
	for _, want := range []string{"testnet", "published", "connected", "served", "4", "3m ago",
		"oldnet", "no directory", "failed", "oldnet: publish: PUT returned 404",
		"quiet", "checking...", "off"} {
		if !strings.Contains(out, want) {
			t.Errorf("status screen lacks %q: %q", want, out)
		}
	}
}

func TestCentralPrefsSharesNothing(t *testing.T) {
	fake := &centralFake{nets: []CentralNetworkInfo{{Network: "testnet", Enabled: true, Supported: true, SupportKnown: true}}}
	out := runCentralScreen(t, fake, "\r", runV3NetCentralPrefs, "")
	if !strings.Contains(out, "Nothing about you is shared") {
		t.Errorf("expected the nothing-shared statement, got %q", out)
	}
	if strings.Count(out, "not offered here") != 3 {
		t.Errorf("expected every switch shown as not offered, got %q", out)
	}
	if fake.fetches != 0 {
		t.Error("preferences must not fetch anything")
	}
}

// The footer sits on the last row; a long search plus a status message
// must be cut to the screen, or the terminal scrolls and every later
// lightbar update lands a row off.
func TestCentralBoardsFooterStaysOnOneRow(t *testing.T) {
	fake := &centralFake{
		nets: []CentralNetworkInfo{{Network: "testnet", Enabled: true, SupportKnown: true, Supported: true}},
		dirs: map[string][]protocol.CentralDirectoryEntry{"testnet": {centralEntry("Sector 7 longnamehere", "x", "", true, 0)}},
	}
	// Search for a 16-character text that matches, then refresh (status
	// "Refreshed."), then quit.
	out := runCentralScreen(t, fake, "/sector 7 longnam\rrq", runV3NetCentralBoards, "")
	// Each footer write starts with a move to row 24 and runs to the next
	// cursor move or clear.
	next := regexp.MustCompile(`\x1b\[(\d+;\d+H|2J|\?25[hl])`)
	footers := 0
	for _, chunk := range strings.Split(out, "\x1b[24;1H")[1:] {
		row := chunk
		if m := next.FindStringIndex(row); m != nil {
			row = row[:m[0]]
		}
		footers++
		if w := ansi.VisibleLength(strings.ReplaceAll(row, "\x1b[2K", "")); w > 79 {
			t.Errorf("footer is %d columns wide: %q", w, row)
		}
	}
	if footers == 0 {
		t.Fatalf("no footer writes found: %q", out)
	}
	if !strings.Contains(out, "Refreshed.") && !strings.Contains(out, "/sector 7 longnam") {
		t.Errorf("footer content missing: %q", out)
	}
}
