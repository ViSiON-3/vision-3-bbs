package scripting

import (
	"context"
	"strconv"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// TestMessageGetHidesOthersPrivateMail pins #465 for the script API:
// v3.message.get returns null for a private message the running user neither
// sent nor received, and the message itself for its recipient.
func TestMessageGetHidesOthersPrivateMail(t *testing.T) {
	mm, err := message.NewMessageManager(t.TempDir(), t.TempDir(), "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	areaID, err := mm.AddArea(message.MessageArea{Tag: "GENERAL", Name: "General", AreaType: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mm.AddPrivateMessage(areaID, "Alice", "Bob", "Secret", "SECRET-BODY", ""); err != nil {
		t.Fatal(err)
	}

	get := func(handle string) string {
		t.Helper()
		sess := newInterruptibleSession("")
		eng := NewEngine(context.Background(), &SessionContext{
			Session: sess, OutputMode: ansi.OutputModeUTF8, ScreenWidth: 80, ScreenHeight: 24,
			UserHandle: handle,
		}, ScriptConfig{}, &Providers{MessageMgr: mm})
		t.Cleanup(func() {
			sess.closeInterrupt()
			eng.Close()
		})
		v, err := eng.vm.RunString(`JSON.stringify(v3.message.get(` + strconv.Itoa(areaID) + `, 1))`)
		if err != nil {
			t.Fatalf("script: %v", err)
		}
		return v.String()
	}

	if got := get("Carol"); got != "null" {
		t.Errorf("Carol's script read another user's private mail: %s", got)
	}
	if got := get("Bob"); got == "null" {
		t.Error("Bob's script could not read his own private mail")
	}
}
