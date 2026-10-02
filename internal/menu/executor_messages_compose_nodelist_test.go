package menu

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// nodelistExecutor is an executor with a compiled fsxnet nodelist.
// runNodelistPrompt is runRecipientPromptWith with the colour codes taken out
// of the output.
func runNodelistPrompt(t *testing.T, e *MenuExecutor, area message.MessageArea, input string) (to, name string, aborted bool, out string) {
	t.Helper()
	to, name, aborted, out = runRecipientPromptWith(t, e, area, input)
	return to, name, aborted, ansiEscapes.ReplaceAllString(out, "")
}

var ansiEscapes = regexp.MustCompile(`\x1b\[[0-9;? ]*[A-Za-z]`)

func nodelistExecutor(t *testing.T) *MenuExecutor {
	t.Helper()
	dir := t.TempDir()
	list := &ftn.CompiledNodelist{
		Network: "fsxnet",
		Date:    time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC),
		Nodes: []ftn.CompiledNode{
			{Address: "21:21/0", Status: "Zone", Name: "fsxNet ZC"},
			{Address: "21:1/100", Status: "Hub", Name: "Risa HUB", Location: "Dunedin NZL", Sysop: "Paul Hayton"},
			{Address: "21:4/158", Name: "Example BBS", Location: "Springfield USA", Sysop: "Jane Sysop"},
			{Address: "21:4/160", Status: "Down", Name: "Gone BBS"},
			{Address: "21:4/161", Status: "Hold", Name: "Sleepy BBS"},
			{Address: "21:4/162", Status: "Pvt", Name: "Hidden BBS"},
		},
	}
	if _, _, err := ftn.SaveCompiledNodelist(dir, "fsxnet", list, false); err != nil {
		t.Fatal(err)
	}
	return &MenuExecutor{Nodelists: ftn.NewNodelistIndex(dir)}
}

var fsxNetmail = message.MessageArea{Tag: "FSX_NET", AreaType: "netmail", Network: "fsxnet", OriginAddr: "21:4/158.1"}

func TestNetmailShowsTheListedSystem(t *testing.T) {
	e := nodelistExecutor(t)

	to, _, aborted, out := runNodelistPrompt(t, e, fsxNetmail, "Paul@21:1/100\r")
	if aborted || to != "Paul@21:1/100" {
		t.Fatalf("to=%q aborted=%v", to, aborted)
	}
	if !strings.Contains(out, "Sending to Risa HUB, Dunedin NZL (Paul Hayton)") || strings.Contains(out, "Send anyway") {
		t.Errorf("output %q", out)
	}

	// A point is not listed; its node is.
	to, _, _, out = runNodelistPrompt(t, e, fsxNetmail, "Jane@21:4/158.2\r")
	if to != "Jane@21:4/158.2" || !strings.Contains(out, "Sending to a point of Example BBS") {
		t.Errorf("point: to=%q output %q", to, out)
	}
}

func TestNetmailAsksAboutUnlistedAddresses(t *testing.T) {
	e := nodelistExecutor(t)

	// Yes: sent as entered.
	to, _, aborted, out := runNodelistPrompt(t, e, fsxNetmail, "Joe@21:4/999\ry")
	if aborted || to != "Joe@21:4/999" {
		t.Errorf("yes: to=%q aborted=%v", to, aborted)
	}
	if !strings.Contains(out, "21:4/999 is not in the fsxnet nodelist of 2026-10-02. Send anyway?") {
		t.Errorf("question missing, output %q", out)
	}

	// No: the address is asked for again.
	to, name, aborted, _ := runNodelistPrompt(t, e, fsxNetmail, "Joe@21:4/999\rn21:4/158\r")
	if aborted || to != "Joe@21:4/158" || name != "Joe" {
		t.Errorf("no: to=%q name=%q aborted=%v", to, name, aborted)
	}

	// A point of an unlisted node names the node.
	_, _, _, out = runNodelistPrompt(t, e, fsxNetmail, "Joe@21:4/999.1\ry")
	if !strings.Contains(out, "21:4/999.1 (the node 21:4/999) is not in the fsxnet nodelist") {
		t.Errorf("point of unlisted node: output %q", out)
	}
}

func TestNetmailNodelistStatus(t *testing.T) {
	e := nodelistExecutor(t)

	// Down is asked about.
	to, _, _, out := runNodelistPrompt(t, e, fsxNetmail, "Op@21:4/160\rn21:4/158\r")
	if to != "Op@21:4/158" || !strings.Contains(out, "21:4/160 is listed as Down in the fsxnet nodelist. Send anyway?") {
		t.Errorf("down: to=%q output %q", to, out)
	}

	// Hold and Pvt are noted, not asked about.
	for addr, note := range map[string]string{"21:4/161": "Listed as Hold", "21:4/162": "Listed as Private"} {
		to, _, _, out := runNodelistPrompt(t, e, fsxNetmail, "Op@"+addr+"\r")
		if to != "Op@"+addr || !strings.Contains(out, note) || strings.Contains(out, "Send anyway") {
			t.Errorf("%s: to=%q output %q", addr, to, out)
		}
	}
}

// Without a compiled list, or for an address in a zone the list does not
// cover, nothing is shown or asked.
func TestNetmailWithoutANodelistAsksNothing(t *testing.T) {
	area := fsxNetmail
	area.Network = "tqwnet"
	area.OriginAddr = ""
	to, _, _, out := runNodelistPrompt(t, nodelistExecutor(t), area, "Joe@1337:3/999\r")
	if to != "Joe@1337:3/999" || strings.Contains(out, "Send anyway") || strings.Contains(out, "Sending to") {
		t.Errorf("no list: to=%q output %q", to, out)
	}

	// Another zone, confirmed at the zone question: not asked twice.
	to, _, _, out = runNodelistPrompt(t, nodelistExecutor(t), fsxNetmail, "Joe@1:234/567\ry")
	if to != "Joe@1:234/567" || strings.Count(out, "Send anyway") != 1 {
		t.Errorf("other zone: to=%q output %q", to, out)
	}
}
