package menu

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func doorMenuCodes(entries []doorMenuEntry) []string {
	codes := []string{}
	for _, e := range entries {
		codes = append(codes, e.code)
	}
	return codes
}

func TestDoorMenuFilteringGroupingSorting(t *testing.T) {
	doors := map[string]config.DoorConfig{
		"A": {Name: "Zulu", Category: "GAMES", ConfigOrder: 1, SortOrder: 2},
		"B": {Name: "Alpha", Category: "GAMES", ConfigOrder: 2, SortOrder: 1},
		"C": {Name: "Charlie", Category: "GAMES", MinAccessLevel: 100},
		"H": {Name: "Hidden", Hidden: true},
		"S": {Name: "Secret", Category: "SECRET"},
		"L": {Name: "Level", Category: "LEVEL"},
		"O": {Name: "Other"}, "U": {Name: "Unknown", Category: "MISSING"},
	}
	cfg := config.ServerConfig{DoorCategories: []config.DoorCategory{{Code: "GAMES", Name: "Games"}, {Code: "SECRET", ACS: "DENY"}, {Code: "LEVEL", MinAccessLevel: 100}}}
	allow := func(s string) bool { return s != "DENY" }
	checks := []struct {
		category, sort string
		want           []string
	}{
		{"", "", []string{"GAMES", "OTHER"}},
		{"GAMES", "name", []string{"B", "A"}},
		{"GAMES", "code", []string{"A", "B"}},
		{"GAMES", "config", []string{"A", "B"}},
		{"GAMES", "manual", []string{"B", "A"}},
		{"OTHER", "", []string{"O", "U"}}, {"SECRET", "", []string{}}, {"LEVEL", "", []string{}},
	}
	for _, tc := range checks {
		t.Run(tc.category+tc.sort, func(t *testing.T) {
			cfg.DoorMenuSort = tc.sort
			got := doorMenuCodes(buildDoorMenuEntries(doors, cfg, tc.category, 10, allow, "Other"))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
	cfg.DoorMenuSort = "code"
	cfg.DoorCategories[0].Sort = "name"
	if got := doorMenuCodes(buildDoorMenuEntries(doors, cfg, "GAMES", 10, allow, "Other")); !reflect.DeepEqual(got, []string{"B", "A"}) {
		t.Fatal(got)
	}
	cfg.DoorCategories = nil
	if got := doorMenuCodes(buildDoorMenuEntries(doors, cfg, "", 10, allow, "Other")); !reflect.DeepEqual(got, []string{"A", "B", "L", "O", "S", "U"}) {
		t.Fatal(got)
	}
}

func TestDoorMenuPageGeometry(t *testing.T) {
	for _, tc := range []struct{ height, top, bot, prompt, row, want int }{{24, 3, 2, 2, 1, 16}, {50, 3, 2, 2, 1, 42}, {24, 3, 2, 2, 2, 8}, {4, 3, 2, 2, 1, 1}} {
		if got := doorMenuPageSize(tc.height, tc.top, tc.bot, tc.prompt, tc.row); got != tc.want {
			t.Fatalf("%+v: got %d", tc, got)
		}
	}
	for _, tc := range []struct {
		text        string
		width, want int
	}{{"a\r\nb\r\n", 80, 2}, {"header", 80, 1}, {"123456789", 5, 2}, {"\x1b[79Cx", 80, 2}, {"", 80, 0}, {strings.Repeat("=", 78) + "\n" + strings.Repeat("-", 41) + "\n" + strings.Repeat("=", 78), 80, 3}} {
		if got := doorMenuLines(tc.text, tc.width); got != tc.want {
			t.Errorf("%q: %d want %d", tc.text, got, tc.want)
		}
	}
}

func TestDoorMenuTemplateFallback(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "menus", "v3")
	overlay := filepath.Join(root, "menus.d", "v3")
	put := func(root, name, text string) {
		t.Helper()
		p := filepath.Join(root, "templates", name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	put(base, "DOORMENU.TOP", "generic")
	put(base, "DOORMENU_GAMES.TOP.ANS", "category")
	put(overlay, "DOORMENU_GAMES.TOP.ans", "overlay")
	put(overlay, "DOORMENU.MID", "generic mid")
	put(base, "DOORCAT.TOP", "categories")
	e := &MenuExecutor{MenuSetPath: base}
	for _, tc := range []struct {
		cat, part string
		cats      bool
		want      string
	}{{"GAMES", "TOP", false, "overlay"}, {"GAMES", "MID", false, "generic mid"}, {"OTHER", "TOP", false, "generic"}, {"", "TOP", true, "categories"}} {
		b, err := e.doorMenuTemplate(tc.cat, tc.part, tc.cats)
		if err != nil || string(b) != tc.want {
			t.Fatalf("%+v got %q %v", tc, b, err)
		}
	}
	if _, err := e.doorMenuTemplate("../../BAD", "TOP", false); err == nil {
		t.Fatal("unsafe category accepted")
	}
}

func doorMenuHarness(t *testing.T, mode, input string, n int) (*cmdCtx, *testSession, *[]string) {
	t.Helper()
	root, err := filepath.Abs("../../menus/v3")
	if err != nil {
		t.Fatal(err)
	}
	e := &MenuExecutor{MenuSetPath: root, RunRegistry: map[string]RunnableFunc{}}
	e.SetServerConfig(config.ServerConfig{DoorMenuMode: mode, DoorMenuSort: "code"})
	e.SetStrings(config.StringsConfig{DoorMenuOther: "Other", DoorMenuEmpty: "No doors available", DoorMenuDenied: "Denied", ExecRunDoorError: "Door %s: %v"})
	doors := map[string]config.DoorConfig{}
	for i := 1; i <= n; i++ {
		code := fmt.Sprintf("D%02d", i)
		doors[code] = config.DoorConfig{Code: code, Name: "Door " + code}
	}
	e.SetDoorRegistry(doors)
	s := newTestSession(input)
	t.Cleanup(func() { resetSessionIH(s) })
	c := &cmdCtx{e: e, s: s, terminal: newTestTerminal(s), currentUser: &user.User{Handle: "Caller", AccessLevel: 10}, nodeNumber: 1, sessionStartTime: time.Now(), outputMode: ansi.OutputModeCP437, termWidth: 80, termHeight: 24}
	calls := []string{}
	e.RunRegistry["DOOR:"] = func(c *cmdCtx, code string) (*user.User, string, error) {
		calls = append(calls, code)
		return c.currentUser, "", nil
	}
	return c, s, &calls
}

func TestDoorMenuSelectionAndReturn(t *testing.T) {
	for _, tc := range []struct {
		mode, input string
		want        []string
	}{{"list", "12\rD02\rq", []string{"D12", "D02"}}, {"lightbar", "\x1b[B\r\rq", []string{"D02", "D02"}}, {"lightbar", "\x1b[F\rq", []string{"D30"}}, {"list", "]1\rq", []string{"D01"}}} {
		t.Run(tc.mode+tc.input, func(t *testing.T) {
			c, _, calls := doorMenuHarness(t, tc.mode, tc.input, 30)
			_, next, err := runDoorMenu(c, "")
			if err != nil || next != "" {
				t.Fatalf("%q %v", next, err)
			}
			if !reflect.DeepEqual(*calls, tc.want) {
				t.Fatalf("got %v want %v", *calls, tc.want)
			}
		})
	}
}

func TestDoorMenuCategoryBackAndDirectAccess(t *testing.T) {
	c, s, calls := doorMenuHarness(t, "list", "1\r1\rqq", 1)
	cfg := c.e.GetServerConfig()
	cfg.DoorCategories = []config.DoorCategory{{Code: "GAMES", Name: "Games"}}
	c.e.SetServerConfig(cfg)
	doors := c.e.DoorRegistry()
	d := doors["D01"]
	d.Category = "GAMES"
	doors["D01"] = d
	c.e.SetDoorRegistry(doors)
	if _, _, err := runDoorMenu(c, ""); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || !strings.Contains(s.output(), "Categories") {
		t.Fatalf("calls %v output %q", *calls, s.output())
	}
	cfg.DoorCategories[0].MinAccessLevel = 100
	c.e.SetServerConfig(cfg)
	if _, _, err := runDoorMenu(c, "GAMES"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.output(), "Denied") {
		t.Fatal("direct category bypassed access")
	}
}

func TestDoorMenuLaunchErrorsAndDisconnect(t *testing.T) {
	for _, tc := range []struct {
		err   error
		next  string
		input string
	}{{errors.New("launch failed"), "", "\rq"}, {io.EOF, "", "\r"}, {nil, "LOGOFF", "\r"}} {
		c, s, _ := doorMenuHarness(t, "lightbar", tc.input, 1)
		c.e.RunRegistry["DOOR:"] = func(c *cmdCtx, _ string) (*user.User, string, error) { return c.currentUser, tc.next, tc.err }
		_, next, err := runDoorMenu(c, "")
		if next != tc.next {
			t.Fatal(next)
		}
		if errors.Is(tc.err, io.EOF) {
			if !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		if tc.err != nil && !errors.Is(tc.err, io.EOF) && !strings.Contains(s.output(), "launch failed") {
			t.Fatal("missing launch error")
		}
	}
}

func TestDoorMenuRechecksAccessAfterInput(t *testing.T) {
	c, s, calls := doorMenuHarness(t, "lightbar", "\rq", 1)
	s.whenOutput("Selection", func() {
		doors := c.e.DoorRegistry()
		d := doors["D01"]
		d.MinAccessLevel = 100
		doors["D01"] = d
		c.e.SetDoorRegistry(doors)
	})
	if _, _, err := runDoorMenu(c, ""); err != nil {
		t.Fatal(err)
	}
	if len(*calls) > 0 {
		t.Fatal("launched revoked door")
	}
}

func TestDoorMenuMNUAccessAndDispatch(t *testing.T) {
	for _, action := range []string{"DOORMENU", "RUN:DOORMENU", "DOORMENU:GAMES", "RUN:DOORMENU:GAMES", "RUN:DOORMENU GAMES"} {
		c, _, _ := doorMenuHarness(t, "list", "q", 1)
		seen := "unset"
		c.e.RunRegistry["DOORMENU"] = func(_ *cmdCtx, args string) (*user.User, string, error) { seen = args; return c.currentUser, "", nil }
		kind, _, _ := c.e.executeCommandAction(action, c.s, c.terminal, nil, c.currentUser, 1, time.Now(), c.outputMode, 80, 24)
		want := ""
		if strings.Contains(action, "GAMES") {
			want = "GAMES"
		}
		if seen != want || kind != "CONTINUE" {
			t.Fatalf("%s: %s %q", action, kind, seen)
		}
	}
	c, s, _ := doorMenuHarness(t, "list", "", 1)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "mnu"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "mnu", "DOORMENU.MNU"), []byte(`{"ACS":"s100","FALLBACK":"MAIN"}`), 0644); err != nil {
		t.Fatal(err)
	}
	c.e.MenuSetPath = root
	if _, next, err := runDoorMenu(c, ""); err != nil || next != "GOTO:MAIN" {
		t.Fatalf("%q %v", next, err)
	}
	if !strings.Contains(s.output(), "Denied") {
		t.Fatal("no denied message")
	}
}

func TestDoorMenuResizeAfterLaunch(t *testing.T) {
	c, s, calls := doorMenuHarness(t, "lightbar", "30\r\rq", 60)
	// No PTY: preferences supply dimensions when no session size is known.
	c.termWidth, c.termHeight = 0, 0
	c.currentUser.ScreenHeight = 24
	c.e.SessionRegistry = session.NewSessionRegistry()
	node := &session.BbsSession{NodeID: 1, ID: 1, Width: 80, Height: 24}
	c.e.SessionRegistry.Register(node)
	original := c.e.RunRegistry["DOOR:"]
	c.e.RunRegistry["DOOR:"] = func(c *cmdCtx, code string) (*user.User, string, error) {
		node.Mutex.Lock()
		node.Height = 50
		node.Mutex.Unlock()
		return original(c, code)
	}
	if _, _, err := runDoorMenu(c, ""); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*calls, []string{"D30", "D30"}) {
		t.Fatal(*calls)
	}
	output := stripAreaAnsi(s.output())
	if !strings.Contains(output, "Page 1 of 4") || !strings.Contains(output, "Page 1 of 2") {
		t.Fatalf("missing resized pagination: %s", output)
	}
}

func TestDoorMenuPromptAndColors(t *testing.T) {
	c, s, _ := doorMenuHarness(t, "lightbar", "q", 1)
	root := t.TempDir()
	for _, dir := range []string{"mnu", "templates", "bar"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"mnu/DOORMENU.MNU":       `{"TITLE":"Custom Doors","CLR":false,"USEPROMPT":true,"PROMPT1":"Caller |UH","PROMPT2":"Your choice"}`,
		"templates/DOORMENU.TOP": "^TI",
		"templates/DOORMENU.MID": "|12^ID ^NA ^DS",
		"templates/DOORMENU.BOT": "^PG/^PT",
		"bar/DOORMENUHI.BAR":     "0,0,31,2,*,*,Highlight\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	c.e.MenuSetPath = root
	if _, _, err := runDoorMenu(c, ""); err != nil {
		t.Fatal(err)
	}
	out := s.output()
	if strings.Contains(out, ansi.ClearScreen()) || !strings.Contains(out, "Caller Caller") || !strings.Contains(out, "Your choice") || !strings.Contains(out, "Custom Doors") || !strings.Contains(out, colorCodeToAnsi(31)) {
		t.Fatalf("unexpected rendering %q", out)
	}
}

func TestDoorMenuCategorySnapshotsAreOwned(t *testing.T) {
	e := &MenuExecutor{}
	cfg := config.ServerConfig{DoorCategories: []config.DoorCategory{{Code: "GAMES", Name: "Games"}}}
	e.SetServerConfig(cfg)
	cfg.DoorCategories[0].Name = "caller mutated"
	got := e.GetServerConfig()
	if got.DoorCategories[0].Name != "Games" {
		t.Fatal("store retained caller slice")
	}
	got.DoorCategories[0].Name = "reader mutated"
	if e.GetServerConfig().DoorCategories[0].Name != "Games" {
		t.Fatal("reader changed snapshot")
	}
}

func TestDoorMenuBrokenCategoryOverrideDoesNotFallBack(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "templates"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "templates", "DOORMENU.TOP"), []byte("generic"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "templates", "DOORMENU_GAMES.TOP")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	e := &MenuExecutor{MenuSetPath: root}
	if b, err := e.doorMenuTemplate("GAMES", "TOP", false); err == nil {
		t.Fatalf("broken explicit override fell back to %q", b)
	}
}

func TestDoorMenuRenderedPagesFitTerminal(t *testing.T) {
	for _, height := range []int{24, 50} {
		for _, mode := range []string{"list", "lightbar"} {
			t.Run(fmt.Sprintf("%s/%d", mode, height), func(t *testing.T) {
				c, s, _ := doorMenuHarness(t, mode, "q", 100)
				c.termHeight = height
				if _, _, err := runDoorMenu(c, ""); err != nil {
					t.Fatal(err)
				}
				rows, _ := ansi.ArtGeometry([]byte(s.output()), 80)
				if rows >= height {
					t.Fatalf("rendered page reaches row %d on %d-row terminal (must reserve bottom row)", rows, height)
				}
			})
		}
	}
}

// Paging help is noise when every door fits on one screen: header and footer
// lines that show the page number or count appear only with several pages.
func TestDoorMenuPagingHelpOnlyWithSeveralPages(t *testing.T) {
	for _, tc := range []struct {
		doors int
		want  bool
	}{{3, false}, {100, true}} {
		c, s, _ := doorMenuHarness(t, "lightbar", "q", tc.doors)
		if _, _, err := runDoorMenu(c, ""); err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(s.output(), "Page"); got != tc.want {
			t.Errorf("%d doors: paging help shown=%v, want %v\n%q", tc.doors, got, tc.want, s.output())
		}
	}
	if got := doorMenuDropPaging("head\r\nPage ^PG of ^PT\r\nfoot"); got != "head\r\nfoot" {
		t.Errorf("doorMenuDropPaging = %q", got)
	}
}

// withDoorColumns sets the harness's global column count.
func withDoorColumns(c *cmdCtx, n int) {
	cfg := c.e.GetServerConfig()
	cfg.DoorMenuColumns = n
	c.e.SetServerConfig(cfg)
}

// Entries run down each column, and a page that is not full splits evenly:
// seven doors in two columns are four rows, D01 beside D05.
func TestDoorMenuColumnsLayout(t *testing.T) {
	c, s, _ := doorMenuHarness(t, "list", "q", 7)
	withDoorColumns(c, 2)
	if _, _, err := runDoorMenu(c, ""); err != nil {
		t.Fatal(err)
	}
	var rows []string
	for _, l := range strings.Split(ansi.StripAnsi(s.output()), "\r\n") {
		if strings.Contains(l, "Door D") {
			rows = append(rows, l)
		}
		if ansi.VisibleLength(l) > 79 {
			t.Errorf("line reaches the last column: %q", l)
		}
	}
	if len(rows) != 4 || !strings.Contains(rows[0], "D01") || !strings.Contains(rows[0], "D05") || strings.Contains(rows[3], "D08") {
		t.Fatalf("unexpected column layout:\n%s", strings.Join(rows, "\n"))
	}
}

// Left and Right move between columns on the same row; Right from a row the
// shorter last column lacks lands on the last entry.
func TestDoorMenuColumnsArrowKeys(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"\x1b[C\r", "D05"},
		{"\x1b[C\x1b[D\r", "D01"},
		{"\x1b[B\x1b[B\x1b[B\x1b[C\r", "D07"},
		{"\x1b[C\x1b[C\r", "D05"},
		{"\x1b[D\r", "D01"},
	} {
		c, _, calls := doorMenuHarness(t, "lightbar", tc.input+"q", 7)
		withDoorColumns(c, 2)
		if _, _, err := runDoorMenu(c, ""); err != nil {
			t.Fatal(err)
		}
		if len(*calls) != 1 || (*calls)[0] != tc.want {
			t.Errorf("%q: launched %v, want %s", tc.input, *calls, tc.want)
		}
	}
}

// Moving the bar within a page repaints the two cells involved instead of
// clearing and redrawing the screen; changing page still redraws it.
func TestDoorMenuLightbarRepaintsInPlace(t *testing.T) {
	for _, tc := range []struct {
		input  string
		clears int
	}{{"\x1b[B\x1b[B\x1b[Aq", 1}, {"]q", 2}} {
		c, s, _ := doorMenuHarness(t, "lightbar", tc.input, 30)
		if _, _, err := runDoorMenu(c, ""); err != nil {
			t.Fatal(err)
		}
		out := s.output()
		if got := strings.Count(out, ansi.ClearScreen()); got != tc.clears {
			t.Errorf("%q: %d screen clears, want %d", tc.input, got, tc.clears)
		}
		if tc.clears == 1 && !strings.Contains(out, ansi.SaveCursor()+ansi.MoveCursor(3, 1)) {
			t.Errorf("%q: no in-place repaint of the first row", tc.input)
		}
	}
}

func TestDoorMenuFitColumns(t *testing.T) {
	for _, tc := range []struct{ cols, width, want int }{{2, 80, 2}, {4, 80, 3}, {4, 40, 1}, {1, 80, 1}, {3, 10, 1}} {
		if got := doorMenuFitColumns(tc.cols, tc.width); got != tc.want {
			t.Errorf("doorMenuFitColumns(%d, %d) = %d, want %d", tc.cols, tc.width, got, tc.want)
		}
	}
}

// A header of full-width lines separated by bare LFs, like the block-bar art
// sysops draw, must not throw the in-place repaint off: entries start on the
// row right after the header.
func TestDoorMenuRepaintBelowWideHeader(t *testing.T) {
	c, s, _ := doorMenuHarness(t, "lightbar", "\x1b[Bq", 3)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "templates"), 0755); err != nil {
		t.Fatal(err)
	}
	header := strings.Repeat("=", 78) + "\n" + strings.Repeat("-", 41) + "\n" + strings.Repeat("=", 78) + "\n  #   Code   Door\n"
	if err := os.WriteFile(filepath.Join(root, "templates", "DOORMENU.TOP"), []byte(header), 0644); err != nil {
		t.Fatal(err)
	}
	shipped := c.e.MenuSetPath
	c.e.MenuSetPath = root
	for _, name := range []string{"mnu/DOORMENU.MNU", "templates/DOORMENU.MID", "templates/DOORMENU.BOT"} {
		b, err := os.ReadFile(filepath.Join(shipped, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), b, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := runDoorMenu(c, ""); err != nil {
		t.Fatal(err)
	}
	// Four header rows: the first entry is row 5, the second row 6.
	if out := s.output(); !strings.Contains(out, ansi.MoveCursor(5, 1)) || !strings.Contains(out, ansi.MoveCursor(6, 1)) {
		t.Fatalf("repaint not on rows 5 and 6:\n%q", out)
	}
}
