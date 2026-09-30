package menu

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// chatcovNet is a fake V3Net chat network. Every session it hands out (the
// picker's probes as well as the one the caller ends up chatting on) is a
// chatcovSvc that records what was done to it, so tests can assert on what
// runChat sent to the network rather than only on what it drew.
type chatcovNet struct {
	name    string
	rooms   []chat.RoomInfo
	history []chat.ChatMessage // returned by every successful Join
	users   []string
	events  []chat.ChatEvent // waiting on each session's Events channel from the start

	roomsErr, postErr, topicErr, privErr error
	joinErr                              func(room string) error
	// nilFrom makes NewSession return nil from that call on (1-based); 0 = never.
	nilFrom int

	mu       sync.Mutex
	calls    int
	sessions []*chatcovSvc
}

// leaf is the ChatLeafInfo runChat sees for this network.
func (n *chatcovNet) leaf() ChatLeafInfo {
	return ChatLeafInfo{NetworkName: n.name, NewSession: func(handle string) chat.ChatService {
		n.mu.Lock()
		defer n.mu.Unlock()
		n.calls++
		if n.nilFrom > 0 && n.calls >= n.nilFrom {
			return nil
		}
		svc := &chatcovSvc{net: n, handle: handle, events: make(chan chat.ChatEvent, len(n.events))}
		for _, ev := range n.events {
			svc.events <- ev
		}
		n.sessions = append(n.sessions, svc)
		return svc
	}}
}

// chatted returns the sessions that were asked to join a room, in the order
// they were created: the ones a caller chatted on, as opposed to probes.
func (n *chatcovNet) chatted() []*chatcovSvc {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []*chatcovSvc
	for _, s := range n.sessions {
		if len(s.log("join")) > 0 {
			out = append(out, s)
		}
	}
	return out
}

// allClosed reports whether every session handed out was closed exactly once.
func (n *chatcovNet) allClosed() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, s := range n.sessions {
		if len(s.log("close")) != 1 {
			return false
		}
	}
	return true
}

// chatcovSvc is one session on a chatcovNet.
type chatcovSvc struct {
	net    *chatcovNet
	handle string
	events chan chat.ChatEvent

	mu    sync.Mutex
	calls []string // "verb arg", in call order
}

func (s *chatcovSvc) record(verb, arg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, strings.TrimSpace(verb+" "+arg))
}

// log returns the arguments of every recorded call to verb, in order.
func (s *chatcovSvc) log(verb string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []string{}
	for _, c := range s.calls {
		if c == verb {
			out = append(out, "")
		} else if rest, ok := strings.CutPrefix(c, verb+" "); ok {
			out = append(out, rest)
		}
	}
	return out
}

func (s *chatcovSvc) Join(room string) ([]chat.RoomInfo, []chat.ChatMessage, error) {
	s.record("join", room)
	if s.net.joinErr != nil {
		if err := s.net.joinErr(room); err != nil {
			return nil, nil, err
		}
	}
	return s.net.rooms, s.net.history, nil
}

func (s *chatcovSvc) Leave(room string) error { s.record("leave", room); return nil }

func (s *chatcovSvc) Post(room, text string) error {
	s.record("post", room+": "+text)
	return s.net.postErr
}

func (s *chatcovSvc) Private(handle, node, text string) error {
	s.record("private", handle+"@"+node+": "+text)
	return s.net.privErr
}

func (s *chatcovSvc) SetTopic(room, topic string) error {
	s.record("topic", room+": "+topic)
	return s.net.topicErr
}

func (s *chatcovSvc) Rooms() ([]chat.RoomInfo, error) { return s.net.rooms, s.net.roomsErr }

func (s *chatcovSvc) History(string, int) ([]chat.ChatMessage, error) { return s.net.history, nil }

func (s *chatcovSvc) Users() []string { return s.net.users }

func (s *chatcovSvc) Events() <-chan chat.ChatEvent { return s.events }

// Close closes the events channel, as the ChatService contract requires. A
// second Close panics on the closed channel, exactly as LocalChatService
// does, so a double close in runChat fails the test.
func (s *chatcovSvc) Close() error {
	s.record("close", "")
	close(s.events)
	return nil
}

// chatcovLeaves is a ChatLeafProvider over a fixed list of networks.
type chatcovLeaves []ChatLeafInfo

func (l chatcovLeaves) ActiveChatLeaves() []ChatLeafInfo { return l }

// chatcovFakeNet returns a network with two rooms and one of each kind of
// event queued, and installs it as env's only V3Net chat leaf.
func chatcovFakeNet(env *menuEnv) *chatcovNet {
	n := &chatcovNet{
		name: "FakeNet",
		rooms: []chat.RoomInfo{
			{Name: "lobby", UserCount: 3, Topic: "General"},
			{Name: "den", UserCount: 2},
		},
		users: []string{"Caller", "Bob"},
		history: []chat.ChatMessage{
			{Handle: "Bob", Text: "earlier on"},
			{IsSystem: true, Text: "server restarted"},
		},
		events: []chat.ChatEvent{
			{Type: chat.TypeMessage, Message: &chat.ChatMessage{Handle: "Bob", Text: "hi from bob"}},
			{Type: chat.TypePrivate, Message: &chat.ChatMessage{Handle: "Bob", Text: "psst"}},
			{Type: chat.TypeJoin, Join: &chat.ChatJoin{Room: "den", Handle: "Eve"}},
			{Type: chat.TypeLeave, Leave: &chat.ChatLeave{Room: "den", Handle: "Eve"}},
			{Type: chat.TypeTopic, Topic: &chat.ChatTopic{Room: "den", Topic: "Cats"}},
			{Type: chat.TypeSystem, Reconnect: true},
			{Type: chat.TypeSystem, Text: "link lagging"},
			{Type: chat.TypeSystem},  // no text: nothing to show
			{Type: chat.TypeMessage}, // no payload: must be skipped, not dereferenced
			{Type: chat.TypePrivate},
			{Type: chat.TypeJoin},
			{Type: chat.TypeLeave},
			{Type: chat.TypeTopic},
		},
	}
	env.e.ChatLeaves = chatcovLeaves{n.leaf()}
	return n
}

// chatcovEventLines is what the events queued by chatcovFakeNet render as.
var chatcovEventLines = []string{
	"<Bob> hi from bob",
	"*** Bob -> you: psst",
	"*** Eve has joined den",
	"*** Eve has left den",
	"*** Topic for den: Cats",
	"*** reconnected",
	"*** link lagging",
}

// chatcovChat runs a chat session for u with input as keystrokes.
func chatcovChat(env *menuEnv, u *user.User, input string) runResult {
	env.t.Helper()
	return env.run(runChat, u, "", input)
}

// chatcovInputRow matches one redraw of the chat input row: position, clear,
// prompt and typed text, then the cursor put back after it.
var chatcovInputRow = regexp.MustCompile(`\x1b\[\d+;1H\x1b\[2K[^\x1b]*\x1b\[\d+;\d+H`)

// chatcovText is r.text() without the input row. The input row echoes every
// keystroke, so with it left in, a typed line would satisfy an assertion that
// the line was written to the chat area whether or not it ever was.
func chatcovText(r runResult) string {
	return testAnsiEscape.ReplaceAllString(chatcovInputRow.ReplaceAllString(r.raw, ""), "")
}

// chatcovHas reports whether chatcovText(r) contains every one of want.
func chatcovHas(r runResult, want ...string) bool {
	txt := chatcovText(r)
	for _, w := range want {
		if !strings.Contains(txt, w) {
			return false
		}
	}
	return true
}

// chatcovLocal opens a second local chat session as handle on env's chat
// database, closed when the test ends: "someone else on the board".
func chatcovLocal(env *menuEnv, handle string) *chat.LocalChatService {
	env.t.Helper()
	svc, err := chat.NewLocalChatService(handle, filepath.Join(env.dataDir(), "chat.db"))
	if err != nil {
		env.t.Fatalf("NewLocalChatService: %v", err)
	}
	env.t.Cleanup(func() { _ = svc.Close() })
	return svc
}

// chatcovLocalHistory returns the texts stored for room in env's local chat
// database, oldest first, as "handle: text".
func chatcovLocalHistory(env *menuEnv, room string) []string {
	env.t.Helper()
	msgs, err := chatcovLocal(env, "historyreader").History(room, 50)
	if err != nil {
		env.t.Fatalf("History(%q): %v", room, err)
	}
	out := []string{}
	for _, m := range msgs {
		out = append(out, m.Handle+": "+m.Text)
	}
	return out
}

// chatcovDrain returns the events waiting on svc without blocking.
func chatcovDrain(svc chat.ChatService) []chat.ChatEvent {
	var out []chat.ChatEvent
	for {
		select {
		case ev := <-svc.Events():
			out = append(out, ev)
		default:
			return out
		}
	}
}

// chatcovEqual fails the test unless got and want hold the same strings.
func chatcovEqual(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("%s = %q, want %q", what, got, want)
	}
}

func TestChatcovLocalSession(t *testing.T) {
	env := newMenuEnv(t)

	r := chatcovChat(env, env.caller, "hello world\r\r/topic Cats\r/rooms\r/users\r/q\r")
	if r.err != nil || r.next != "" || r.user != nil {
		t.Fatalf("runChat = (%v, %q, %v), want a plain return to the menu", r.user, r.next, r.err)
	}
	if !chatcovHas(r, "*** Joined #lobby", "<Caller> hello world", "Users: Caller", "/rooms /join /msg /topic /network /q") {
		t.Errorf("chat screen missing join notice, echo or status bar:\n%s", chatcovText(r))
	}
	// The topic change comes back as an event, and /rooms then lists it.
	if !chatcovHas(r, "*** Topic for lobby: Cats", "Active Rooms", "  1 users  Cats") {
		t.Errorf("topic change or room list missing:\n%s", chatcovText(r))
	}
	// The scroll region covers the chat rows of a 24-line screen while the
	// session runs and is reset on the way out.
	if !strings.Contains(r.raw, "\x1b[6;21r") || !strings.HasSuffix(r.raw, "\x1b[r") {
		t.Errorf("scroll region not set to rows 6-21 and reset at exit")
	}

	chatcovEqual(t, "stored lobby history", chatcovLocalHistory(env, "lobby"), []string{"Caller: hello world"})
	// Leaving chat takes the caller out of the room, which empties and drops it.
	if rooms, _ := chatcovLocal(env, "observer").Rooms(); len(rooms) != 0 {
		t.Errorf("rooms still occupied after the caller quit: %+v", rooms)
	}

	// The next session scrolls the stored history back in; /QUIT also leaves.
	r = chatcovChat(env, env.sysop, "/QUIT\r")
	if r.next != "" || !chatcovHas(r, "<Caller> hello world") {
		t.Errorf("scrollback not shown to the next session (next=%q):\n%s", r.next, chatcovText(r))
	}
}

func TestChatcovNotLoggedIn(t *testing.T) {
	env := newMenuEnv(t)
	if r := chatcovChat(env, nil, "hello\r/q\r"); r.raw != "" || r.next != "" || r.err != nil {
		t.Errorf("runChat with no user = (%q, %v, output %q), want nothing", r.next, r.err, r.raw)
	}
}

func TestChatcovDisconnectLeavesRoom(t *testing.T) {
	env := newMenuEnv(t)
	other := chatcovLocal(env, "Sysop")
	if _, _, err := other.Join("lobby"); err != nil {
		t.Fatal(err)
	}

	// Enter at the room prompt takes the default; then the line drops mid-message.
	r := chatcovChat(env, env.caller, "\rhalf a mess")
	if r.next != "LOGOFF" || r.err != nil {
		t.Fatalf("runChat = (%q, %v), want LOGOFF", r.next, r.err)
	}
	if !strings.HasSuffix(r.raw, "\x1b[r") {
		t.Errorf("scroll region not reset after a disconnect")
	}

	// The other member saw the caller arrive and go, and no half-typed post.
	var seen []string
	for _, ev := range chatcovDrain(other) {
		switch ev.Type {
		case chat.TypeJoin:
			seen = append(seen, "join "+ev.Join.Handle)
		case chat.TypeLeave:
			seen = append(seen, "leave "+ev.Leave.Handle)
		default:
			seen = append(seen, fmt.Sprintf("unexpected event type %d", ev.Type))
		}
	}
	// One leave, not two: cleanup calls Leave and then Close, and Close must
	// not announce a second departure from a room already left.
	chatcovEqual(t, "events seen by the other member", seen, []string{"join Caller", "leave Caller"})
}

func TestChatcovRoomPicker(t *testing.T) {
	env := newMenuEnv(t)
	other := chatcovLocal(env, "Sysop")
	if _, _, err := other.Join("lounge"); err != nil {
		t.Fatal(err)
	}
	if err := other.SetTopic("lounge", "Sofas"); err != nil {
		t.Fatal(err)
	}
	chatcovDrain(other) // its own topic event

	r := chatcovChat(env, env.caller, "1\rhey all\r/msg Sysop@2 psst there\r/msg Sysop\r/q\r")
	if r.next != "" || r.err != nil {
		t.Fatalf("runChat = (%q, %v), want a plain return", r.next, r.err)
	}
	if !chatcovHas(r, "Active Rooms", "lounge", "Sofas", "Select room [lobby]:", "*** Joined #lounge", "<Caller> hey all") {
		t.Errorf("room picker or chat screen wrong:\n%s", chatcovText(r))
	}
	if !chatcovHas(r, "Users: Caller, Sysop") && !chatcovHas(r, "Users: Sysop, Caller") {
		t.Errorf("status bar does not list both members:\n%s", chatcovText(r))
	}

	var seen []string
	for _, ev := range chatcovDrain(other) {
		switch ev.Type {
		case chat.TypeJoin:
			seen = append(seen, "join "+ev.Join.Handle+" "+ev.Join.Room)
		case chat.TypeMessage:
			seen = append(seen, "message "+ev.Message.Handle+": "+ev.Message.Text)
		case chat.TypePrivate:
			seen = append(seen, "private "+ev.Message.Handle+": "+ev.Message.Text)
		case chat.TypeLeave:
			seen = append(seen, "leave "+ev.Leave.Handle+" "+ev.Leave.Room)
		}
	}
	// "/msg Sysop" has no message text and sends nothing.
	chatcovEqual(t, "events seen by the other member", seen, []string{
		"join Caller lounge",
		"message Caller: hey all",
		"private Caller: psst there",
		"leave Caller lounge",
	})

	for _, tc := range []struct {
		name, reply, wantRoom string
	}{
		{"Enter takes the lobby", "\r", "lobby"},
		{"a name joins that room", " den \r", "den"},
		{"a number past the list is a room name", "9\r", "9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := chatcovChat(env.sub(t), env.caller, tc.reply+"/q\r")
			// Matched up to the colour reset that ends the notice, so "#9"
			// cannot be satisfied by some longer room name.
			if !strings.Contains(r.raw, "Joined #"+tc.wantRoom+"\x1b") {
				t.Errorf("want room %q joined:\n%s", tc.wantRoom, chatcovText(r))
			}
		})
	}
}

func TestChatcovJoinCommand(t *testing.T) {
	env := newMenuEnv(t)
	// Someone has already been talking in #den.
	other := chatcovLocal(env, "Sysop")
	if _, _, err := other.Join("den"); err != nil {
		t.Fatal(err)
	}
	if err := other.Post("den", "old news"); err != nil {
		t.Fatal(err)
	}
	if err := other.Leave("den"); err != nil {
		t.Fatal(err)
	}

	r := chatcovChat(env, env.caller, "in lobby\r/join den\rin den\r/q\r")
	if !chatcovHas(r, "*** Joined #lobby", "*** Joined #den", "<Sysop> old news", "<Caller> in den") {
		t.Errorf("/join did not move rooms and show the new room's history:\n%s", chatcovText(r))
	}
	chatcovEqual(t, "lobby history", chatcovLocalHistory(env, "lobby"), []string{"Caller: in lobby"})
	chatcovEqual(t, "den history", chatcovLocalHistory(env, "den"), []string{"Sysop: old news", "Caller: in den"})

	// A room name the service rejects is reported.
	r = chatcovChat(env, env.caller, "/join Bad!Room\r/q\r")
	if !chatcovHas(r, "Could not join room: room name may only contain lowercase letters,") {
		t.Errorf("rejected /join not reported:\n%s", chatcovText(r))
	}
	if chatcovHas(r, "*** Joined #Bad!Room") {
		t.Errorf("rejected /join still announced the room:\n%s", chatcovText(r))
	}
}

func TestChatcovLongMessageWraps(t *testing.T) {
	env := newMenuEnv(t)
	// 69 characters: fits the 70-column input line, but not an 80-column
	// chat row once the timestamp and handle are in front of it.
	msg := strings.TrimSpace(strings.Repeat("wordy ", 11)) + " end"
	if len(msg) != 69 {
		t.Fatalf("test message is %d chars, want 69", len(msg))
	}

	r := chatcovChat(env, env.caller, msg+"\r/q\r")
	// The last word moves to a continuation row: the region scrolls up two
	// rows, then the message goes on row 20 and the prefixed remainder on 21.
	_, written, ok := strings.Cut(r.raw, "\x1b[21;1H\r\n\r\n\x1b[20;1H")
	if !ok {
		t.Fatalf("chat region not scrolled by two rows for a two-row message:\n%s", chatcovText(r))
	}
	row20, row21, ok := strings.Cut(written, "\x1b[21;1H")
	if !ok {
		t.Fatalf("no continuation row written at row 21")
	}
	if got := testAnsiEscape.ReplaceAllString(row20, ""); !strings.HasSuffix(got, "<Caller> "+strings.TrimSuffix(msg, " end")) {
		t.Errorf("row 20 = %q, want the message up to its last word", got)
	}
	if got := testAnsiEscape.ReplaceAllString(row21, ""); !strings.HasPrefix(got, "      \u2514 end") {
		t.Errorf("row 21 starts %q, want the continuation marker and the wrapped word", got[:min(len(got), 20)])
	}
	chatcovEqual(t, "stored message", chatcovLocalHistory(env, "lobby"), []string{"Caller: " + msg})
}

// Without CHATHEADER.ANS in the menu set the header is drawn as text: the
// room, and the topic once there is one.
func TestChatcovFallbackHeader(t *testing.T) {
	env := newMenuEnv(t)
	env.e.MenuSetPath = filepath.Join(t.TempDir(), "menus", "v3")

	r := chatcovChat(env, env.caller, "/topic Cats\r/q\r")
	if !chatcovHas(r, " #lobby", " #lobby / Cats") {
		t.Errorf("text header missing the room or the topic:\n%s", chatcovText(r))
	}
	// Rows 4 and 5 of the header area are blanked.
	if !strings.Contains(r.raw, "\x1b[4;1H\x1b[2K") || !strings.Contains(r.raw, "\x1b[5;1H\x1b[2K") {
		t.Errorf("unused header rows not cleared")
	}
}

func TestChatcovArtHeader(t *testing.T) {
	env := newMenuEnv(t)
	chatcovFakeNet(env)

	r := chatcovChat(env, env.caller, "\r2\r/q\r")
	// Everything after the scroll region is set belongs to the chat screen;
	// the pickers, which also name the network, come before it.
	_, screen, ok := strings.Cut(r.raw, "\x1b[6;21r")
	if !ok {
		t.Fatalf("scroll region never set:\n%s", chatcovText(r))
	}
	screen = testAnsiEscape.ReplaceAllString(screen, "")
	if !strings.Contains(screen, "Room: den") || !strings.Contains(screen, "Network: FakeNet") || !strings.Contains(screen, "Topic: Cats") {
		t.Errorf("header art not filled in with the room, network and topic:\n%s", screen)
	}
	if strings.Contains(screen, "@ROOM") || strings.Contains(screen, "@NET") || strings.Contains(screen, "@TOPIC") {
		t.Errorf("header art placeholders left unexpanded:\n%s", screen)
	}
}

// With no terminal size on the command context the chat layout uses the
// height recorded for the node's session, or 24 rows if there is none.
func TestChatcovScreenHeight(t *testing.T) {
	sizeless := func(c *cmdCtx, args string) (*user.User, string, error) {
		c.termWidth, c.termHeight = 0, 0
		return runChat(c, args)
	}
	for _, tc := range []struct {
		name       string
		height     int // registered session height; -1 = no session
		wantRegion string
		wantInput  string // where the prompt is drawn
	}{
		{"no session", -1, "\x1b[6;21r", "\x1b[24;1H\x1b[2K<Caller> "},
		{"session without a height", 0, "\x1b[6;21r", "\x1b[24;1H\x1b[2K<Caller> "},
		{"tall terminal", 40, "\x1b[6;37r", "\x1b[40;1H\x1b[2K<Caller> "},
		{"too short for a chat area", 6, "\x1b[6;6r", "\x1b[6;1H\x1b[2K<Caller> "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newMenuEnv(t)
			if tc.height >= 0 {
				env.e.SessionRegistry.Register(&session.BbsSession{NodeID: 1, Height: tc.height})
			}
			r := env.run(sizeless, env.caller, "", "/q\r")
			if !strings.Contains(r.raw, tc.wantRegion) {
				t.Errorf("scroll region %q not set", tc.wantRegion)
			}
			if !strings.Contains(r.raw, tc.wantInput) {
				t.Errorf("input prompt not drawn with %q", tc.wantInput)
			}
		})
	}
}

func TestChatcovSetupFailure(t *testing.T) {
	env := newMenuEnv(t)
	cfg := env.e.GetServerConfig()
	cfg.DataDir = filepath.Join(env.root, "no-such-dir")
	env.e.SetServerConfig(cfg)

	// The chat database cannot be opened: back to the menu, screen untouched.
	r := chatcovChat(env, env.caller, "hello\r/q\r")
	if r.next != "" || r.err != nil || r.raw != "" {
		t.Errorf("runChat = (%q, %v, output %q), want a silent return", r.next, r.err, r.raw)
	}

	// The same when a network join fails and the local fallback cannot open.
	net := chatcovFakeNet(env)
	net.rooms = nil
	net.joinErr = func(string) error { return errors.New("hub down") }
	r = chatcovChat(env, env.caller, "\rhello\r/q\r")
	if r.next != "" || r.err != nil || chatcovHas(r, "Joined #") {
		t.Errorf("runChat = (%q, %v), want a return without joining:\n%s", r.next, r.err, chatcovText(r))
	}
	if !net.allClosed() {
		t.Errorf("network session left open after the failed join")
	}
}

func TestChatcovNetworkPicker(t *testing.T) {
	env := newMenuEnv(t)
	net := chatcovFakeNet(env)

	// Enter takes network 1; "2" at the room prompt picks the second room.
	r := chatcovChat(env, env.caller, "\r2\rhi net\r/topic Dogs\r/msg Bob@7 yo there\r/msg Al hey\r/rooms\r/users\r/q\r")
	if r.next != "" || r.err != nil {
		t.Fatalf("runChat = (%q, %v), want a plain return", r.next, r.err)
	}
	if !chatcovHas(r, "Chat Networks", " 1. FakeNet", "(5 users online)", " 2. Local", "(this BBS only)", "Select network [1]:") {
		t.Errorf("network picker wrong:\n%s", chatcovText(r))
	}
	if !chatcovHas(r, "Active Rooms", "  3 users  General", "  2 users  no topic", "Select room [lobby]:") {
		t.Errorf("room picker wrong:\n%s", chatcovText(r))
	}
	if !chatcovHas(r, "*** Joined #den", "<Bob> earlier on", "*** server restarted", "<Caller> hi net", "Users: Caller, Bob") {
		t.Errorf("chat screen missing join, history, echo or users:\n%s", chatcovText(r))
	}
	if !chatcovHas(r, chatcovEventLines...) {
		t.Errorf("not every network event was rendered:\n%s", chatcovText(r))
	}

	chatted := net.chatted()
	if len(chatted) != 1 {
		t.Fatalf("%d network sessions joined a room, want 1", len(chatted))
	}
	svc := chatted[0]
	chatcovEqual(t, "joins", svc.log("join"), []string{"den"})
	chatcovEqual(t, "posts", svc.log("post"), []string{"den: hi net"})
	chatcovEqual(t, "topics", svc.log("topic"), []string{"den: Dogs"})
	// handle@node is split for the network; a bare handle has no node.
	chatcovEqual(t, "private messages", svc.log("private"), []string{"Bob@7: yo there", "Al@: hey"})
	chatcovEqual(t, "leaves", svc.log("leave"), []string{"den"})
	// The probe used to count users is closed too, not just the chat session.
	if len(net.sessions) != 2 || !net.allClosed() {
		t.Errorf("want 2 sessions (probe + chat), each closed once; have %d, allClosed=%v", len(net.sessions), net.allClosed())
	}
}

func TestChatcovNetworkPickerChoices(t *testing.T) {
	for _, tc := range []struct {
		name, reply string
		wantNet     bool // chat ends up on the fake network
	}{
		{"local by number", "2\r", false},
		{"out of range falls back to the first", "7\r\r", true},
		{"not a number falls back to the first", "x\r\r", true},
		{"zero falls back to the first", "0\r\r", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newMenuEnv(t)
			net := chatcovFakeNet(env)

			r := chatcovChat(env, env.caller, tc.reply+"picked\r/q\r")
			if !chatcovHas(r, "<Caller> picked") {
				t.Fatalf("chat never started:\n%s", chatcovText(r))
			}
			onNet := len(net.chatted()) == 1
			if onNet != tc.wantNet {
				t.Errorf("chatting on the network = %v, want %v", onNet, tc.wantNet)
			}
			local := chatcovLocalHistory(env, "lobby")
			if tc.wantNet {
				chatcovEqual(t, "network posts", net.chatted()[0].log("post"), []string{"lobby: picked"})
				chatcovEqual(t, "local history", local, []string{})
			} else {
				chatcovEqual(t, "local history", local, []string{"Caller: picked"})
			}
		})
	}

	// Hanging up at the network prompt ends chat before it starts.
	env := newMenuEnv(t)
	net := chatcovFakeNet(env)
	r := chatcovChat(env, env.caller, "")
	if r.next != "" || r.err != nil || chatcovHas(r, "Joined #") || len(net.chatted()) != 0 {
		t.Errorf("runChat = (%q, %v), want a return without joining:\n%s", r.next, r.err, chatcovText(r))
	}
}

func TestChatcovNetworkErrorsAreReported(t *testing.T) {
	env := newMenuEnv(t)
	net := chatcovFakeNet(env)
	net.events = nil
	net.postErr = errors.New("post boom")
	net.topicErr = errors.New("topic boom")
	net.privErr = errors.New("private boom")
	net.joinErr = func(room string) error {
		if room == "vault" {
			return errors.New("join boom")
		}
		return nil
	}

	r := chatcovChat(env, env.caller, "\r\rhi\r/topic x\r/msg Bob yo\r/join vault\r/q\r")
	if !chatcovHas(r, "Could not post: post boom", "Could not set topic: topic boom",
		"Could not send private message: private boom", "Could not join room: join boom") {
		t.Errorf("service errors not all reported:\n%s", chatcovText(r))
	}
	// A post that failed is not echoed as if it had been sent.
	if chatcovHas(r, "<Caller> hi") {
		t.Errorf("failed post echoed to the chat area:\n%s", chatcovText(r))
	}

	// A failing room list is reported by /rooms, and at the picker just
	// means "no rooms": straight into the lobby without a prompt.
	net.roomsErr = errors.New("rooms boom")
	r = chatcovChat(env, env.caller, "\r/rooms\r/q\r")
	if !chatcovHas(r, "*** Joined #lobby", "Could not list rooms: rooms boom") {
		t.Errorf("room list failure not handled:\n%s", chatcovText(r))
	}
	if chatcovHas(r, "Select room") {
		t.Errorf("room prompt shown although the room list failed:\n%s", chatcovText(r))
	}
	if !chatcovHas(r, "(0 users online)") {
		t.Errorf("network with no readable room list should count 0 users:\n%s", chatcovText(r))
	}
}

// A network that cannot be joined does not cost the caller their chat: the
// session falls back to the local service in the same room.
func TestChatcovJoinFailureFallsBackToLocal(t *testing.T) {
	env := newMenuEnv(t)
	net := chatcovFakeNet(env)
	net.rooms = nil
	net.joinErr = func(string) error { return errors.New("hub down") }

	r := chatcovChat(env, env.caller, "\rstill here\r/q\r")
	if r.next != "" || !chatcovHas(r, "*** Joined #lobby", "<Caller> still here") {
		t.Fatalf("fallback chat did not run (next=%q):\n%s", r.next, chatcovText(r))
	}
	chatcovEqual(t, "local history", chatcovLocalHistory(env, "lobby"), []string{"Caller: still here"})
	if chatted := net.chatted(); len(chatted) != 1 || len(chatted[0].log("post")) != 0 {
		t.Errorf("message went to the network that refused the join")
	}
	if !net.allClosed() {
		t.Errorf("refusing network session was not closed")
	}
}

func TestChatcovNetworkCommand(t *testing.T) {
	env := newMenuEnv(t)
	net := chatcovFakeNet(env)

	// Start on Local, then hop to the network and its second room.
	r := chatcovChat(env, env.caller, "2\rbefore\r/network\r1\r2\rafter\r/q\r")
	if r.next != "" || r.err != nil {
		t.Fatalf("runChat = (%q, %v), want a plain return", r.next, r.err)
	}
	if !chatcovHas(r, "*** Chat Networks:", " 1. FakeNet         (5 users online)", " 2. Local           (this BBS only)",
		"*** Available Rooms:", " 1. lobby        (3) General", " 2. den          (2) no topic") {
		t.Errorf("/network pickers wrong:\n%s", chatcovText(r))
	}
	// Both questions are asked on the input row, in place of the chat prompt.
	for _, prompt := range []string{"Select network [1]: ", "Select room [lobby]: "} {
		if !strings.Contains(r.raw, "\x1b[24;1H\x1b[2K"+prompt) {
			t.Errorf("prompt %q not shown on the input row", prompt)
		}
	}
	if !chatcovHas(r, "*** Joined #lobby", "*** Joined #den", "<Bob> earlier on", "<Caller> after") {
		t.Errorf("chat did not carry on in the new room:\n%s", chatcovText(r))
	}
	// The event pump is restarted on the new service.
	if !chatcovHas(r, chatcovEventLines...) {
		t.Errorf("events from the new network were not rendered:\n%s", chatcovText(r))
	}
	// The chat rows are wiped for the new room.
	for _, row := range []int{6, 13, 21} {
		if !strings.Contains(r.raw, fmt.Sprintf("\x1b[%d;1H\x1b[2K", row)) {
			t.Errorf("chat row %d not cleared after switching network", row)
		}
	}

	chatcovEqual(t, "local history", chatcovLocalHistory(env, "lobby"), []string{"Caller: before"})
	chatted := net.chatted()
	if len(chatted) != 1 {
		t.Fatalf("%d network sessions joined a room, want 1", len(chatted))
	}
	chatcovEqual(t, "network joins", chatted[0].log("join"), []string{"den"})
	chatcovEqual(t, "network posts", chatted[0].log("post"), []string{"den: after"})
	if !net.allClosed() {
		t.Errorf("a network session was left open")
	}
	if rooms, _ := chatcovLocal(env, "observer").Rooms(); len(rooms) != 0 {
		t.Errorf("caller still in a local room after moving network: %+v", rooms)
	}
}

func TestChatcovNetworkCommandChoices(t *testing.T) {
	for _, tc := range []struct {
		name           string
		network, room  string // replies to the two /network prompts
		wantNet        bool
		wantRoom       string
		wantRoomPrompt bool
	}{
		{"Enter twice: first network, lobby", "\r", "\r", true, "lobby", true},
		{"room by name", "1\r", " zed \r", true, "zed", true},
		{"room number past the list is a name", "1\r", "9\r", true, "9", true},
		{"out-of-range network is the first", "9\r", "1\r", true, "lobby", true},
		{"non-numeric network is the first", "x\r", "1\r", true, "lobby", true},
		{"local has no rooms to pick from", "2\r", "", false, "lobby", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newMenuEnv(t)
			net := chatcovFakeNet(env)
			net.events = nil

			r := chatcovChat(env, env.caller, "2\r/network\r"+tc.network+tc.room+"moved\r/q\r")
			if !chatcovHas(r, "<Caller> moved") {
				t.Fatalf("chat did not resume after /network:\n%s", chatcovText(r))
			}
			if got := strings.Contains(r.raw, "\x1b[2KSelect room [lobby]: "); got != tc.wantRoomPrompt {
				t.Errorf("room prompt shown = %v, want %v", got, tc.wantRoomPrompt)
			}
			if tc.wantNet {
				if len(net.chatted()) != 1 {
					t.Fatalf("not chatting on the network")
				}
				chatcovEqual(t, "network posts", net.chatted()[0].log("post"), []string{tc.wantRoom + ": moved"})
			} else {
				if len(net.chatted()) != 0 {
					t.Errorf("joined the network although Local was chosen")
				}
				chatcovEqual(t, "local history", chatcovLocalHistory(env, tc.wantRoom), []string{"Caller: moved"})
			}
		})
	}
}

func TestChatcovNetworkCommandWithoutNetworks(t *testing.T) {
	env := newMenuEnv(t)

	// No V3Net leaves: Local is the only entry, and picking it rejoins.
	r := chatcovChat(env, env.caller, "one\r/network\r\rtwo\r/q\r")
	if !chatcovHas(r, "*** Chat Networks:", " 1. Local           (this BBS only)", "<Caller> two") {
		t.Errorf("/network on a local-only board wrong:\n%s", chatcovText(r))
	}
	if n := strings.Count(chatcovText(r), "*** Joined #lobby"); n != 2 {
		t.Errorf("lobby joined %d times, want 2 (start + after /network)", n)
	}
	// Rejoining scrolls the room's history back in, including the caller's own line.
	if n := strings.Count(chatcovText(r), "<Caller> one"); n != 2 {
		t.Errorf("first message shown %d times, want 2 (echo + scrollback)", n)
	}
	chatcovEqual(t, "local history", chatcovLocalHistory(env, "lobby"), []string{"Caller: one", "Caller: two"})

	// Hanging up at the network prompt still leaves cleanly.
	r = chatcovChat(env, env.caller, "/network\r")
	if r.next != "LOGOFF" || !strings.HasSuffix(r.raw, "\x1b[r") {
		t.Errorf("disconnect at the /network prompt: next=%q, want LOGOFF with the scroll region reset", r.next)
	}
	if rooms, _ := chatcovLocal(env, "observer").Rooms(); len(rooms) != 0 {
		t.Errorf("rooms still occupied after the disconnect: %+v", rooms)
	}
}

func TestChatcovNetworkCommandJoinFailure(t *testing.T) {
	env := newMenuEnv(t)
	net := chatcovFakeNet(env)
	net.events = nil
	net.joinErr = func(string) error { return errors.New("hub down") }

	// The network refuses the join: the room is joined locally instead.
	r := chatcovChat(env, env.caller, "2\r/network\r1\rden\rmade it\r/q\r")
	if !chatcovHas(r, "Could not join room: hub down", "*** Joined #den", "<Caller> made it") {
		t.Errorf("local fallback after a refused /network join wrong:\n%s", chatcovText(r))
	}
	chatcovEqual(t, "local den history", chatcovLocalHistory(env, "den"), []string{"Caller: made it"})
	if !net.allClosed() {
		t.Errorf("refusing network session was not closed")
	}

	// A room the local service will not take either ends the chat session.
	r = chatcovChat(env, env.caller, "2\r/network\r1\rNo!Room\rnever sent\r/q\r")
	if r.next != "" || r.err != nil {
		t.Errorf("runChat = (%q, %v), want a plain return", r.next, r.err)
	}
	if chatcovHas(r, "<Caller> never sent") || !strings.HasSuffix(r.raw, "\x1b[r") {
		t.Errorf("chat carried on, or left the scroll region set, after both joins failed:\n%s", chatcovText(r))
	}
	if rooms, _ := chatcovLocal(env, "observer").Rooms(); len(rooms) != 0 {
		t.Errorf("rooms still occupied after the failed switch: %+v", rooms)
	}
}

func TestChatcovNetworkCommandNoSession(t *testing.T) {
	env := newMenuEnv(t)
	net := chatcovFakeNet(env)
	// Calls 1 and 2 are the two pickers' probes; the third would be the
	// session to chat on, and the network fails to provide one.
	net.nilFrom = 3

	r := chatcovChat(env, env.caller, "2\r/network\r1\rnever sent\r/q\r")
	if r.next != "" || r.err != nil {
		t.Errorf("runChat = (%q, %v), want a plain return", r.next, r.err)
	}
	if chatcovHas(r, "<Caller> never sent") || !strings.HasSuffix(r.raw, "\x1b[r") {
		t.Errorf("chat carried on, or left the scroll region set, without a session:\n%s", chatcovText(r))
	}
}

// chatcovReadLine feeds input to chatReadLine on row 24 and returns the line,
// everything it drew and the error.
func chatcovReadLine(t *testing.T, input string, width int, prompt string) (string, string, error) {
	t.Helper()
	ts := newTestSession(input)
	t.Cleanup(func() { resetSessionIH(ts) })
	var mu sync.Mutex
	var out strings.Builder
	line, err := chatReadLine(ts, &mu, func(b []byte) { out.Write(b) }, 24, width, prompt)
	return line, out.String(), err
}

func TestChatcovReadLine(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		width       int
		want        string
	}{
		{"plain line", "hello\r", 80, "hello"},
		{"surrounding spaces trimmed", "  hi  \r", 80, "hi"},
		{"backspace and DEL erase", "abx\x08y\x7fc\r", 80, "abc"},
		{"backspace on an empty line", "\x08\x08ok\r", 80, "ok"},
		{"arrow keys are not text", "a\x1b[A\x1b[Db\r", 80, "ab"},
		{"input stops one short of the width", "abcdefgh\r", 6, "abc"}, // 6 - len("> ") - 1
		{"always room for one character", "xyz\r", 1, "x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := chatcovReadLine(t, tc.input, tc.width, "> ")
			if err != nil || got != tc.want {
				t.Errorf("chatReadLine(%q) = (%q, %v), want %q", tc.input, got, err, tc.want)
			}
		})
	}

	// Each keystroke redraws the input row with the cursor after the text;
	// Enter leaves the row showing just the prompt.
	_, out, _ := chatcovReadLine(t, "ab\r", 80, "> ")
	for _, want := range []string{
		"\x1b[24;1H\x1b[2K> \x1b[24;3H",
		"\x1b[24;1H\x1b[2K> a\x1b[24;4H",
		"\x1b[24;1H\x1b[2K> ab\x1b[24;5H",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("redraw %q missing from %q", want, out)
		}
	}
	if !strings.HasSuffix(out, "\x1b[24;1H\x1b[2K> \x1b[24;3H") {
		t.Errorf("input row not cleared back to the prompt after Enter: %q", out)
	}

	// Anything that abandons the line reads as a disconnect.
	for name, input := range map[string]string{
		"Ctrl-C": "abc\x03", "Ctrl-A": "abc\x01", "ESC": "abc\x1b", "end of input": "abc",
	} {
		if got, _, err := chatcovReadLine(t, input, 80, "> "); !errors.Is(err, io.EOF) || got != "" {
			t.Errorf("%s: chatReadLine = (%q, %v), want io.EOF", name, got, err)
		}
	}
}

func TestChatcovPipeDisplayLen(t *testing.T) {
	for in, want := range map[string]int{
		"":                 0,
		"plain":            5,
		"|15hi|07":         2,
		"|07":              0,
		"a|b":              3, // a pipe that is not a colour code is text
		"|1":               2,
		"|x5ab":            5,
		"|08[|15C|08] |14": 4,
	} {
		if got := pipeDisplayLen(in); got != want {
			t.Errorf("pipeDisplayLen(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestChatcovWrapPipeText(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		width      int
		want       []string
	}{
		{"fits", "aaa bbb", 7, []string{"aaa bbb"}},
		{"fits once colour codes are discounted", "|15aaa|07 |12bbb|07", 7, []string{"|15aaa|07 |12bbb|07"}},
		{"wraps at a word boundary", "aaa bbb ccc", 7, []string{"aaa bbb", ">> ccc"}},
		{"continuation prefix counts against the width", "aaa bbb ccc dd", 7, []string{"aaa bbb", ">> ccc", ">> dd"}},
		{"colour codes do not count when wrapping", "|15aaa|07 |12bbb|07 ccc", 7, []string{"|15aaa|07 |12bbb|07", ">> ccc"}},
		{"a word longer than the width is not split", "abcdefghij", 4, []string{"abcdefghij"}},
		{"nothing but spaces", "          ", 4, []string{"          "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chatcovEqual(t, "wrapPipeText", wrapPipeText(tc.text, tc.width, ">> ", 3), tc.want)
		})
	}
}

func TestChatcovFormatChatMessage(t *testing.T) {
	const sysFmt, userFmt = "*** %s", "<%s> %s"
	if got := formatChatMessage(chat.ChatMessage{Handle: "Bob", Text: "hi"}, sysFmt, userFmt); got != "<Bob> hi" {
		t.Errorf("user message = %q, want %q", got, "<Bob> hi")
	}
	// A system message has no author to show, whatever Handle holds.
	if got := formatChatMessage(chat.ChatMessage{Handle: "Bob", Text: "hub restarted", IsSystem: true}, sysFmt, userFmt); got != "*** hub restarted" {
		t.Errorf("system message = %q, want %q", got, "*** hub restarted")
	}
}
