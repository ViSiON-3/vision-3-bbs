package menu

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// chatScreenCodesMsg holds every screen code between letters, so a code that
// runs is seen as its expansion next to a letter. The chat screen writes some of
// the same sequences itself, but never next to these.
const chatScreenCodesMsg = "a|CLb|CRc|DEd|Pe|PPf"

// chatScreenCodeBytes are what ReplacePipeCodes makes of chatScreenCodesMsg.
var chatScreenCodeBytes = []string{"a\x1b[2J", "\x1b[Hb", "b\r\nc", "c\x1b[Kd", "d\x1b[se", "e\x1b[uf"}

// chatCheckNoScreenCodes fails if out shows any screen code having run.
func chatCheckNoScreenCodes(t *testing.T, what, out string) {
	t.Helper()
	for _, code := range chatScreenCodeBytes {
		if strings.Contains(out, code) {
			t.Errorf("%s ran a screen code: output holds %q", what, code)
		}
	}
}

// One caller's room message, through the local service to another caller's
// running chat session, and back through the scrollback.
func TestChatRoomMessageCannotRunScreenCodes(t *testing.T) {
	env, live, _ := startToastChat(t)
	bob := chatcovLocal(env, "Bob")
	if _, _, err := bob.Join("lobby"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bob.Leave("lobby") })

	mark := len(live.s.output())
	if err := bob.Post("lobby", chatScreenCodesMsg+" ENDMARK"); err != nil {
		t.Fatal(err)
	}
	toastWaitFor(t, "the room line", func() bool { return strings.Contains(live.s.output()[mark:], "ENDMARK") })
	got := live.s.output()[mark:]
	t.Logf("other caller received %q", got)
	chatCheckNoScreenCodes(t, "a room message", got)
	if !strings.Contains(got, chatScreenCodesMsg) {
		t.Errorf("the codes are not shown as typed: %q", got)
	}

	// Each line is echoed before the next key is read, so PgUp runs after them
	// all. Twenty puts the marked line mid-page, not on the top row PgUp reaches.
	for range 20 {
		live.typeKeys("filler\r")
	}
	mark = len(live.s.output())
	// #844: this wait once timed out on Windows under -race and the cause isn't
	// known. If it happens again, show what the screen got and whether chat ended,
	// as it would if a stalled runner split PgUp's bytes into a bare ESC.
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		ended := false
		select {
		case <-live.done:
			ended = true
		default:
		}
		t.Logf("chat ended: %v; output since the last mark: %q", ended, live.s.output()[mark:])
	})
	live.typeKeys("\x1b[5~")
	toastWaitFor(t, "the line replayed", func() bool { return strings.Contains(live.s.output()[mark:], "ENDMARK") })
	got = live.s.output()[mark:]
	chatCheckNoScreenCodes(t, "the scrollback", got)
	if !strings.Contains(got, chatScreenCodesMsg) {
		t.Errorf("the replayed codes are not shown as typed: %q", got)
	}
	live.typeKeys("\r/q\r")
}

// The stored history replayed to the next caller into the room.
func TestChatLocalHistoryCannotRunScreenCodes(t *testing.T) {
	env := newMenuEnv(t)
	chatcovChat(env, env.caller, chatScreenCodesMsg+"\r/q\r")
	r := chatcovChat(env, env.sysop, "/q\r")
	chatCheckNoScreenCodes(t, "the history", r.raw)
	if !chatcovHas(r, "<Caller> "+chatScreenCodesMsg) {
		t.Errorf("history does not show the codes as typed:\n%s", chatcovText(r))
	}
}

// Everything another board can send over a V3Net network: this goes through the
// pickers, the header, the status bar and every kind of event.
func TestChatNetworkTextCannotRunScreenCodes(t *testing.T) {
	const evil = chatScreenCodesMsg
	env := newMenuEnv(t)
	n := chatcovFakeNet(env)
	n.name = "net" + evil
	env.e.ChatLeaves = chatcovLeaves{n.leaf()} // the leaf holds a copy of the name
	n.rooms = []chat.RoomInfo{
		{Name: "lobby", UserCount: 1, Topic: "pickertopic" + evil},
		{Name: "room" + evil, UserCount: 1},
	}
	n.users = []string{"Caller", "user" + evil}
	n.history = []chat.ChatMessage{
		{Handle: "histhandle" + evil, Text: "histtext" + evil},
		{IsSystem: true, Text: "histsystem" + evil},
	}
	n.events = []chat.ChatEvent{
		{Type: chat.TypeMessage, Message: &chat.ChatMessage{Handle: "msghandle" + evil, Text: "msgtext" + evil}},
		{Type: chat.TypePrivate, Message: &chat.ChatMessage{Handle: "privhandle" + evil, Text: "privtext" + evil}},
		{Type: chat.TypeJoin, Join: &chat.ChatJoin{Room: "jr" + evil, Handle: "joinhandle" + evil}},
		{Type: chat.TypeLeave, Leave: &chat.ChatLeave{Room: "lr" + evil, Handle: "leavehandle" + evil}},
		{Type: chat.TypeTopic, Topic: &chat.ChatTopic{Room: "tr" + evil, Topic: "topictext" + evil}},
		{Type: chat.TypeSystem, Text: "systext" + evil},
	}

	r := chatcovChat(env, env.caller, "1\r1\r/rooms\r/network\r\r\r/nosuch"+evil+"\r/q\r")
	chatCheckNoScreenCodes(t, "network text", r.raw)
	for _, want := range []string{
		"net" + evil,
		"pickertopic" + evil,
		"room" + evil,
		"user" + evil,
		"<histhandle" + evil + "> histtext" + evil,
		"histsystem" + evil,
		"<msghandle" + evil + "> msgtext" + evil,
		"privhandle" + evil + " -> you: privtext" + evil,
		"joinhandle" + evil + " has joined jr" + evil,
		"leavehandle" + evil + " has left lr" + evil,
		"Topic for tr" + evil + ": topictext" + evil,
		"systext" + evil,
		"/nosuch" + evil,
	} {
		if !strings.Contains(r.text(), want) {
			t.Errorf("output does not show %q as typed", want)
		}
	}
}

// V3Net text is not limited by what a keyboard can type: an escape sequence sent
// as it is must not reach the caller's terminal, in a line or in the header.
func TestChatNetworkTextCannotSendControlCharacters(t *testing.T) {
	env := newMenuEnv(t)
	n := chatcovFakeNet(env)
	n.history = nil
	n.events = []chat.ChatEvent{
		{Type: chat.TypeMessage, Message: &chat.ChatMessage{Handle: "esc\x1b[31mH", Text: "esc\x1b[32mT"}},
		{Type: chat.TypeMessage, Message: &chat.ChatMessage{Handle: "Bob", Text: "csi\u009b33mT line\r\nbreak"}},
		{Type: chat.TypePrivate, Message: &chat.ChatMessage{Handle: "esc\x1b[34mH", Text: "esc\x1b[35mT"}},
		{Type: chat.TypeJoin, Join: &chat.ChatJoin{Room: "den", Handle: "esc\x1b[36mH"}},
		{Type: chat.TypeTopic, Topic: &chat.ChatTopic{Room: "den", Topic: "esc\x1b[37mT"}},
	}
	r := chatcovChat(env, env.caller, "1\r1\r/q\r")
	for _, bad := range []string{"\x1b[31mH", "\x1b[32mT", "\u009b", "line\r\nbreak", "\x1b[34mH", "\x1b[35mT", "\x1b[36mH", "\x1b[37mT"} {
		if strings.Contains(r.raw, bad) {
			t.Errorf("output holds %q", bad)
		}
	}
	if !strings.Contains(r.raw, "esc [32mT") {
		t.Errorf("the rest of the text should still show:\n%q", r.raw)
	}
}

// The text header expands its line, so the topic is escaped there.
func TestChatFallbackHeaderCannotRunScreenCodes(t *testing.T) {
	env := newMenuEnv(t)
	env.e.MenuSetPath = filepath.Join(t.TempDir(), "menus", "v3")
	r := chatcovChat(env, env.caller, "/topic "+chatScreenCodesMsg+"\r/q\r")
	chatCheckNoScreenCodes(t, "the text header", r.raw)
	if !chatcovHas(r, " #lobby / "+chatScreenCodesMsg) {
		t.Errorf("topic not shown as typed:\n%s", chatcovText(r))
	}
}

// The art header is not expanded, so its topic must not be escaped: that would
// show the doubled pipe.
func TestChatArtHeaderShowsPipesOnce(t *testing.T) {
	env := newMenuEnv(t)
	r := chatcovChat(env, env.caller, "/topic x|CLy\r/q\r")
	if !chatcovHas(r, "Topic: x|CLy") || strings.Contains(r.raw, "x||CLy") {
		t.Errorf("header does not show the topic as typed:\n%s", chatcovText(r))
	}
}

func TestChatTextKeepsColoursAndTheEscapedPipe(t *testing.T) {
	const format = "<%s> %s"
	for _, tc := range []struct{ name, handle, text, want string }{
		{"colour", "Bob", "|04red", "<Bob> " + string(ansi.ReplacePipeCodes([]byte("|04"))) + "red"},
		{"background", "|B10x", "y", "<" + string(ansi.ReplacePipeCodes([]byte("|B10"))) + "x> y"},
		{"screen code", "Bob", "|CL", "<Bob> |CL"},
		{"typed escape", "Bob", "||CL", "<Bob> |CL"},
		{"a typed double pipe is one pipe, as it always was", "Bob", "a || b", "<Bob> a | b"},
		{"trailing pipe cannot join the format", "Bob|", "x", "<Bob|> x"},
		{"unknown code", "Bob", "|99", "<Bob> |99"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line := formatChatMessage(chat.ChatMessage{Handle: tc.handle, Text: tc.text}, "", format)
			if got := string(ansi.ReplacePipeCodes([]byte(line))); got != tc.want {
				t.Errorf("expanded %q = %q, want %q", line, got, tc.want)
			}
		})
	}
}

// A sysop's format keeps every pipe code; only what goes into it is escaped.
func TestChatFormatsKeepScreenCodes(t *testing.T) {
	line := formatChatMessage(chat.ChatMessage{Handle: "Bob", Text: "|CL"}, "", "|CL<%s> %s")
	got := string(ansi.ReplacePipeCodes([]byte(line)))
	if want := "\x1b[2J\x1b[H<Bob> |CL"; got != want {
		t.Errorf("expanded = %q, want %q", got, want)
	}
	line = formatChatMessage(chat.ChatMessage{IsSystem: true, Text: "|CL"}, "|CR*** %s", "")
	got = string(ansi.ReplacePipeCodes([]byte(line)))
	if want := "\r\n*** |CL"; got != want {
		t.Errorf("expanded = %q, want %q", got, want)
	}
}

// An escaped code must be measured as it shows, or the line wraps early.
func TestPipeDisplayLenCountsEscapes(t *testing.T) {
	for in, want := range map[string]int{
		"||":    1,
		"||CL":  3,
		"||05":  3, // shows as "|05"
		"|16":   3, // not a code: shows as typed
		"|B1x":  1,
		"|B10x": 1,
		"|23":   0,
		"end|":  4,
	} {
		if got := pipeDisplayLen(in); got != want {
			t.Errorf("pipeDisplayLen(%q) = %d, want %d", in, got, want)
		}
	}

	escaped := chatText("|CL |CL")
	if got := wrapPipeText(escaped, 7, ">> ", 3); len(got) != 1 {
		t.Errorf("wrapPipeText(%q, 7) = %q, want one line", escaped, got)
	}
}

func TestSysopChatHeaderCannotRunScreenCodes(t *testing.T) {
	var out bytes.Buffer
	st := &config.StringsConfig{SysopChatHeader: " %s chatting with %s "}
	drawChatView(&out, ansi.OutputModeUTF8, 80, 24, &config.ThemeConfig{}, st, builtinChatLayout(80, 24), "SysOp", chatScreenCodesMsg, time.Now())
	chatCheckNoScreenCodes(t, "the sysop chat header", out.String())
	if !strings.Contains(out.String(), "chatting with "+chatScreenCodesMsg) {
		t.Errorf("handle not shown as typed: %q", out.String())
	}
}

func TestNewUserNoticesCannotRunScreenCodes(t *testing.T) {
	sysop := sess(2, 255)
	e := notifyFixture(t, true, sysop)
	if paged, _ := e.notifySysopsOfNewUser(nil, &user.User{Handle: chatScreenCodesMsg}, 1); paged != 1 {
		t.Fatalf("paged %d, want 1", paged)
	}
	pages := sysop.DrainPages()
	if len(pages) != 1 {
		t.Fatalf("pages = %q, want one", pages)
	}
	// The page is expanded as a toast, and the stored fallback as it stands.
	for _, got := range []string{string(toastPipes(pages[0])), string(ansi.ReplacePipeCodes([]byte(pages[0])))} {
		chatCheckNoScreenCodes(t, "the new-user page", got)
		if !strings.Contains(got, chatScreenCodesMsg) {
			t.Errorf("page %q does not show the handle as typed", got)
		}
	}

	e.SetStrings(config.StringsConfig{NewUserSysopNotice: "New user: %s signed up %s ago from node %d."})
	text := e.renderSysopNotice(sysopNotice{Handle: chatScreenCodesMsg, Node: 1, CreatedAt: time.Now()}, time.Now())
	got := string(ansi.ReplacePipeCodes([]byte(text)))
	chatCheckNoScreenCodes(t, "the new-user notice", got)
	if !strings.Contains(got, chatScreenCodesMsg) {
		t.Errorf("notice %q does not show the handle as typed", got)
	}
}

// scripting/nodes.go puts a script's message and a handle into a page; a page
// is only ever shown through these, which is what makes that safe.
func TestScriptPageTextCannotRunScreenCodes(t *testing.T) {
	s := session.BbsSession{}
	s.AddPage("[Node 1 - " + chatScreenCodesMsg + "] " + chatScreenCodesMsg)
	page, ok := s.PeekPage()
	if !ok {
		t.Fatal("no page")
	}
	chatCheckNoScreenCodes(t, "a script page", string(toastPipes(toastLine(page))))
}

func TestToastLineBlanksC1Controls(t *testing.T) {
	if got, want := toastLine("a\u009bb\u0080c\u009fd"), "a b c d"; got != want {
		t.Errorf("toastLine = %q, want %q", got, want)
	}
	if got, want := toastLine("café"), "café"; got != want {
		t.Errorf("toastLine = %q, want %q", got, want)
	}
}
