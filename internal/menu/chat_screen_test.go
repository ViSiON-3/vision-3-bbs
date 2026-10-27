package menu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

func TestParseChatLayoutDefaultsAndValidates(t *testing.T) {
	sc, err := parseChatLayout([]byte(`{"sysop":{"lines":[{"row":2,"from":1,"to":40}]},"caller":{"lines":[{"row":4,"from":1,"to":40}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if sc.Width != 80 || sc.Height != 25 {
		t.Fatalf("size = %dx%d; want 80x25 by default", sc.Width, sc.Height)
	}
	if sc.fits(80, 24) || !sc.fits(80, 25) || !sc.fits(132, 50) {
		t.Fatal("fits is wrong about terminal sizes")
	}
	for name, layout := range map[string]string{
		"unknown key":      `{"sysop":{"lines":[{"row":1,"from":1,"to":9}]},"caller":{"lines":[{"row":2,"from":1,"to":9}]},"colour":1}`,
		"no caller lines":  `{"sysop":{"lines":[{"row":1,"from":1,"to":9}]},"caller":{"lines":[]}}`,
		"line off screen":  `{"sysop":{"lines":[{"row":1,"from":1,"to":81}]},"caller":{"lines":[{"row":2,"from":1,"to":9}]}}`,
		"line backwards":   `{"sysop":{"lines":[{"row":1,"from":9,"to":1}]},"caller":{"lines":[{"row":2,"from":1,"to":9}]}}`,
		"field off screen": `{"sysop":{"lines":[{"row":1,"from":1,"to":9}]},"caller":{"lines":[{"row":2,"from":1,"to":9}]},"fields":{"time":{"row":26,"col":1}}}`,
	} {
		if _, err := parseChatLayout([]byte(layout)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// writeChatScreen puts a screen called name in a bare menu set at dir.
func writeChatScreen(t *testing.T, dir, name, layout string, art []byte) {
	t.Helper()
	for _, sub := range []string{"cfg", "ansi"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "cfg", name+".json"), []byte(layout), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ansi", name+".ANS"), art, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestChatScreensListsLayouts(t *testing.T) {
	dir := t.TempDir()
	writeChatScreen(t, dir, "CHAT2", "{}", nil)
	writeChatScreen(t, dir, "chat1", "{}", nil)
	if err := os.WriteFile(filepath.Join(dir, "cfg", "MAIN.CFG"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ChatScreens(menuset.Bare(dir))
	if strings.Join(got, ",") != "CHAT2,chat1" {
		t.Fatalf("screens = %v; want CHAT2,chat1", got)
	}
	if got := ChatScreens(menuset.Bare(filepath.Join(dir, "missing"))); got != nil {
		t.Fatalf("screens of a missing set = %v", got)
	}
}

const testLayout = `{
  "width": 40, "height": 8, "statusColor": 15,
  "sysop": {"color": 14, "lines": [{"row": 2, "from": 3, "to": 20}, {"row": 3, "from": 3, "to": 20}]},
  "caller": {"lines": [{"row": 5, "from": 3, "to": 38}, {"row": 6, "from": 3, "to": 38}]},
  "fields": {"time": {"row": 8, "col": 3, "width": 5}, "elapsed": {"row": 8, "col": 11, "width": 2}, "callerName": {"row": 8, "col": 20}}
}`

const testArt = "\x1b[36m[@SYSOP###@]\r\n\r\n\r\n\x1b[36m[@CALLER##@]\r\n\r\n\r\n\r\nT:      E:"

// screenEnv points the chat env at a menu set and default screen.
func screenEnv(t *testing.T, r *chatRig, m menuset.Set, defaultScreen string) *chatEnv {
	t.Helper()
	env := *sysopChat.Load()
	env.menus = func() menuset.Set { return m }
	env.theme = func() *config.ThemeConfig {
		return &config.ThemeConfig{ChatSysopColor: 11, ChatUserColor: 10, SysopChatScreen: defaultScreen}
	}
	SetSysopChatEnv(env)
	return sysopChat.Load()
}

func TestChooseChatLayoutFallsBackToBuiltin(t *testing.T) {
	dir := t.TempDir()
	writeChatScreen(t, dir, "TEST", testLayout, []byte(testArt))
	r := newChatRig(t, "")
	env := screenEnv(t, r, menuset.Bare(dir), "TEST")
	for name, tc := range map[string]struct {
		want          string
		width, height int
		wantName      string
	}{
		"default":            {"", 80, 25, "TEST"},
		"asked for":          {"TEST", 40, 8, "TEST"},
		"builtin asked for":  {"builtin", 80, 25, ChatScreenBuiltin},
		"missing screen":     {"NOPE", 80, 25, ChatScreenBuiltin},
		"terminal too short": {"", 80, 7, ChatScreenBuiltin},
		"terminal too thin":  {"", 39, 25, ChatScreenBuiltin},
	} {
		if got := chooseChatLayout(env, tc.want, tc.width, tc.height); got.name != tc.wantName {
			t.Errorf("%s: chose %q; want %q", name, got.name, tc.wantName)
		}
	}
	env.menus = nil
	if got := chooseChatLayout(env, "TEST", 80, 25); got.name != ChatScreenBuiltin {
		t.Errorf("without a menu set chose %q", got.name)
	}
}

func TestChatOnDesignedScreen(t *testing.T) {
	dir := t.TempDir()
	writeChatScreen(t, dir, "TEST", testLayout, []byte(testArt))
	r := newChatRig(t, "")
	env := screenEnv(t, r, menuset.Bare(dir), "TEST")
	sc := chooseChatLayout(env, "", 80, 25)
	r.begin(t)
	done := make(chan struct{})
	go func() {
		runSysopChatOn(r.ih, r.tap, r.term, ansi.OutputModeUTF8, 80, 25, "SysOp", "Caller", sc)
		close(done)
	}()
	r.tap.Inject("SysOp", []byte("one\rtwo\rthree"))
	r.sess.Send("hi there")
	waitFor(t, func() bool {
		return r.term.Row(2) == "  two" && r.term.Row(3) == "  three" && r.term.Row(5) == "  hi there"
	}, "designed screen not rendered: "+r.term.Snapshot())
	if got := r.term.Row(1); got != "[SysOp     ]" {
		t.Errorf("sysop name in the art = %q", got)
	}
	if got := r.term.Row(4); got != "[Caller    ]" {
		t.Errorf("caller name in the art = %q", got)
	}
	hhmm := time.Now().Format("15:04")
	if got := r.term.Row(8); !strings.HasPrefix(got, "T:"+hhmm+" E:0") || !strings.HasSuffix(got, "Caller") {
		t.Errorf("status row = %q; want the time, elapsed minutes and caller name", got)
	}
	if got := r.term.Cell(2, 3).Fg; got != 93 {
		t.Errorf("sysop text fg = %d; want 93 (yellow from the layout)", got)
	}
	if got := r.term.Cell(5, 3).Fg; got != 92 {
		t.Errorf("caller text fg = %d; want 92 (green from the theme)", got)
	}
	_ = r.tap.StopChat("SysOp")
	waitDone(t, done, "chat ignored StopChat")
}

func TestShippedChatScreenLoads(t *testing.T) {
	m := menuset.Bare(filepath.Join("..", "..", "menus", "v3"))
	sc, err := loadChatLayout(m, "CHAT1")
	if err != nil {
		t.Fatal(err)
	}
	if !sc.fits(80, 25) || sc.fits(80, 24) {
		t.Fatalf("CHAT1 is %dx%d; want a screen for 80x25", sc.Width, sc.Height)
	}
	r := newChatRig(t, "")
	screenEnv(t, r, m, "CHAT1")
	r.begin(t)
	done := make(chan struct{})
	go func() {
		runSysopChatOn(r.ih, r.tap, r.term, ansi.OutputModeUTF8, 80, 25, "SysOp", "Caller", sc)
		close(done)
	}()
	r.tap.Inject("SysOp", []byte("hello"))
	r.sess.Send("hi")
	waitFor(t, func() bool {
		return strings.HasPrefix(r.term.Row(3), "│ hello") && strings.HasPrefix(r.term.Row(14), "│ hi")
	}, "CHAT1 not rendered: "+r.term.Snapshot())
	for row, want := range map[int]string{2: "SysOp", 13: "Caller", 1: "Time " + time.Now().Format("15:04"), 24: "Minutes in chat: 0"} {
		if got := r.term.Row(row); !strings.Contains(got, want) {
			t.Errorf("row %d = %q; want it to contain %q", row, got, want)
		}
	}
	_ = r.tap.StopChat("SysOp")
	waitDone(t, done, "chat ignored StopChat")
}

func TestConvertChatNfo(t *testing.T) {
	nfo := `TIME=60,1
DATE=70,1
ELAPSED=0,0
SYSOPNAME=10,2
USERNAME=10,13
STATCOLOR=15
SYSOPCOLOR=11
USERCOLOR=10
SYSOP_CLEAR_LINE
SYSOPLINES
2 3,4 77,4
1 3,3 77,3
USERLINES
1 3,14 77,14
2 3,15 77,15
`
	out, err := ConvertChatNfo(strings.NewReader(nfo))
	if err != nil {
		t.Fatal(err)
	}
	sc, err := parseChatLayout(out)
	if err != nil {
		t.Fatalf("converted layout does not parse: %v\n%s", err, out)
	}
	if sc.StatusColor != 15 || sc.Sysop.Color != 11 || sc.Caller.Color != 10 {
		t.Errorf("colours = %d/%d/%d", sc.StatusColor, sc.Sysop.Color, sc.Caller.Color)
	}
	if f := sc.Fields.Time; f == nil || f.Row != 1 || f.Col != 60 {
		t.Errorf("time field = %+v", f)
	}
	if sc.Fields.Elapsed != nil {
		t.Error("a field at 0,0 should be left out")
	}
	if f := sc.Fields.CallerName; f == nil || f.Row != 13 || f.Col != 10 {
		t.Errorf("caller name field = %+v", f)
	}
	want, _ := json.Marshal([]chatSegment{{Row: 3, From: 3, To: 77}, {Row: 4, From: 3, To: 77}})
	if got, _ := json.Marshal(sc.Sysop.Lines); string(got) != string(want) {
		t.Errorf("sysop lines = %s; want %s (in line-number order)", got, want)
	}
	if len(sc.Caller.Lines) != 2 || sc.Caller.Lines[1].Row != 15 {
		t.Errorf("caller lines = %+v", sc.Caller.Lines)
	}
	for name, bad := range map[string]string{
		"no lines":   "TIME=1,1\n",
		"two rows":   "SYSOPLINES\n1 1,1 10,2\nUSERLINES\n1 1,3 10,3\n",
		"nonsense":   "HELLO\n",
		"bad colour": "STATCOLOR=x\nSYSOPLINES\n1 1,1 10,1\nUSERLINES\n1 1,3 10,3\n",
	} {
		if _, err := ConvertChatNfo(strings.NewReader(bad)); err == nil {
			t.Errorf("%s: converted", name)
		}
	}
}

func TestConvertChatArt(t *testing.T) {
	got := ConvertChatArt([]byte("SysOp: |S  User: |u |07"))
	if string(got) != "SysOp: @SYSOP@  User: @CALLER@ |07" {
		t.Fatalf("converted art = %q", got)
	}
}
