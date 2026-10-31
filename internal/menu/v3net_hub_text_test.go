package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// hostileV3NetText holds a screen code, raw CSI and OSC sequences and the 8-bit
// form of CSI, as a hub or another node could send them.
const hostileV3NetText = "Evil|CL\x1b[2J\x1b]0;owned\x07\u009b2J"

// assertV3NetTextSafe fails if hostileV3NetText acted on the screen in out.
func assertV3NetTextSafe(t *testing.T, out string) {
	t.Helper()
	for _, bad := range []string{ansi.ClearScreen(), "\x1b[2J", "\x1b]0;", "\x07", "\u009b"} {
		if strings.Contains(out, bad) {
			t.Errorf("hub text reached the screen as %q: %q", bad, out)
		}
	}
	if !strings.Contains(out, "Evil|CL") {
		t.Errorf("hub text should show its pipes as text: %q", out)
	}
}

// A hub's area name travels from the NAL through the queued notice to the
// sysop's login screen.
func TestSysopNoticeOfferDoesNotRunHubScreenCodes(t *testing.T) {
	e, um := newAreaFixture(t)
	mm, err := message.NewMessageManager(t.TempDir(), t.TempDir(), "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	e.MessageMgr = mm

	e.NoteV3NetNAL(um, "felonynet", nalWith("fel.a"), "TEST")
	n := nalWith("fel.a", "fel.evil")
	n.Areas[1].Name = hostileV3NetText
	if queued := e.NoteV3NetNAL(um, "felonynet", n, "TEST"); queued == 0 {
		t.Fatal("no offer queued")
	}
	e.V3NetStatus = &fakeV3NetStatus{network: "felonynet", hubURL: "https://hub.example", nal: n}

	assertV3NetTextSafe(t, runNoticeScreen(t, e, 255, "n\r"))
}
