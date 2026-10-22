package menu

import (
	"strings"
	"testing"
)

// TestChatSlashCommandsStayLocal covers #659: a line starting with / is a
// command, and one that is unknown or incomplete is answered on the caller's
// screen instead of being posted to the room. Each row is one line typed on a
// network chat, with what it must show and what, if anything, it must post.
func TestChatSlashCommandsStayLocal(t *testing.T) {
	for _, tc := range []struct {
		name, line string
		show       []string // text that must appear in the chat area
		posts      []string // what the network receives, as "room: text"
	}{
		{"/? lists the commands", "/?", []string{"/join <room>", "/topic <text>", "/msg <user> <text>", "/rooms", "/users", "/network", "/q"}, nil},
		{"/help lists the commands", "/help", []string{"/join <room>", "/q"}, nil},
		{"/HELP is case-insensitive", "/HELP", []string{"/join <room>"}, nil},
		{"unknown command", "/networks", []string{"*** Unknown command /networks - type /? for help."}, nil},
		{"unknown command keeps its arguments private", "/shrug a b c", []string{"*** Unknown command /shrug - type /? for help."}, nil},
		{"lone slash", "/", []string{"*** Unknown command / - type /? for help."}, nil},
		{"bare /topic", "/topic", []string{"*** Usage: /topic <text>"}, nil},
		{"bare /join", "/JOIN", []string{"*** Usage: /join <room>"}, nil},
		{"/join with only spaces", "/join   ", []string{"*** Usage: /join <room>"}, nil},
		{"bare /msg", "/msg", []string{"*** Usage: /msg <user>[@node] <text>"}, nil},
		{"/msg without text", "/msg Bob", []string{"*** Usage: /msg <user>[@node] <text>"}, nil},
		{"double slash sends a literal slash", "//shrug", nil, []string{"lobby: /shrug"}},
		{"double slash alone sends a slash", "//", nil, []string{"lobby: /"}},
		{"plain text is posted", "hello", []string{"<Caller> hello"}, []string{"lobby: hello"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newMenuEnv(t)
			net := chatcovFakeNet(env)
			net.events = nil

			r := chatcovChat(env, env.caller, "\r\r"+tc.line+"\r/q\r")
			if r.next != "" || r.err != nil {
				t.Fatalf("runChat = (%q, %v), want a plain return", r.next, r.err)
			}
			for _, want := range tc.show {
				if !chatcovHas(r, want) {
					t.Errorf("%q not shown:\n%s", want, chatcovText(r))
				}
			}
			chatted := net.chatted()
			if len(chatted) != 1 {
				t.Fatalf("%d network sessions joined a room, want 1", len(chatted))
			}
			want := tc.posts
			if want == nil {
				want = []string{}
			}
			chatcovEqual(t, "posts", chatted[0].log("post"), want)
			if strings.HasPrefix(tc.line, "/") && !strings.HasPrefix(tc.line, "//") {
				chatcovEqual(t, "topics", chatted[0].log("topic"), []string{})
				chatcovEqual(t, "private messages", chatted[0].log("private"), []string{})
				chatcovEqual(t, "joins", chatted[0].log("join"), []string{"lobby"})
			}
		})
	}
}
