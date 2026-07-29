package menu

import "testing"

func TestMatchCommand(t *testing.T) {
	allow := func(acs string, keys string) bool { return true }
	cmds := []CommandRecord{
		{Keys: "M", Command: "GOTO:MAIL"},
		{Keys: "G", Command: "LOGOFF"},
	}
	action, _, matched := matchCommand(cmds, "M", allow)
	if !matched || action != "GOTO:MAIL" {
		t.Fatalf("got (%q, %v), want (GOTO:MAIL, true)", action, matched)
	}
	_, _, matched = matchCommand(cmds, "ZZ", allow)
	if matched {
		t.Fatal("ZZ should not match")
	}
	deny := func(string, string) bool { return false }
	if _, _, m := matchCommand(cmds, "M", deny); m {
		t.Fatal("ACS-denied command must not match")
	}
}

func TestMatchCommandGlobalHangup(t *testing.T) {
	// /G matches unconditionally, without consulting hasAccess at all,
	// and without needing any command in the list.
	denyAll := func(string, string) bool { return false }
	action, nodeActivity, matched := matchCommand(nil, "/G", denyAll)
	if !matched || action != "RUN:IMMEDIATELOGOFF" {
		t.Fatalf("got (%q, %q, %v), want (RUN:IMMEDIATELOGOFF, \"\", true)", action, nodeActivity, matched)
	}
	if nodeActivity != "" {
		t.Fatalf("got nodeActivity %q, want empty", nodeActivity)
	}
}

func TestMatchCommandEnterDefault(t *testing.T) {
	// ^M matches Enter (empty input) as the menu's default command.
	allow := func(string, string) bool { return true }
	cmds := []CommandRecord{
		{Keys: "^M", Command: "GOTO:MAIN", NodeActivity: "Main Menu"},
	}
	action, nodeActivity, matched := matchCommand(cmds, "", allow)
	if !matched || action != "GOTO:MAIN" || nodeActivity != "Main Menu" {
		t.Fatalf("got (%q, %q, %v), want (GOTO:MAIN, Main Menu, true)", action, nodeActivity, matched)
	}
	// ^M must not match when the user actually typed something.
	if _, _, m := matchCommand(cmds, "X", allow); m {
		t.Fatal("^M should not match non-empty input")
	}
}

func TestMatchCommandNumericWildcard(t *testing.T) {
	// ## matches any all-numeric input, appending it as args to the command.
	allow := func(string, string) bool { return true }
	cmds := []CommandRecord{
		{Keys: "##", Command: "GOTO:READMSG"},
	}
	action, _, matched := matchCommand(cmds, "42", allow)
	if !matched || action != "GOTO:READMSG 42" {
		t.Fatalf("got (%q, %v), want (\"GOTO:READMSG 42\", true)", action, matched)
	}
	// Non-numeric input must not match ##.
	if _, _, m := matchCommand(cmds, "4A", allow); m {
		t.Fatal("## should not match non-numeric input")
	}
}

func TestMatchCommandHasAccessReceivesKeys(t *testing.T) {
	// hasAccess must be called with the command's own Keys string (not just
	// its ACS), so a closure built by the caller can log/attribute denials
	// per-command (see executor_run.go's construction of this closure,
	// which logs the "keys" field on ACS denial).
	var gotACS, gotKeys string
	calls := 0
	spy := func(acs string, keys string) bool {
		calls++
		gotACS, gotKeys = acs, keys
		return false
	}
	cmds := []CommandRecord{
		{Keys: "Q W", Command: "GOTO:QUIT", ACS: "s10"},
	}
	if _, _, matched := matchCommand(cmds, "Q", spy); matched {
		t.Fatal("hasAccess denied the command; it must not match")
	}
	if calls != 1 {
		t.Fatalf("hasAccess called %d times, want 1", calls)
	}
	if gotACS != "s10" || gotKeys != "Q W" {
		t.Fatalf("hasAccess called with (%q, %q), want (\"s10\", \"Q W\")", gotACS, gotKeys)
	}
}
