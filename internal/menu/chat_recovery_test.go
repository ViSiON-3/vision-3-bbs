package menu

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
)

// chatLastHeader returns everything drawn from the last header on, starting at
// its room label (the first placeholder in the art), so a test can check what
// the header says at the end of the session. The pickers, which also name the
// networks, come before the scroll region is set and are skipped.
func chatLastHeader(t *testing.T, r runResult) string {
	t.Helper()
	_, screen, ok := strings.Cut(r.raw, "\x1b[6;21r")
	if !ok {
		t.Fatalf("scroll region never set:\n%s", chatcovText(r))
	}
	screen = testAnsiEscape.ReplaceAllString(screen, "")
	i := strings.LastIndex(screen, "Room: ")
	if i < 0 {
		t.Fatalf("no header drawn:\n%s", screen)
	}
	return screen[i:]
}

// A network whose probe fails, times out or yields no session is listed as
// unavailable by both pickers, and choosing it lands the caller in local chat.
func TestChatProbeFailureMarksNetworkUnavailable(t *testing.T) {
	oldTimeout := chatProbeTimeout
	chatProbeTimeout = 50 * time.Millisecond
	t.Cleanup(func() { chatProbeTimeout = oldTimeout })

	for _, tc := range []struct {
		name     string
		breakNet func(t *testing.T, n *chatcovNet)
	}{
		{"room list error", func(t *testing.T, n *chatcovNet) { n.roomsErr = errors.New("hub down") }},
		{"room list timeout", func(t *testing.T, n *chatcovNet) {
			n.roomsBlock = make(chan struct{})
			t.Cleanup(func() { close(n.roomsBlock) })
		}},
		// Before the fix this panicked in the probe goroutine.
		{"no session", func(t *testing.T, n *chatcovNet) { n.nilFrom = 1 }},
	} {
		t.Run(tc.name+", initial picker", func(t *testing.T) {
			env := newMenuEnv(t)
			net := chatcovFakeNet(env)
			net.events = nil
			tc.breakNet(t, net)

			r := chatcovChat(env, env.caller, "1\rhello\r/q\r")
			if !chatcovHas(r, " 1. FakeNet", "unavailable", "Network unavailable, falling back to local.", "<Caller> hello") {
				t.Errorf("unavailable network not listed or not refused:\n%s", chatcovText(r))
			}
			if chatcovHas(r, "users online") {
				t.Errorf("unavailable network listed with a user count:\n%s", chatcovText(r))
			}
			if len(net.chatted()) != 0 {
				t.Errorf("joined a room on the unavailable network")
			}
			chatcovEqual(t, "local history", chatcovLocalHistory(env, "lobby"), []string{"Caller: hello"})
			if h := chatLastHeader(t, r); !strings.Contains(h, "Network: Local") {
				t.Errorf("header = %q, want the Local network", h)
			}
		})
		t.Run(tc.name+", /network", func(t *testing.T) {
			env := newMenuEnv(t)
			net := chatcovFakeNet(env)
			net.events = nil
			tc.breakNet(t, net)

			r := chatcovChat(env, env.caller, "2\rbefore\r/network\r1\rafter\r/q\r")
			if !chatcovHas(r, " 1. FakeNet         (unavailable)", "*** Network unavailable, using local.", "<Caller> after") {
				t.Errorf("unavailable network not listed or not refused by /network:\n%s", chatcovText(r))
			}
			if len(net.chatted()) != 0 {
				t.Errorf("joined a room on the unavailable network")
			}
			chatcovEqual(t, "local history", chatcovLocalHistory(env, "lobby"), []string{"Caller: before", "Caller: after"})
		})
	}
}

// When the chosen network refuses the join and chat falls back to the local
// service, the header names Local rather than the network that refused.
func TestChatJoinFallbackHeaderShowsLocal(t *testing.T) {
	t.Run("initial join", func(t *testing.T) {
		env := newMenuEnv(t)
		net := chatcovFakeNet(env)
		net.events = nil
		net.rooms = nil
		net.joinErr = func(string) error { return errors.New("hub down") }

		r := chatcovChat(env, env.caller, "\r/q\r")
		if h := chatLastHeader(t, r); !strings.Contains(h, "Network: Local") {
			t.Errorf("header = %q, want the Local network after the fallback", h)
		}
	})
	t.Run("/network join", func(t *testing.T) {
		env := newMenuEnv(t)
		net := chatcovFakeNet(env)
		net.events = nil
		net.joinErr = func(room string) error {
			if room == "den" {
				return errors.New("hub down")
			}
			return nil
		}

		// Start on the network in the lobby, then move to #den on it, which
		// the network refuses and the local service takes instead.
		r := chatcovChat(env, env.caller, "\r\r/network\r1\r2\rhere\r/q\r")
		if !chatcovHas(r, "Network: FakeNet", "Could not join room: hub down", "*** Joined #den") {
			t.Fatalf("did not start on the network and fall back to local #den:\n%s", chatcovText(r))
		}
		if h := chatLastHeader(t, r); !strings.Contains(h, "Network: Local") {
			t.Errorf("header = %q, want the Local network after the fallback", h)
		}
		chatcovEqual(t, "local den history", chatcovLocalHistory(env, "den"), []string{"Caller: here"})
	})
}

// A /join the service refuses leaves the caller in the room they were in:
// they are rejoined to it, later posts still go to it, and the header keeps
// naming it.
func TestChatJoinFailureKeepsCurrentRoom(t *testing.T) {
	env := newMenuEnv(t)
	net := chatcovFakeNet(env)
	net.events = nil
	net.joinErr = func(room string) error {
		if room == "vault" {
			return errors.New("join boom")
		}
		return nil
	}

	r := chatcovChat(env, env.caller, "\r\r/join vault\rstill here\r/q\r")
	if !chatcovHas(r, "Could not join room: join boom", "<Caller> still here") {
		t.Fatalf("failed /join not reported, or chat did not carry on:\n%s", chatcovText(r))
	}
	if chatcovHas(r, "Joined #vault") || chatcovHas(r, "Room: vault") {
		t.Errorf("refused room announced or shown in the header:\n%s", chatcovText(r))
	}
	if h := chatLastHeader(t, r); !strings.Contains(h, "Room: lobby") {
		t.Errorf("header = %q, want it still to name #lobby", h)
	}
	svc := net.chatted()[0]
	chatcovEqual(t, "joins", svc.log("join"), []string{"lobby", "vault", "lobby"})
	chatcovEqual(t, "leaves", svc.log("leave"), []string{"lobby", "lobby"})
	chatcovEqual(t, "posts", svc.log("post"), []string{"lobby: still here"})
}

// If going back to the old room fails too, the caller is told, and chat stays
// addressed to the old room rather than the one that was refused.
func TestChatJoinFailureRejoinFails(t *testing.T) {
	env := newMenuEnv(t)
	net := chatcovFakeNet(env)
	net.events = nil
	joins := 0
	net.joinErr = func(string) error {
		joins++
		// The initial join works; the /join and the rejoin of lobby fail;
		// the next /join works again.
		if joins == 2 || joins == 3 {
			return errors.New("hub gone")
		}
		return nil
	}

	r := chatcovChat(env, env.caller, "\r\r/join vault\rstill here\r/topic lost\r/join den\rhi\r/q\r")
	if !chatcovHas(r, "Could not join room: hub gone", "Could not rejoin #lobby: hub gone",
		"You are not in a room. Use /JOIN <room> to join one.", "*** Joined #den") {
		t.Errorf("failed rejoin not reported, or no-room state not shown:\n%s", chatcovText(r))
	}
	svc := net.chatted()[0]
	// Nothing goes to lobby once the caller has left it, and the next /join
	// does not try to leave a room they are not in.
	chatcovEqual(t, "posts", svc.log("post"), []string{"den: hi"})
	chatcovEqual(t, "topics", svc.log("topic"), nil)
	chatcovEqual(t, "leaves", svc.log("leave"), []string{"lobby", "den"})
}

// A room name the service would refuse is rejected before the caller leaves
// their room; an acceptable one is normalised the way the service joins it,
// so posts are addressed to the room actually joined.
func TestChatJoinNormalisesRoomName(t *testing.T) {
	env := newMenuEnv(t)
	net := chatcovFakeNet(env)
	net.events = nil

	r := chatcovChat(env, env.caller, "\r\r/join Bad!Room\r/join Back Room\rhi\r/q\r")
	if !chatcovHas(r, "Could not join room: room name may only contain", "*** Joined #back-room", "Room: back-room") {
		t.Errorf("/join did not reject the bad name and join the normalised one:\n%s", chatcovText(r))
	}
	svc := net.chatted()[0]
	chatcovEqual(t, "joins", svc.log("join"), []string{"lobby", "back-room"})
	chatcovEqual(t, "leaves", svc.log("leave"), []string{"lobby", "back-room"})
	chatcovEqual(t, "posts", svc.log("post"), []string{"back-room: hi"})
}

func TestChatRoomChoice(t *testing.T) {
	rooms := []chat.RoomInfo{{Name: "lobby"}, {Name: "den"}}
	for _, tc := range []struct {
		input, want string
		wantErr     bool
	}{
		{"", "lobby", false},
		{"  ", "lobby", false},
		{"2", "den", false},
		{"3", "3", false}, // out of range, so a room name
		{"Den", "den", false},
		{" Back Room ", "back-room", false},
		{"Bad!Room", "lobby", true},
	} {
		got, err := chatRoomChoice(tc.input, rooms)
		if got != tc.want || (err != nil) != tc.wantErr {
			t.Errorf("chatRoomChoice(%q) = %q, %v; want %q, error %v", tc.input, got, err, tc.want, tc.wantErr)
		}
	}
}

// A room name typed at the picker is normalised before the join, so posts
// and the final Leave go to the room actually joined (#538).
func TestChatPickerNormalisesRoomName(t *testing.T) {
	env := newMenuEnv(t)
	net := chatcovFakeNet(env)
	net.events = nil

	chatcovChat(env, env.caller, "\rDen\rhi\r/q\r")
	svc := net.chatted()[0]
	chatcovEqual(t, "joins", svc.log("join"), []string{"den"})
	chatcovEqual(t, "posts", svc.log("post"), []string{"den: hi"})
	chatcovEqual(t, "leaves", svc.log("leave"), []string{"den"})
}

// A probe that times out stops waiting for the room list but does not close
// the session while that request is still running; it is closed once the
// request returns (#538).
func TestChatProbeTimeoutClosesSessionAfterRooms(t *testing.T) {
	oldTimeout := chatProbeTimeout
	chatProbeTimeout = 20 * time.Millisecond
	t.Cleanup(func() { chatProbeTimeout = oldTimeout })

	env := newMenuEnv(t)
	net := chatcovFakeNet(env)
	net.roomsBlock = make(chan struct{})

	nets := probeChatNetworks([]ChatLeafInfo{net.leaf()}, "caller")
	if nets[0].avail {
		t.Fatal("timed-out network marked available")
	}
	net.mu.Lock()
	probe := net.sessions[0]
	net.mu.Unlock()
	if closes := probe.log("close"); len(closes) != 0 {
		t.Fatal("probe session closed while its room list request was still running")
	}

	close(net.roomsBlock)
	deadline := time.Now().Add(2 * time.Second)
	for len(probe.log("close")) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("probe session never closed after its room list returned")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
