package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// newNodelistMsgManager is a message manager with an fsxnet netmail area and
// a local area.
func newNodelistMsgManager(t *testing.T) (mm *message.MessageManager, netmailID, localID int) {
	t.Helper()
	mm, err := message.NewMessageManager(t.TempDir(), t.TempDir(), "TestBBS", nil)
	if err != nil {
		t.Fatalf("NewMessageManager: %v", err)
	}
	if netmailID, err = mm.AddArea(message.MessageArea{Tag: "FSX_NET", Name: "fsxNet Netmail", AreaType: "netmail", Network: "fsxnet"}); err != nil {
		t.Fatal(err)
	}
	if localID, err = mm.AddArea(message.MessageArea{Tag: "GENERAL", Name: "General", AreaType: "local"}); err != nil {
		t.Fatal(err)
	}
	return mm, netmailID, localID
}

func TestMsgHeaderSystemNames(t *testing.T) {
	e := nodelistExecutor(t)
	mm, netmailID, localID := newNodelistMsgManager(t)
	msg := &message.DisplayMessage{From: "Paul", To: "Jane", OrigAddr: "21:1/100", DestAddr: "21:4/158.1"}

	subs := buildMsgSubstitutions(msg, "FSX_NET", 1, 1, false, 0, "fsxNet", "Netmail", mm, netmailID, nil, 1, nil, e.Nodelists)
	// The destination is a point; its node's name is used.
	if subs['Y'] != "Risa HUB" || subs['R'] != "Example BBS" {
		t.Errorf("@Y@ = %q, @R@ = %q; want Risa HUB and Example BBS", subs['Y'], subs['R'])
	}

	// Unlisted addresses, a local area, and no nodelists all leave them blank.
	unlisted := &message.DisplayMessage{From: "Joe", To: "All", OrigAddr: "21:4/999"}
	cases := map[string]map[byte]string{
		"unlisted":     buildMsgSubstitutions(unlisted, "FSX_NET", 1, 1, false, 0, "", "", mm, netmailID, nil, 1, nil, e.Nodelists),
		"local area":   buildMsgSubstitutions(msg, "GENERAL", 1, 1, false, 0, "", "", mm, localID, nil, 1, nil, e.Nodelists),
		"no nodelists": buildMsgSubstitutions(msg, "FSX_NET", 1, 1, false, 0, "", "", mm, netmailID, nil, 1, nil, nil),
	}
	for name, subs := range cases {
		if subs['Y'] != "" || subs['R'] != "" {
			t.Errorf("%s: @Y@ = %q, @R@ = %q; want both blank", name, subs['Y'], subs['R'])
		}
	}
}

// @Y@ and @R@ are recognised by the header template processor, with the
// usual width modifiers.
func TestMsgHeaderSystemNamePlaceholders(t *testing.T) {
	subs := map[byte]string{'F': "Paul (21:1/100)", 'Y': "Risa HUB", 'R': "Example BBS"}
	got := string(processTemplate([]byte("@F@ of @Y@ to [@R:14@]"), subs, buildAutoWidths(subs, 1, 80, false), false))
	if want := "Paul (21:1/100) of Risa HUB to [Example BBS   ]"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// In an optional group, a blank system name blanks the group.
	subs['Y'] = ""
	got = string(processTemplate([]byte("@F@|{ of @Y@|}."), subs, buildAutoWidths(subs, 1, 80, false), false))
	if !strings.HasPrefix(got, "Paul (21:1/100)") || strings.Contains(got, "of") {
		t.Errorf("blank @Y@ in a group: got %q", got)
	}
}

// @I@, the V3Net network name, is replaced in headers like the other codes;
// it was filled in but never matched.
func TestMsgHeaderV3NetNetworkPlaceholder(t *testing.T) {
	subs := map[byte]string{'B': "FEL_GEN", 'I': "felonynet"}
	got := string(processTemplate([]byte("@B@ [@I:10@]"), subs, buildAutoWidths(subs, 1, 80, false), false))
	if want := "FEL_GEN [felonynet ]"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// Blank outside V3Net areas: an optional group around it goes blank.
	subs['I'] = ""
	got = string(processTemplate([]byte("@B@|{ via @I@|}"), subs, buildAutoWidths(subs, 1, 80, false), false))
	if strings.Contains(got, "via") {
		t.Errorf("blank @I@ in a group: got %q", got)
	}
}
