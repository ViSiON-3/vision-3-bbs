package menu

import (
	"strings"
	"testing"
)

// chatScreenCodesMsg holds every screen code. Each is followed by a letter, so
// a code that runs is seen as its expansion between two letters.
const chatScreenCodesMsg = "a|CLb|CRc|DEd|Pe|PPf"

// chatScreenCodeBytes are what ReplacePipeCodes writes for the screen codes.
var chatScreenCodeBytes = []string{"\x1b[2J", "b\r\nc", "\x1b[K", "\x1b[s", "\x1b[u"}

// chatCheckNoScreenCodes fails if out shows any screen code having run.
func chatCheckNoScreenCodes(t *testing.T, what, out string) {
	t.Helper()
	for _, code := range chatScreenCodeBytes {
		if strings.Contains(out, code) {
			t.Errorf("%s ran a screen code: output holds %q", what, code)
		}
	}
}

// One caller's room message, through the local service to another caller's
// running chat session.
func TestChatRoomMessageCannotRunScreenCodes(t *testing.T) {
	env, live, _ := startToastChat(t)
	bob := chatcovLocal(env, "Bob")
	if _, _, err := bob.Join("lobby"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bob.Leave("lobby") })

	mark := len(live.s.output())
	if err := bob.Post("lobby", chatScreenCodesMsg+" ENDMARK"); err != nil {
		t.Fatal(err)
	}
	toastWaitFor(t, "the room line", func() bool { return strings.Contains(live.s.output()[mark:], "ENDMARK") })
	got := live.s.output()[mark:]
	t.Logf("other caller received %q", got)
	chatCheckNoScreenCodes(t, "a room message", got)
	if !strings.Contains(got, chatScreenCodesMsg) {
		t.Errorf("the codes are not shown as typed: %q", got)
	}
	live.typeKeys("/q\r")
}
