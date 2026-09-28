package menu

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// overCap returns text whose byte-offset cut at limit lands inside a
// two-byte "é": one ASCII byte, then more "é"s than the limit allows. A
// rune-counted cap keeps "A" plus limit-1 "é"s; a byte cut at limit keeps
// "A", (limit-1)/2 "é"s and, for an even limit, the lone lead byte 0xC3.
func overCap(limit int) (input, want string) {
	return "A" + strings.Repeat("é", limit+5), "A" + strings.Repeat("é", limit-1)
}

// assertCapped checks that a stored value is valid UTF-8 and holds exactly
// the expected characters.
func assertCapped(t *testing.T, field, got, want string, limit int) {
	t.Helper()
	if !utf8.ValidString(got) {
		t.Errorf("%s = %q is not valid UTF-8", field, got)
	}
	if n := utf8.RuneCountInString(got); n != limit {
		t.Errorf("%s has %d characters, want %d", field, n, limit)
	}
	if got != want {
		t.Errorf("%s = %q, want %q", field, got, want)
	}
}

// BBSLISTADD caps every field in characters, not bytes: the issue #453 repro
// (39 ASCII characters then "é" for the 40-character name) stores the name
// whole, and longer multi-byte values are cut to the cap without leaving a
// partial character behind.
func TestBBSListAddCapsFieldsByRune(t *testing.T) {
	env := newMenuEnv(t)
	issueName := strings.Repeat("N", 39) + "é"
	addr, wantAddr := overCap(60)
	telnet, wantTelnet := overCap(10)
	ssh, wantSSH := overCap(10)
	web, wantWeb := overCap(80)
	sysop, wantSysop := overCap(30)
	software, wantSoftware := overCap(20)
	desc, wantDesc := overCap(200)
	input := issueName + "\r" + addr + "\r" + telnet + "\r" + ssh + "\r" + web + "\r" +
		sysop + "\r" + software + "\r" + desc + "\r"
	r := env.runCmd("BBSLISTADD", env.caller, "", input)
	if r.err != nil || !r.has("Your entry has been added!") {
		t.Fatalf("err=%v output:\n%s", r.err, r.text())
	}
	got := loadEnvBBSList(t, env).Listings[0]
	assertCapped(t, "Name", got.Name, issueName, 40)
	assertCapped(t, "Address", got.Address, wantAddr, 60)
	assertCapped(t, "TelnetPort", got.TelnetPort, wantTelnet, 10)
	assertCapped(t, "SSHPort", got.SSHPort, wantSSH, 10)
	assertCapped(t, "Web", got.Web, wantWeb, 80)
	assertCapped(t, "Sysop", got.Sysop, wantSysop, 30)
	assertCapped(t, "Software", got.Software, wantSoftware, 20)
	assertCapped(t, "Description", got.Description, wantDesc, 200)

	// A name over the cap is cut to 40 characters, not 40 bytes.
	env2 := newMenuEnv(t)
	longName, wantName := overCap(40)
	env2.runCmd("BBSLISTADD", env2.caller, "", longName+"\rbbs.example\r\r\r\r\r\r\r")
	assertCapped(t, "long Name", loadEnvBBSList(t, env2).Listings[0].Name, wantName, 40)
}

// BBSLISTEDIT applies the same character caps as BBSLISTADD.
func TestBBSListEditCapsFieldsByRune(t *testing.T) {
	env := newMenuEnv(t)
	seedBBSList(t, env)

	caps := []struct {
		choice string
		limit  int
	}{{"1", 40}, {"2", 60}, {"3", 10}, {"4", 10}, {"5", 80}, {"6", 30}, {"7", 20}, {"8", 200}}
	input := "2\r"
	for _, c := range caps {
		v, _ := overCap(c.limit)
		input += c.choice + "\r" + v + "\r"
	}
	input += "Q\r"
	if r := env.runCmd("BBSLISTEDIT", env.caller, "", input); !r.has("Entry updated!") {
		t.Fatalf("edit flow:\n%s", r.text())
	}

	got := loadEnvBBSList(t, env).Listings[1]
	fields := []string{got.Name, got.Address, got.TelnetPort, got.SSHPort, got.Web, got.Sysop, got.Software, got.Description}
	names := []string{"Name", "Address", "TelnetPort", "SSHPort", "Web", "Sysop", "Software", "Description"}
	for i, c := range caps {
		_, want := overCap(c.limit)
		assertCapped(t, names[i], fields[i], want, c.limit)
	}
}

// The BBS list browser cuts its name, detail and description columns by
// character: a cut that would land inside "é" drops no half-character on
// screen. At 80 columns the name column is 25 wide, detail values 34 and
// description lines 47.
func TestBBSListBrowseColumnsCutByRune(t *testing.T) {
	env := newMenuEnv(t)
	name := strings.Repeat("B", 24) + "é" + "tail"  // byte 25 is inside "é"
	sysop := strings.Repeat("S", 33) + "é" + "tail" // byte 34 is inside "é"
	desc := strings.Repeat("D", 46) + "é" + "tail"  // byte 47 is inside "é"
	bld := &bbsListData{NextID: 2, Listings: []BBSListing{
		{ID: 1, Name: name, Sysop: sysop, Address: "bbs.example", Description: desc, AddedBy: "Someone"},
	}}
	if err := saveBBSListData(env.cfgDir(), bld); err != nil {
		t.Fatal(err)
	}

	r := env.runCmd("BBSLIST", env.caller, "", "q")
	if !utf8.ValidString(r.raw) {
		t.Errorf("browser output is not valid UTF-8")
	}
	for _, want := range []string{
		// The left-hand list row, "V ## name"; the detail panel shows the
		// name uncut, so the row prefix pins this to the name column.
		" 1 " + strings.Repeat("B", 24) + "é ",
		strings.Repeat("S", 33) + "é",
		strings.Repeat("D", 46) + "é",
	} {
		if !r.has(want) {
			t.Errorf("output lacks the column cut by character %q:\n%s", want, r.text())
		}
	}
	// The name also shows uncut in the wider detail panel, so only the
	// sysop and description columns are checked for having been cut.
	for _, uncut := range []string{sysop, desc} {
		if r.has(uncut) {
			t.Errorf("%q was not cut:\n%s", uncut, r.text())
		}
	}
}

// EDITNEWS caps a new or edited title at 28 characters, never splitting a
// multi-byte character at the 28th byte.
func TestEditNewsTitleCapByRune(t *testing.T) {
	title, want := overCap(28)

	env := newMenuEnv(t)
	seedNews(t, env, &NewsData{NextID: 1})
	input := "a\r" + title + "\r" + "0\r" + "0\r" + "n\r" + "\r" + "body\r" + "\r" + "Q\r"
	if r := env.runCmd("EDITNEWS", env.sysop, "", input); !r.has("News item added!") {
		t.Fatalf("add flow:\n%s", r.text())
	}
	assertCapped(t, "added Title", loadEnvNews(t, env).Items[0].Title, want, 28)

	env = newMenuEnv(t)
	seedNews(t, env, newsFixture(time.Now()))
	input = "E\r1\r" + "T\r" + title + "\r" + "Q\r" + "Q\r"
	if r := env.runCmd("EDITNEWS", env.sysop, "", input); !r.has("Item saved.") {
		t.Fatalf("edit flow:\n%s", r.text())
	}
	assertCapped(t, "edited Title", loadEnvNews(t, env).Items[0].Title, want, 28)
}

// The header template picker pads or cuts each row to 39 columns counted in
// characters, so a BAR-file name whose 39th byte falls inside "é" is neither
// split nor under-padded.
func TestTemplateOptionTextCutsByRune(t *testing.T) {
	// "[1 ] - " is 7 columns, leaving 32 for the name.
	cut := strings.Repeat("T", 31) + "é" + "tail"
	short := "Défaut"
	for _, tc := range []struct {
		name, text, want string
	}{
		{"cut inside é", cut, "[1 ] - " + strings.Repeat("T", 31) + "é"},
		{"padded with é", short, "[1 ] - " + short + strings.Repeat(" ", 39-7-utf8.RuneCountInString(short))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := templateOptionText(1, tc.text)
			if !utf8.ValidString(got) {
				t.Errorf("row %q is not valid UTF-8", got)
			}
			if n := utf8.RuneCountInString(got); n != templateOptionWidth {
				t.Errorf("row %q is %d columns, want %d", got, n, templateOptionWidth)
			}
			if got != tc.want {
				t.Errorf("row = %q, want %q", got, tc.want)
			}
		})
	}
}
