package menu

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// TestGetHeaderTypeSelection drives GETHEADERTYPE: SPACE saves the
// highlighted style, Down moves the highlight, a digit jumps to that style,
// Enter previews and Y keeps it, N at the preview returns to the list, and
// Q leaves the saved style alone.
func TestGetHeaderTypeSelection(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  int
	}{
		{"space picks the first", " ", 1},
		{"down then space picks the second", "\x1b[B\x1b[B\x1b[A ", 2},
		{"digit hotkey then space", "7 ", 7},
		{"preview and accept", "\x1b[B\x1b[B\rY", 3},
		{"preview, decline, then pick", "\rN\x1b[B ", 2},
		{"quit keeps the old style", "\x1b[B\x1b[Bq", 5},
	}
	env := newMsgEnv(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := env.sub(t)
			env.caller.MsgHdr = 5
			if err := env.um.UpdateUser(env.caller); err != nil {
				t.Fatal(err)
			}
			r := env.runCmd("GETHEADERTYPE", env.caller, "", tc.input)
			if r.err != nil {
				t.Fatalf("GETHEADERTYPE: %v", r.err)
			}
			if got := env.mustDiskUser(2).MsgHdr; got != tc.want {
				t.Errorf("saved MsgHdr = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestGetHeaderTypePreviewShowsSampleAndCP437 checks the preview renders the
// template with sample data for the caller, in CP437 as well as UTF-8.
func TestGetHeaderTypePreviewShowsSampleAndCP437(t *testing.T) {
	env := newMsgEnv(t)
	for _, mode := range []struct {
		name string
		cp   bool
	}{{"utf8", false}, {"cp437", true}} {
		t.Run(mode.name, func(t *testing.T) {
			env := env.sub(t)
			if mode.cp {
				env.outputMode = ansi.OutputModeCP437
			}
			r := env.runCmd("GETHEADERTYPE", env.caller, "", "2\rNq")
			if !r.has("ViSiON/3 Rocks!", "Caller") {
				t.Errorf("preview missing sample data; output:\n%s", r.text())
			}
			if !r.has("Pick this header?") {
				t.Errorf("preview should ask to pick; output:\n%s", r.text())
			}
		})
	}
}

// TestGetHeaderTypeNoUser checks the picker does nothing without a caller.
func TestGetHeaderTypeNoUser(t *testing.T) {
	env := newMsgEnv(t)
	if r := env.runCmd("GETHEADERTYPE", nil, "", " "); r.raw != "" || r.err != nil {
		t.Errorf("anonymous GETHEADERTYPE wrote %q err=%v", r.text(), r.err)
	}
}

// TestFindMessageByMSGID checks an echomail post is found by the MSGID it
// was given and an unknown MSGID is not.
func TestFindMessageByMSGID(t *testing.T) {
	env := newMsgEnv(t)
	areaID, err := env.e.MessageMgr.AddArea(message.MessageArea{
		Tag: "ECHO", Name: "Echo", AreaType: "echomail", EchoTag: "ECHO",
		OriginAddr: "21:1/100", ConferenceID: 1, ACSRead: "s10", ACSWrite: "s10",
	})
	if err != nil {
		t.Fatalf("AddArea: %v", err)
	}
	env.postMsgs(areaID, testMsg{from: "Sysop", to: "All", subject: "one"}, testMsg{from: "Sysop", to: "All", subject: "two"})
	id := env.mustMsg(areaID, 2).MsgID
	if id == "" {
		t.Fatal("echomail post has no MSGID")
	}
	if got := findMessageByMSGID(env.e.MessageMgr, areaID, id); got != 2 {
		t.Errorf("findMessageByMSGID(%q) = %d, want 2", id, got)
	}
	if got := findMessageByMSGID(env.e.MessageMgr, areaID, "21:1/100 deadbeef"); got != 0 {
		t.Errorf("unknown MSGID found at %d", got)
	}
}
