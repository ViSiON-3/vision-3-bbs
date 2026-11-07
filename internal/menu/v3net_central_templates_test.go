package menu

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// templateFake is a provider with both feeds on testnet, for the stock
// WHONET and LASTNET templates.
func templateFake() *centralFake {
	return &centralFake{
		nets: []CentralNetworkInfo{activityNet("testnet")},
		acts: map[string]map[string][]protocol.CentralActivityBoard{"testnet": {
			protocol.CentralKindPresence: {activityBoardOf("bbbb", "Beta BBS", protocol.CentralKindPresence,
				protocol.CentralActivityEntry{Handle: "Alice", Session: "s1", Availability: protocol.CentralAvailable},
				protocol.CentralActivityEntry{Handle: "Alice", Session: "s2"})},
			protocol.CentralKindLastCallers: {activityBoardOf("bbbb", "Beta BBS", protocol.CentralKindLastCallers,
				protocol.CentralActivityEntry{Handle: "Bob", At: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), Until: time.Now().UTC().Add(-48 * time.Minute).Format(time.RFC3339)})},
		}},
	}
}

// The stock menu set lays network Who's Online out from WHONET: its rows,
// its footer text, and no token left unfilled.
func TestCentralWhoUsesTemplates(t *testing.T) {
	env := newMenuEnv(t)
	env.e.V3NetCentral = templateFake()
	r := env.runCmd("V3NETCENTRALWHO", env.caller, "", "\r")
	if r.err != nil {
		t.Fatal(r.err)
	}
	if !r.has("V3Net Central: Who's Online", "Alice", "Beta BBS", "testnet", "available", "(2 nodes)", "Only callers who turned on") {
		t.Errorf("output:\n%s", r.text())
	}
	if strings.Count(r.text(), "Alice") != 1 || strings.Contains(r.text(), "@") {
		t.Errorf("rows not grouped, or a token left unfilled:\n%s", r.text())
	}
}

// LASTCALLERS --all lays the merged list out from LASTNET, with each row's
// From filled in as local or the network's name.
func TestLastCallersAllUsesTemplates(t *testing.T) {
	env := newMenuEnv(t)
	seedLastCallers(t, env)
	env.e.V3NetCentral = templateFake()
	r := env.runCmd("LASTCALLERS", env.caller, "20 --all", "q")
	if r.err != nil {
		t.Fatal(r.err)
	}
	if !r.has("V3Net Central", "From", "Called", "local = this board", "testnet", "Bob", "Beta BBS", "Alpha", "12", "Hide this board's callers") {
		t.Errorf("output:\n%s", r.text())
	}
	if !strings.Contains(r.text(), "local      Alpha") || strings.Contains(r.text(), "@") || r.has("Sysop") {
		t.Errorf("From column, an unfilled token, or a hidden login:\n%s", r.text())
	}
}

// The templates are UTF-8; a CP437 caller gets them converted, not raw.
func TestCentralTemplatesInCP437(t *testing.T) {
	env := newMenuEnv(t)
	env.outputMode = ansi.OutputModeCP437
	env.e.V3NetCentral = templateFake()
	r := env.runCmd("V3NETCENTRALWHO", env.caller, "", "\r")
	if strings.Contains(r.raw, "─") || !strings.Contains(r.raw, "\xc4") {
		t.Error("the rule was not converted to CP437")
	}
}

// Another board's text is filled in after pipe codes are expanded, so it is
// drawn as written and cannot colour or clear the caller's screen.
func TestCentralTemplateValuesAreText(t *testing.T) {
	tpl := centralTemplate{top: "", mid: string(ansi.ReplacePipeCodes([]byte("|15@UN:12@|07 @BB@"))), bot: ""}
	out := renderWhoTemplate(tpl, []centralWhoRow{{handle: "|12Red|CL", board: "Evil\x1b[2J", network: "n"}}, 1, nil)
	if !strings.Contains(out, "|12Red|CL") || strings.Contains(out, "\x1b[2J") || strings.Contains(out, "\x1b[1;31m") {
		t.Errorf("value was interpreted: %q", out)
	}
	if !strings.Contains(out, "|12Red|CL   ") {
		t.Errorf("width not applied: %q", out)
	}
}

// Rows are trimmed so the template, footer and pause fit the screen.
func TestCentralTemplateRowsThatFit(t *testing.T) {
	tpl := centralTemplate{top: "a\r\nb\r\nc\r\n", mid: "row", bot: "z\r\n"}
	if got := tpl.rowsThatFit(24, 3); got != 16 {
		t.Errorf("rows %d, want 16", got)
	}
	if got := tpl.rowsThatFit(4, 3); got != 1 {
		t.Errorf("rows %d on a tiny screen, want at least 1", got)
	}
}
