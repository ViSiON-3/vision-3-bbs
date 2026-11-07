package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"golang.org/x/term"
)

// runSharingKonfig drives USERCONFIG with central as the board's V3Net
// Central, for keys.
func runSharingKonfig(t *testing.T, um *user.UserMgr, u *user.User, central V3NetCentralProvider, keys string) *testterm.Term {
	t.Helper()
	e := konfigTestExecutor(t)
	e.V3NetCentral = central
	screen := testterm.New(80, 24)
	sess := testterm.NewSession(screen, keys)
	t.Cleanup(func() {
		resetSessionIH(sess)
		sessionTermSizes.Delete(sess)
	})
	c := &cmdCtx{e: e, s: sess, terminal: term.NewTerminal(sess, ""), userManager: um, currentUser: u,
		nodeNumber: 1, outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24}
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	return screen
}

// Konfig offers V3Net Sharing only where a network offers Who's Online or
// Last Callers, so a board without them sees no change.
func TestKonfigV3NetSharingShownWhereOffered(t *testing.T) {
	featureOff := &centralFake{nets: []CentralNetworkInfo{{Network: "testnet", Enabled: true, SupportKnown: true, Supported: true}}}
	for name, tc := range map[string]struct {
		central V3NetCentralProvider
		shown   bool
	}{
		"no V3Net":    {nil, false},
		"feature off": {featureOff, false},
		"offered":     {&centralFake{nets: []CentralNetworkInfo{activityNet("testnet")}}, true},
	} {
		t.Run(name, func(t *testing.T) {
			um, u := newUserConfigTestUser(t)
			snap := runSharingKonfig(t, um, u, tc.central, "q").Snapshot()
			if got := strings.Contains(snap, "[R] V3Net Sharing"); got != tc.shown {
				t.Errorf("V3Net Sharing shown = %v, want %v:\n%s", got, tc.shown, snap)
			}
		})
	}
}

// R opens the V3Net Sharing screen; a switch turned on there is saved on
// the stored record, republished, and summed up back on Konfig.
func TestKonfigV3NetSharingOpensSharingScreen(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	fake := &centralFake{nets: []CentralNetworkInfo{activityNet("testnet")}}
	snap := runSharingKonfig(t, um, u, fake, "r1\rq\rq").Snapshot()
	stored, ok := um.GetUserByID(u.ID)
	if !ok || !stored.CentralPresence {
		t.Fatalf("Who's Online not saved: %+v", stored)
	}
	if fake.notifies != 1 {
		t.Errorf("change not republished: %d notifies", fake.notifies)
	}
	if !strings.Contains(snap, "[R] V3Net Sharing") || !strings.Contains(snap, "Online") {
		t.Errorf("Konfig not back, or value not updated:\n%s", snap)
	}
}
