package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
)

// Callers on this board, and everyone in local chat or on a hub that
// predates members, are named by bare handle; callers on other boards as
// handle@Board, with the node ID when the hub has no name for the board.
func TestChatUserName(t *testing.T) {
	for _, tc := range []struct {
		handle, node, bbs, home, want string
	}{
		{"Bob", "", "", "", "Bob"},
		{"Bob", "home", "My BBS", "home", "Bob"},
		{"Bob", "", "", "home", "Bob"},
		{"Bob", "other", "Other BBS", "home", "Bob@Other BBS"},
		{"Bob", "other", "", "home", "Bob@other"},
	} {
		if got := chatUserName(tc.handle, tc.node, tc.bbs, tc.home); got != tc.want {
			t.Errorf("chatUserName(%q, %q, %q, %q) = %q, want %q", tc.handle, tc.node, tc.bbs, tc.home, got, tc.want)
		}
	}
	users := []chat.ChatUser{{Handle: "Caller", Node: "home"}, {Handle: "Eve", Node: "other", BBS: "Other BBS"}}
	if got := chatUserList(users, "home"); got != "Caller, Eve@Other BBS" {
		t.Errorf("user list = %q", got)
	}
}

// /msg names its recipient as the user list does, so what the caller sees
// is what they type, spaces and all; a bare handle reaches a caller on
// another board when nobody here has it, and is refused when several boards
// in the room share it. A recipient not in the room goes to the hub as
// before, as a word with an optional @node.
func TestChatMsgTarget(t *testing.T) {
	users := []chat.ChatUser{
		{Handle: "Caller", Node: "home", BBS: "My BBS"},
		{Handle: "Bob", Node: "home", BBS: "My BBS"},
		{Handle: "Bob", Node: "b", BBS: "Beta BBS"},
		{Handle: "Eve", Node: "b", BBS: "Beta BBS"},
		{Handle: "Eve", Node: "g", BBS: "Gamma"},
		{Handle: "Pat Twin", Node: "home", BBS: "My BBS"},
		{Handle: "Zed", Node: "z"},
	}
	for _, tc := range []struct {
		arg                string
		handle, node, text string
		usage              bool
		ambiguous          string // text the refusal must contain
	}{
		{arg: "Bob hi", handle: "Bob", node: "home", text: "hi"},
		{arg: "bob@beta bbs hi there", handle: "Bob", node: "b", text: "hi there"},
		{arg: "Eve hi", ambiguous: "Eve@Beta BBS, Eve@Gamma"},
		{arg: "Eve@Gamma hi", handle: "Eve", node: "g", text: "hi"},
		{arg: "Pat Twin hello", handle: "Pat Twin", node: "home", text: "hello"},
		{arg: "Zed@z yo", handle: "Zed", node: "z", text: "yo"},
		{arg: "zed yo", handle: "Zed", node: "z", text: "yo"},
		{arg: "Bobby hi", handle: "Bobby", node: "", text: "hi"},
		{arg: "Al@7 yo", handle: "Al", node: "7", text: "yo"},
		{arg: "Al hey", handle: "Al", node: "", text: "hey"},
		{arg: "Bob", usage: true},
		{arg: "Bob@Beta BBS", usage: true},
		{arg: "Al", usage: true},
		{arg: "", usage: true},
	} {
		handle, node, text, err := chatMsgTarget(tc.arg, users, "home")
		switch {
		case tc.usage:
			if err != errChatMsgUsage {
				t.Errorf("%q: err = %v, want usage", tc.arg, err)
			}
		case tc.ambiguous != "":
			if err == nil || !strings.Contains(err.Error(), tc.ambiguous) {
				t.Errorf("%q: err = %v, want a refusal naming %q", tc.arg, err, tc.ambiguous)
			}
		case err != nil || handle != tc.handle || node != tc.node || text != tc.text:
			t.Errorf("%q = (%q, %q, %q, %v), want (%q, %q, %q)", tc.arg, handle, node, text, err, tc.handle, tc.node, tc.text)
		}
	}
	// Two boards with the same name: the node IDs tell the choices apart.
	twins := []chat.ChatUser{{Handle: "Eve", Node: "b1", BBS: "Beta"}, {Handle: "Eve", Node: "b2", BBS: "Beta"}}
	if _, _, _, err := chatMsgTarget("Eve hi", twins, "home"); err == nil || !strings.Contains(err.Error(), "Eve@b1, Eve@b2") {
		t.Errorf("same-named boards: %v", err)
	}
	// Names that differ only in case match the same recipients, so they
	// are told apart by node too.
	cased := []chat.ChatUser{{Handle: "Bob", Node: "b1", BBS: "Beta"}, {Handle: "bob", Node: "b2", BBS: "Beta"}}
	if _, _, _, err := chatMsgTarget("bob hi", cased, "home"); err == nil || !strings.Contains(err.Error(), "Bob@b1, bob@b2") {
		t.Errorf("same-named boards, handles differing in case: %v", err)
	}
	if handle, node, _, err := chatMsgTarget("bob@b2 hi", cased, "home"); err != nil || handle != "bob" || node != "b2" {
		t.Errorf("suggested choice = (%q, %q, %v), want bob on b2", handle, node, err)
	}
}

// On network chat the user list, history, live lines, private messages and
// join and leave notices name callers on other boards with their board;
// this board's callers are bare. /msg takes those names.
func TestChatShowsBoardsOfOtherCallers(t *testing.T) {
	env := newMenuEnv(t)
	net := chatcovFakeNet(env)
	net.node = "home"
	net.users = []chat.ChatUser{
		{Handle: "Caller", Node: "home", BBS: "My BBS"},
		{Handle: "Bob", Node: "home", BBS: "My BBS"},
		{Handle: "Eve", Node: "other", BBS: "Other BBS"},
	}
	net.history = []chat.ChatMessage{
		{Handle: "Eve", Node: "other", BBS: "Other BBS", Text: "from afar"},
		{Handle: "Bob", Node: "home", BBS: "My BBS", Text: "from here"},
	}
	net.events = []chat.ChatEvent{
		{Type: chat.TypeMessage, Message: &chat.ChatMessage{Handle: "Eve", Node: "other", BBS: "Other BBS", Text: "live"}},
		{Type: chat.TypePrivate, Message: &chat.ChatMessage{Handle: "Eve", Node: "other", BBS: "Other BBS", Text: "psst"}},
		{Type: chat.TypeJoin, Join: &chat.ChatJoin{Room: "lobby", Handle: "Zed", Node: "z"}},
		{Type: chat.TypeLeave, Leave: &chat.ChatLeave{Room: "lobby", Handle: "Bob", Node: "home", BBS: "My BBS"}},
	}

	r := chatcovChat(env, env.caller, "\r\r/msg Eve@Other BBS yo\r/msg bob hi\r/msg eve again\r/q\r")
	if r.next != "" || r.err != nil {
		t.Fatalf("runChat = (%q, %v), want a plain return", r.next, r.err)
	}
	for _, want := range []string{
		"Users: Caller, Bob, Eve@Other BBS",
		"<Eve@Other BBS> from afar", "<Bob> from here",
		"<Eve@Other BBS> live", "*** Eve@Other BBS -> you: psst",
		"*** Zed@z has joined lobby", "*** Bob has left lobby",
	} {
		if !chatcovHas(r, want) {
			t.Errorf("%q not shown:\n%s", want, chatcovText(r))
		}
	}
	chatted := net.chatted()
	if len(chatted) != 1 {
		t.Fatalf("%d network sessions joined a room, want 1", len(chatted))
	}
	chatcovEqual(t, "private messages", chatted[0].log("private"), []string{"Eve@other: yo", "Bob@home: hi", "Eve@other: again"})
}
