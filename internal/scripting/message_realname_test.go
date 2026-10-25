package scripting

import (
	"strconv"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// TestPostSignsWithRealNameWhereRequired checks that v3.message.post signs
// with the caller's real name in a real_name_only area, as the message
// reader does, and with the handle elsewhere.
func TestPostSignsWithRealNameWhereRequired(t *testing.T) {
	mm, err := message.NewMessageManager(t.TempDir(), t.TempDir(), "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	realID, err := mm.AddArea(message.MessageArea{Tag: "FIDOECHO", Name: "Echo", AreaType: "echomail", EchoTag: "FIDOECHO", RealNameOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	handleID, err := mm.AddArea(message.MessageArea{Tag: "GENERAL", Name: "General", AreaType: "local"})
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, harnessOpts{providers: &Providers{MessageMgr: mm}})

	for _, tt := range []struct {
		areaID int
		want   string
	}{
		{realID, "Test User"},
		{handleID, "Tester"},
	} {
		h.eval(`v3.message.post(` + strconv.Itoa(tt.areaID) + `, {subject: "Hi", body: "Hello"})`)
		msg, err := mm.GetMessage(tt.areaID, 1)
		if err != nil {
			t.Fatalf("area %d: GetMessage: %v", tt.areaID, err)
		}
		if msg.From != tt.want {
			t.Errorf("area %d: From = %q, want %q", tt.areaID, msg.From, tt.want)
		}
	}
}
