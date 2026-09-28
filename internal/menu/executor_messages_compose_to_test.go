package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestComposeRecipientKindFor(t *testing.T) {
	tests := []struct {
		area message.MessageArea
		want composeRecipientKind
	}{
		{message.MessageArea{Tag: "PRIVMAIL", AreaType: "local"}, recipientPrivate},
		{message.MessageArea{Tag: "privmail"}, recipientPrivate},
		{message.MessageArea{Tag: "FSX_NET", AreaType: "netmail"}, recipientNetmail},
		{message.MessageArea{Tag: "DIRECT", AreaType: "direct"}, recipientNetmail},
		{message.MessageArea{Tag: "FSX_GEN", AreaType: "echomail"}, recipientPublic},
		{message.MessageArea{Tag: "GENERAL", AreaType: "local"}, recipientPublic},
	}
	for _, tt := range tests {
		if got := composeRecipientKindFor(&tt.area); got != tt.want {
			t.Errorf("%s (%s): kind = %d, want %d", tt.area.Tag, tt.area.AreaType, got, tt.want)
		}
	}
}

// runRecipientPrompt drives promptComposeRecipient with scripted keystrokes.
func runRecipientPrompt(t *testing.T, area message.MessageArea, input string) (to, name string, aborted bool, out string) {
	t.Helper()
	um := user.NewUserMgrForTest(
		&user.User{ID: 1, Handle: "Bob", AccessLevel: 10},
		&user.User{ID: 2, Handle: "Gone", AccessLevel: 10, DeletedUser: true},
	)
	ts := newTestSession(input)
	e := &MenuExecutor{}
	to, name, aborted, err := e.promptComposeRecipient(ts, newTestTerminal(ts), um, &area, ansi.OutputModeUTF8, 1, 80, 24)
	if err != nil {
		t.Fatalf("promptComposeRecipient(%q): %v", input, err)
	}
	return to, name, aborted, ts.output()
}

func TestPromptComposeRecipientPublicDefaultsToAll(t *testing.T) {
	to, name, aborted, _ := runRecipientPrompt(t, message.MessageArea{Tag: "GENERAL", AreaType: "local"}, "\r")
	if aborted || to != "All" || name != "All" {
		t.Errorf("got to=%q name=%q aborted=%v, want All", to, name, aborted)
	}
}

func TestPromptComposeRecipientPrivateRequiresRealUser(t *testing.T) {
	area := message.MessageArea{Tag: "PRIVMAIL", AreaType: "local"}

	// Unknown and deleted users are refused and asked again; the stored name
	// is the recipient's own spelling of their handle.
	to, name, aborted, out := runRecipientPrompt(t, area, "nobody\rgone\rbob\r")
	if aborted || to != "Bob" || name != "Bob" {
		t.Fatalf("got to=%q name=%q aborted=%v, want Bob", to, name, aborted)
	}
	if strings.Count(out, "not found") != 2 {
		t.Errorf("want two not-found notices, output %q", out)
	}
	if strings.Contains(out, "All") {
		t.Errorf("private To: was pre-filled with All: %q", out)
	}

	// Blank abandons the post rather than becoming "All".
	if _, _, aborted, _ := runRecipientPrompt(t, area, "\r"); !aborted {
		t.Error("blank private recipient did not abort")
	}
}

func TestPromptComposeRecipientNetmailRequiresAddress(t *testing.T) {
	area := message.MessageArea{Tag: "NETMAIL", AreaType: "netmail"}

	to, name, aborted, _ := runRecipientPrompt(t, area, "Joe Sysop@21:1/100\r")
	if aborted || to != "Joe Sysop@21:1/100" || name != "Joe Sysop" {
		t.Errorf("inline address: to=%q name=%q aborted=%v", to, name, aborted)
	}

	// No address in To: ask for one, refusing anything that does not parse.
	to, name, aborted, out := runRecipientPrompt(t, area, "Joe Sysop\rnowhere\r21:1/100\r")
	if aborted || to != "Joe Sysop@21:1/100" || name != "Joe Sysop" {
		t.Errorf("prompted address: to=%q name=%q aborted=%v", to, name, aborted)
	}
	if !strings.Contains(out, "not an FTN address") {
		t.Errorf("bad address was not reported, output %q", out)
	}

	if _, _, aborted, _ := runRecipientPrompt(t, area, "\r"); !aborted {
		t.Error("blank netmail recipient did not abort")
	}
	if _, _, aborted, _ := runRecipientPrompt(t, area, "Joe\r\r"); !aborted {
		t.Error("blank netmail address did not abort")
	}
}

func TestPromptComposeRecipientNetmailAsksAboutOtherZones(t *testing.T) {
	area := message.MessageArea{Tag: "FSX_NET", AreaType: "netmail", Network: "fsxnet", OriginAddr: "21:4/158"}

	// Same zone as the network: no question.
	to, _, aborted, out := runRecipientPrompt(t, area, "Joe@21:1/100\r")
	if aborted || to != "Joe@21:1/100" || strings.Contains(out, "Send anyway") {
		t.Errorf("same zone: to=%q aborted=%v asked=%v", to, aborted, strings.Contains(out, "Send anyway"))
	}

	// Another zone, answered Yes: kept as entered.
	to, _, aborted, out = runRecipientPrompt(t, area, "Joe@1:234/567\ry")
	if aborted || to != "Joe@1:234/567" {
		t.Errorf("other zone, yes: to=%q aborted=%v", to, aborted)
	}
	if !strings.Contains(out, "1:234/567 is not in fsxnet (zone 21)") {
		t.Errorf("zone question missing, output %q", out)
	}

	// Another zone, answered No: the address is asked for again.
	to, name, aborted, _ := runRecipientPrompt(t, area, "Joe@1:234/567\rn21:1/100\r")
	if aborted || to != "Joe@21:1/100" || name != "Joe" {
		t.Errorf("other zone, no: to=%q name=%q aborted=%v", to, name, aborted)
	}

	// No origin address on the area: nothing to compare with, so no question.
	area.OriginAddr = ""
	if to, _, _, out := runRecipientPrompt(t, area, "Joe@1:234/567\r"); to != "Joe@1:234/567" || strings.Contains(out, "Send anyway") {
		t.Errorf("no origin: to=%q, asked=%v", to, strings.Contains(out, "Send anyway"))
	}
}
