package menu

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// chatModNet is a network, home node "home", with Eve on Other BBS in the
// room beside the caller and Bob.
func chatModNet(env *menuEnv) *chatcovNet {
	net := chatcovFakeNet(env)
	net.node = "home"
	net.events = nil
	net.history = nil
	net.users = []chat.ChatUser{
		{Handle: "Caller", Node: "home", BBS: "My BBS"},
		{Handle: "Bob", Node: "home", BBS: "My BBS"},
		{Handle: "Eve", Node: "other", BBS: "Other BBS"},
	}
	return net
}

// eveLine is a room line, private message, join or leave from Eve.
func eveLine(kind chat.ChatEventType, text string) chat.ChatEvent {
	eve := chat.ChatMessage{Handle: "Eve", Node: "other", BBS: "Other BBS", Text: text}
	switch kind {
	case chat.TypeJoin:
		return chat.ChatEvent{Type: kind, Join: &chat.ChatJoin{Room: "lobby", Handle: "Eve", Node: "other", BBS: "Other BBS"}}
	case chat.TypeLeave:
		return chat.ChatEvent{Type: kind, Leave: &chat.ChatLeave{Room: "lobby", Handle: "Eve", Node: "other", BBS: "Other BBS"}}
	}
	return chat.ChatEvent{Type: kind, Message: &eve}
}

// /mute hides a caller's room lines, private messages and join and leave
// notices for this session, named as the user list names them; /unmute
// shows them again; /ignored says who is muted. Nothing is stored.
func TestChatMuteHidesCallerThisSession(t *testing.T) {
	env := newMenuEnv(t)
	net := chatModNet(env)

	live := startChatLive(env, env.caller)
	live.waitOutput("Select network [1]:")
	live.typeKeys("1\r")
	live.waitOutput("Select room [lobby]:")
	live.typeKeys("\r")
	live.waitOutput("Joined #lobby")
	svc := net.chatted()[0]

	live.typeKeys("/mute Eve@Other BBS\r")
	live.waitOutput("Muted Eve@Other BBS for this session")
	for _, ev := range []chat.ChatEvent{eveLine(chat.TypeMessage, "hidden line"), eveLine(chat.TypePrivate, "hidden psst"), eveLine(chat.TypeJoin, ""), eveLine(chat.TypeLeave, "")} {
		svc.events <- ev
	}
	// Bob's line still shows, and proves Eve's were handled before it.
	svc.events <- chat.ChatEvent{Type: chat.TypeMessage, Message: &chat.ChatMessage{Handle: "Bob", Node: "home", Text: "still here"}}
	live.waitOutput("<Bob> still here")
	live.typeKeys("/ignored\r")
	live.waitOutput("Muted this session: Eve@Other BBS")
	live.typeKeys("/unmute eve\r")
	live.waitOutput("No longer muting Eve@Other BBS")
	svc.events <- eveLine(chat.TypeMessage, "back again")
	live.waitOutput("<Eve@Other BBS> back again")
	live.typeKeys("/q\r")
	<-live.done

	out := testAnsiEscape.ReplaceAllString(live.s.output(), "")
	for _, hidden := range []string{"hidden line", "hidden psst", "Eve@Other BBS has joined", "Eve@Other BBS has left"} {
		if strings.Contains(out, hidden) {
			t.Errorf("%q shown while Eve was muted:\n%s", hidden, out)
		}
	}
	stored, _ := env.um.GetUserByID(env.caller.ID)
	if len(stored.TwitList) != 0 {
		t.Errorf("a mute was stored: %+v", stored.TwitList)
	}
}

// /block puts the caller on the Twit List as handle@node-id and hides
// them from then on, history included; /unblock takes them off. A Twit
// entry made elsewhere hides them in chat the same way.
func TestChatBlockUsesTwitList(t *testing.T) {
	env := newMenuEnv(t)
	net := chatModNet(env)
	net.history = []chat.ChatMessage{{Handle: "Eve", Node: "other", BBS: "Other BBS", Text: "from before"}, {Handle: "Bob", Node: "home", Text: "bob before"}}

	r := chatcovChat(env, env.caller, "\r\r/block Eve@Other BBS\r/ignored\r/block Eve@Other BBS\r/unblock Eve@Other BBS\r/unblock Eve@Other BBS\r/q\r")
	for _, want := range []string{"<Eve@Other BBS> from before", "Blocked Eve@Other BBS", "Blocked (your Twit List): Eve@other", "Eve@Other BBS is already blocked", "Unblocked Eve@Other BBS", "Eve@Other BBS is not on your Twit List"} {
		if !chatcovHas(r, want) {
			t.Errorf("%q not shown:\n%s", want, chatcovText(r))
		}
	}
	stored, _ := env.um.GetUserByID(env.caller.ID)
	if len(stored.TwitList) != 0 {
		t.Errorf("Twit List after unblock: %+v", stored.TwitList)
	}

	// Blocked before the call: Eve's history and live lines are hidden.
	if _, err := env.um.SetCentralConsent(env.caller.ID, func(*user.User) {}); err != nil {
		t.Fatal(err)
	}
	if err := saveTwitList(env.um, env.caller, []user.TwitEntry{{Name: "Eve", Addr: "other"}}); err != nil {
		t.Fatal(err)
	}
	net.events = []chat.ChatEvent{eveLine(chat.TypeMessage, "live hidden"), {Type: chat.TypeMessage, Message: &chat.ChatMessage{Handle: "Bob", Node: "home", Text: "bob live"}}}
	r = chatcovChat(env, env.caller, "\r\r/q\r")
	if chatcovHas(r, "from before") || chatcovHas(r, "live hidden") || !chatcovHas(r, "bob before", "<Bob> bob live") {
		t.Errorf("blocked caller's lines shown, or Bob's hidden:\n%s", chatcovText(r))
	}
	if got := net.chatted(); len(got) != 2 || len(got[1].log("post")) != 0 {
		t.Errorf("sessions chatted = %d", len(got))
	}
}

// Naming the caller themselves, or nobody, is refused; a caller not in
// the room is named handle@node-id on a network.
func TestChatWhoTarget(t *testing.T) {
	users := []chat.ChatUser{{Handle: "Caller", Node: "home"}, {Handle: "Eve", Node: "b", BBS: "Beta"}, {Handle: "Eve", Node: "g", BBS: "Gamma"}, {Handle: "Pat Twin", Node: "home"}}
	for _, tc := range []struct {
		arg, home    string
		handle, node string
		wantErr      string
	}{
		{"Eve@Gamma", "home", "Eve", "g", ""},
		{"pat twin", "home", "Pat Twin", "home", ""},
		{"Eve", "home", "", "", "more than one board"},
		{"Eve@Gamma hi", "home", "", "", "no one here is named"},
		{"Zed@zz", "home", "Zed", "zz", ""},
		{"Zed", "home", "", "", "handle@node-id"},
		{"Zed", "", "Zed", "", ""},
	} {
		who, err := chatWhoTarget(tc.arg, users, tc.home)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("%q: err = %v, want %q", tc.arg, err, tc.wantErr)
			}
			continue
		}
		if err != nil || who.Handle != tc.handle || who.Node != tc.node {
			t.Errorf("%q = (%+v, %v), want %s@%s", tc.arg, who, err, tc.handle, tc.node)
		}
	}
}

// A caller the SysOp barred gets no network: CHAT NET says so and ends,
// CHAT says so and runs local chat, a session in a room ends at the next
// line, and Preferences shows the switch as the SysOp's.
func TestChatSysopBar(t *testing.T) {
	env := newMenuEnv(t)
	net := chatModNet(env)
	bar := func(barred bool) {
		t.Helper()
		if _, err := env.um.SetCentralConsent(env.caller.ID, func(x *user.User) { x.CentralChatBarred = barred }); err != nil {
			t.Fatal(err)
		}
	}
	bar(true)

	r := env.run(runChat, env.caller, "NET", "")
	if !chatcovHas(r, "your SysOp has turned it off") || chatcovHas(r, "Select network") {
		t.Errorf("CHAT NET for a barred caller:\n%s", chatcovText(r))
	}
	r = chatcovChat(env, env.caller, "hello\r/q\r")
	if !chatcovHas(r, "this chat is this board only", "<Caller> hello") || chatcovHas(r, "Select network") {
		t.Errorf("CHAT for a barred caller should be local:\n%s", chatcovText(r))
	}
	if len(net.chatted()) != 0 {
		t.Fatal("barred caller reached the network")
	}

	fake := &centralFake{nets: []CentralNetworkInfo{chatNet("testnet")}}
	stored, _ := env.um.GetUserByID(env.caller.ID)
	out := runPrefs(t, fake, stored, "4\rq\r")
	if !strings.Contains(out, "set by the SysOp") || !strings.Contains(out, "has turned network chat off for you") {
		t.Errorf("Preferences for a barred caller:\n%s", out)
	}

	// Barred mid-room, from the user editor on another node.
	bar(false)
	live := startChatLive(env, env.caller)
	live.waitOutput("Select network [1]:")
	live.typeKeys("1\r")
	live.waitOutput("Select room [lobby]:")
	live.typeKeys("\r")
	live.waitOutput("Joined #lobby")
	bar(true)
	live.typeKeys("anyone?\r")
	live.waitOutput("Your SysOp has turned network chat off for you; leaving.")
	select {
	case <-live.done:
	case <-time.After(handlerWait):
		t.Fatal("chat did not end after the bar")
	}
	if posts := net.chatted()[0].log("post"); len(posts) != 0 {
		t.Errorf("posted after the bar: %v", posts)
	}
}
