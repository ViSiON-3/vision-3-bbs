package menu

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/chat"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestChatArtFill(t *testing.T) {
	widths := map[string]int{}
	fields := map[string]func(int) string{
		"ROOM": func(int) string { return "lobby" },
		"TOPIC": func(int) string {
			return "say @U@ and @ROOM#@" // placeholders in a value stay text
		},
		"U": func(w int) string { widths["U"] = w; return "·003" },
	}
	art := "[@ROOM###@][@U#@][@TOPIC#############@][@NOPE##@]"
	got := string(chatArtFill([]byte(art), fields))
	want := "[lobby    ][·003][say @U@ and @ROOM#@ ][@NOPE##@]"
	if got != want {
		t.Errorf("chatArtFill = %q, want %q", got, want)
	}
	if widths["U"] != 4 {
		t.Errorf("U asked to fill %d columns, want 4", widths["U"])
	}
}

func TestChatCounterFields(t *testing.T) {
	for _, tc := range []struct {
		name, got, want string
	}{
		{"users", chatCounter(4, 3, ""), "·003"},
		{"mentions", chatCounter(3, 1, ""), "·01"},
		{"too large shows nines", chatCounter(3, 250, ""), "·99"},
		{"lag with its unit", chatCounter(6, 42, "ms"), "·042ms"},
		{"wider field, more digits", chatCounter(7, 12, ""), "·000012"},
		{"no room for digits", chatCounter(1, 5, ""), "5"},
		{"lag", chatLagField(6, 42*time.Millisecond, true), "·042ms"},
		{"lag under a millisecond", chatLagField(6, 300*time.Microsecond, true), "·000ms"},
		{"lag not measured", chatLagField(6, 0, false), "·---ms"},
		{"lag not measured, narrow field", chatLagField(3, 0, false), "-"},
		{"buffer", chatBufferField(8, 12, 500), "·012/500"},
		{"buffer full", chatBufferField(8, 500, 500), "·500/500"},
		{"buffer in a narrow field", chatBufferField(6, 12, 500), "·12/99"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

func TestChatMentions(t *testing.T) {
	for text, want := range map[string]bool{
		"hey caller":                true,
		"Caller, you there?":        true,
		"ping @CALLER!":             true,
		"caller's turn":             true,
		"callers unite":             false,
		"recaller":                  false,
		"nobody here":               false,
		"x caller2 y":               false,
		"first callerx then caller": true,
	} {
		if got := chatMentions(text, "Caller"); got != want {
			t.Errorf("chatMentions(%q) = %v, want %v", text, got, want)
		}
	}
	if chatMentions("anything", "") {
		t.Errorf("an empty handle is never mentioned")
	}
}

// chatLiveSession is a test session whose keystrokes are fed while chat runs,
// so a test can wait for an event to be drawn before typing the next line.
// Output is guarded, since the test reads it while chat goroutines write it.
type chatLiveSession struct {
	*testSession
	in  *io.PipeReader
	mu  sync.Mutex
	out bytes.Buffer
}

func (s *chatLiveSession) Read(p []byte) (int, error) { return s.in.Read(p) }

func (s *chatLiveSession) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out.Write(p)
}

func (s *chatLiveSession) output() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out.String()
}

// chatLive is a chat session in progress.
type chatLive struct {
	t    *testing.T
	s    *chatLiveSession
	keys *io.PipeWriter
	done chan struct{}
}

// startChatLive runs chat for u on its own goroutine.
func startChatLive(env *menuEnv, u *user.User) *chatLive {
	env.t.Helper()
	r, w := io.Pipe()
	s := &chatLiveSession{testSession: newTestSession(""), in: r}
	s.addr = &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2222}
	SetSessionOutputMode(s, env.outputMode)
	c := &cmdCtx{
		e: env.e, s: s, terminal: term.NewTerminal(s, ""), userManager: env.um,
		currentUser: u, nodeNumber: 1, sessionStartTime: time.Now(),
		outputMode: env.outputMode, termWidth: 80, termHeight: 24,
	}
	live := &chatLive{t: env.t, s: s, keys: w, done: make(chan struct{})}
	go func() {
		defer close(live.done)
		_, _, _ = runChat(c, "")
	}()
	env.t.Cleanup(func() {
		_ = w.Close()
		select {
		case <-live.done:
		case <-time.After(10 * time.Second):
			env.t.Errorf("chat still running after its input closed")
		}
		resetSessionIH(s)
		ClearSessionOutputMode(s)
	})
	return live
}

// typeKeys sends keystrokes to the running chat.
func (c *chatLive) typeKeys(keys string) {
	c.t.Helper()
	if _, err := c.keys.Write([]byte(keys)); err != nil {
		c.t.Fatalf("typing %q: %v", keys, err)
	}
}

// header returns the text of the last header drawn, from its room label on.
func (c *chatLive) header() string {
	screen := testAnsiEscape.ReplaceAllString(c.s.output(), "")
	i := strings.LastIndex(screen, "Room: ")
	if i < 0 {
		return ""
	}
	// The header is five rows; the art's last row ends in its corner.
	end := strings.Index(screen[i:], "┘")
	if end < 0 {
		return screen[i:]
	}
	return screen[i : i+end]
}

// waitHeader waits until the last header drawn contains every one of want.
func (c *chatLive) waitHeader(want ...string) {
	c.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		h := c.header()
		ok := true
		for _, w := range want {
			ok = ok && strings.Contains(h, w)
		}
		if ok {
			return
		}
		if time.Now().After(deadline) {
			c.t.Fatalf("header never showed %q; last header:\n%s\nscreen: %q", want, h, c.s.output())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitOutput waits until the screen text contains marker.
func (c *chatLive) waitOutput(marker string) {
	c.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(testAnsiEscape.ReplaceAllString(c.s.output(), ""), marker) {
		if time.Now().After(deadline) {
			c.t.Fatalf("%q never shown", marker)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestChatHeaderUsersAndMentions covers the Users and Mentions counters of
// #660 on Local chat, driven by a second session on the same board.
func TestChatHeaderUsersAndMentions(t *testing.T) {
	env := newMenuEnv(t)
	sysop := chatcovLocal(env, "Sysop")
	if _, _, err := sysop.Join("lobby"); err != nil {
		t.Fatal(err)
	}

	live := startChatLive(env, env.caller)
	live.waitOutput("Select room [lobby]:")
	live.typeKeys("\r")
	live.waitHeader("[Users·002]", "[Mentions·00]")

	// Messages that name the caller as a word count; others do not.
	for _, text := range []string{"hi caller", "callers are great", "CALLER, you there?"} {
		if err := sysop.Post("lobby", text); err != nil {
			t.Fatal(err)
		}
	}
	live.waitOutput("CALLER, you there?")
	live.waitHeader("[Mentions·02]")

	// A third member joins, then leaves.
	eve := chatcovLocal(env, "Eve")
	if _, _, err := eve.Join("lobby"); err != nil {
		t.Fatal(err)
	}
	live.waitHeader("[Users·003]")
	if err := eve.Leave("lobby"); err != nil {
		t.Fatal(err)
	}
	live.waitHeader("[Users·002]")

	// A room change resets the mention count and shows the new room's users.
	live.typeKeys("/join den\r")
	live.waitHeader("Room: den", "[Users·001]", "[Mentions·00]")
	live.typeKeys("/q\r")
}

// On Local chat there is no network to measure, so Lag is blank.
func TestChatHeaderLagBlankOnLocal(t *testing.T) {
	env := newMenuEnv(t)
	live := startChatLive(env, env.caller)
	live.waitHeader("[Lag      ]", "[Users·001]")
	live.typeKeys("/q\r")
}

// Filling the counters keeps the stock art's shape: every header row is as
// wide as the top border.
func TestChatHeaderKeepsArtWidth(t *testing.T) {
	env := newMenuEnv(t)
	r := chatcovChat(env, env.caller, "/q\r")
	_, screen, _ := strings.Cut(r.raw, "\x1b[6;21r")
	var widths []int
	for row := 1; row <= 5; row++ {
		start := strings.Index(screen, fmt.Sprintf("\x1b[%d;1H", row))
		if start < 0 {
			t.Fatalf("header row %d not drawn", row)
		}
		rest := screen[start+len(fmt.Sprintf("\x1b[%d;1H", row)):]
		if end := strings.Index(rest, fmt.Sprintf("\x1b[%d;1H", row+1)); end >= 0 {
			rest = rest[:end]
		} else if end := strings.Index(rest, "\x1b[24;1H"); end >= 0 {
			rest = rest[:end]
		}
		widths = append(widths, utf8.RuneCountInString(testAnsiEscape.ReplaceAllString(rest, "")))
	}
	for i, w := range widths {
		if w != widths[0] {
			t.Errorf("header row %d is %d columns, row 1 is %d", i+1, w, widths[0])
		}
	}
}

// chatPingSvc is a chatcovSvc whose network answers pings.
type chatPingSvc struct {
	*chatcovSvc
	ping func() (time.Duration, error)
}

func (s *chatPingSvc) Ping(ctx context.Context) (time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return s.ping()
}

// chatPingNet installs a fake network, with no queued events, whose sessions
// answer pings with ping.
func chatPingNet(env *menuEnv, ping func() (time.Duration, error)) *chatcovNet {
	n := chatcovFakeNet(env)
	n.events = nil
	base := n.leaf()
	env.e.ChatLeaves = chatcovLeaves{{NetworkName: n.name, NewSession: func(handle string) chat.ChatService {
		return &chatPingSvc{chatcovSvc: base.NewSession(handle).(*chatcovSvc), ping: ping}
	}}}
	return n
}

// TestChatHeaderLag covers the Lag counter of #660: on network chat it shows
// the measured round trip, refreshed on a timer, and dashes while the network
// cannot be reached.
func TestChatHeaderLag(t *testing.T) {
	old := chatLagInterval
	chatLagInterval = 10 * time.Millisecond
	t.Cleanup(func() { chatLagInterval = old })

	env := newMenuEnv(t)
	var mu sync.Mutex
	rtt, fail := 42*time.Millisecond, false
	chatPingNet(env, func() (time.Duration, error) {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			return 0, errors.New("hub down")
		}
		return rtt, nil
	})

	live := startChatLive(env, env.caller)
	live.waitOutput("Select network [1]:")
	live.typeKeys("\r")
	live.waitOutput("Select room [lobby]:")
	live.typeKeys("\r")
	live.waitHeader("[Lag·042ms]", "[Users·002]")

	mu.Lock()
	rtt = 1500 * time.Millisecond
	mu.Unlock()
	live.waitHeader("[Lag·999ms]")

	mu.Lock()
	fail = true
	mu.Unlock()
	live.waitHeader("[Lag·---ms]")

	// /network to Local stops measuring and blanks the field.
	live.typeKeys("/network\r")
	live.waitOutput("Select network [1]: ")
	live.typeKeys("2\r")
	live.waitHeader("Network: Local", "[Lag      ]")
	live.typeKeys("/q\r")
}
