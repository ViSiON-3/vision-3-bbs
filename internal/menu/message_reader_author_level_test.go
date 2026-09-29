package menu

import (
	"strings"
	"testing"
)

// The header's @L@ is the message author's access level, not the reader's
// (#487). Header style 2 prints it as "(who is level N)": for a local author
// that shows the author's level, and for a From that is not a local handle
// (remote, netmail, real-name or anonymous posts) the whole group is blanked
// rather than showing an empty or borrowed level.
func TestMessageReaderHeaderShowsAuthorLevel(t *testing.T) {
	env := newMsgEnv(t) // header style 2; Caller is level 30, Sysop 255
	env.postMsgs(generalAreaID,
		testMsg{from: "Sysop", to: "All", subject: "local-author"},
		testMsg{from: "Remote Guy", to: "All", subject: "remote-author"},
	)

	// Read message 1 (local author), N to message 2 (remote author), quit.
	r := env.runCmd("READMSGS", env.caller, "", "NQ")
	if r.err != nil {
		t.Fatalf("READMSGS: %v", r.err)
	}
	txt := r.text()
	if !r.has("local-author", "remote-author") {
		t.Fatalf("both messages should be shown; output:\n%s", txt)
	}

	// Style 2 prints the subject line above "Posted by", so each message's
	// level group follows its subject.
	local, remote, _ := strings.Cut(txt, "remote-author")
	if !strings.Contains(local, "who is level 255") {
		t.Errorf("local author's header should show the author's level 255; output:\n%s", local)
	}
	if strings.Contains(txt, "who is level 30") {
		t.Errorf("a header showed the reader's level 30; output:\n%s", txt)
	}
	if !strings.Contains(remote, "Posted by Remote Guy") {
		t.Fatalf("remote message header missing; output:\n%s", remote)
	}
	if strings.Contains(remote, "who is level") {
		t.Errorf("remote author's header should blank the level group; output:\n%s", remote)
	}
}
