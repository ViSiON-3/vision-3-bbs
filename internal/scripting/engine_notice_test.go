package scripting

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNoticeStopsAScriptAtItsKeyWait(t *testing.T) {
	for name, read := range map[string]string{
		"getkey":            "v3.console.getkey()",
		"readkey":           "v3.console.readkey()",
		"readkey with time": "v3.console.readkey(5)",
		"pause":             "v3.console.pause()",
	} {
		t.Run(name, func(t *testing.T) {
			notice := make(chan struct{})
			close(notice)
			h := newHarness(t, harnessOpts{notice: notice})
			// try/catch cannot keep the script going.
			err := h.run(`v3.console.write("drawn");
for (var i = 0; i < 100; i++) { try { ` + read + `; } catch (e) { v3.console.write("caught"); } }
v3.console.write("ran on");`)
			if !errors.Is(err, ErrNotice) {
				t.Fatalf("Run = %v, want ErrNotice", err)
			}
			if out := h.output(); !strings.HasPrefix(out, "drawn") || strings.Contains(out, "ran on") || strings.Contains(out, "caught") {
				t.Errorf("output = %q", out)
			}
		})
	}
}

func TestNoticeLeavesKeysReadInToTheScript(t *testing.T) {
	notice := make(chan struct{})
	close(notice)

	t.Run("parsed and waiting", func(t *testing.T) {
		h := newHarness(t, harnessOpts{notice: notice})
		h.eng.inputBuf = []byte("y")
		if key, _, err := h.eng.readKeyEcho(0); key != "y" || err != nil {
			t.Fatalf("readKeyEcho = %q, %v; want y", key, err)
		}
		if _, _, err := h.eng.readKeyEcho(0); !errors.Is(err, ErrNotice) {
			t.Fatalf("readKeyEcho = %v, want ErrNotice", err)
		}
	})

	t.Run("read from the session", func(t *testing.T) {
		h := newHarness(t, harnessOpts{input: "x", notice: notice})
		h.eng.startReader()
		deadline := time.Now().Add(5 * time.Second)
		for len(h.eng.rawInputCh) == 0 {
			if time.Now().After(deadline) {
				t.Fatal("the reader never read the key")
			}
			time.Sleep(time.Millisecond)
		}
		if key, _, err := h.eng.readKeyEcho(0); key != "x" || err != nil {
			t.Fatalf("readKeyEcho = %q, %v; want x", key, err)
		}
		if _, _, err := h.eng.readKeyEcho(0); !errors.Is(err, ErrNotice) {
			t.Fatalf("readKeyEcho = %v, want ErrNotice", err)
		}
	})
}

// A notice sent once, rather than closed, that yields to a key already
// read in still stops the script at the next key wait.
func TestNoticeSentOnceStaysDue(t *testing.T) {
	notice := make(chan struct{}, 1)
	h := newHarness(t, harnessOpts{input: "x", notice: notice})
	h.eng.startReader()
	deadline := time.Now().Add(5 * time.Second)
	for len(h.eng.rawInputCh) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the reader never read the key")
		}
		time.Sleep(time.Millisecond)
	}
	notice <- struct{}{}
	// Either case of the select may win; the key comes back first either way.
	if key, _, err := h.eng.readKeyEcho(0); key != "x" || err != nil {
		t.Fatalf("readKeyEcho = %q, %v; want x", key, err)
	}
	// Bounded, so a lost notice fails the test rather than hanging it.
	if key, _, err := h.eng.readKeyEcho(5 * time.Second); !errors.Is(err, ErrNotice) {
		t.Fatalf("readKeyEcho = %q, %v; want ErrNotice", key, err)
	}
}

func TestNoticeDoesNotStopALineBeingTyped(t *testing.T) {
	notice := make(chan struct{})
	close(notice)
	h := newHarness(t, harnessOpts{input: "abc\r", notice: notice})
	if err := h.run(`v3.console.write("[" + v3.console.getstr(10) + "]");`); err != nil {
		t.Fatalf("Run = %v", err)
	}
	if out := h.output(); !strings.HasSuffix(out, "[abc]") {
		t.Errorf("output = %q", out)
	}
}

func TestNoNoticeLeavesTheScriptRunning(t *testing.T) {
	h := newHarness(t, harnessOpts{input: "x"})
	h.mustRun(`v3.console.write("[" + v3.console.getkey() + "]");`)
	if out := h.output(); out != "[x]" {
		t.Errorf("output = %q", out)
	}
}

// feedSession is a session whose input the test sends while the script runs.
type feedSession struct {
	keys chan []byte
	mu   sync.Mutex
	out  []byte
}

func newFeedSession(t *testing.T) *feedSession {
	s := &feedSession{keys: make(chan []byte, 8)}
	t.Cleanup(func() { close(s.keys) })
	return s
}

func (s *feedSession) Read(p []byte) (int, error) {
	b, ok := <-s.keys
	if !ok {
		return 0, io.EOF
	}
	return copy(p, b), nil
}

func (s *feedSession) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.out = append(s.out, p...)
	s.mu.Unlock()
	return len(p), nil
}

func (s *feedSession) output() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.out)
}

// twoKeyScript reads a two-key command as the board's menu scripts do: the
// first key, then up to 500 ms for the second.
const twoKeyScript = `for (;;) {
  var k = v3.console.readkey(66);
  if (k !== "/") continue;
  v3.console.write("<gap>");
  var next = v3.console.readkey(500);
  v3.console.write("[" + k + next + "]");
  break;
}`

// waitOutput waits for the script to write text.
func waitOutput(t *testing.T, s *feedSession, text string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(s.output(), text) {
		if time.Now().After(deadline) {
			t.Fatalf("the script never wrote %q; output %q", text, s.output())
		}
		time.Sleep(time.Millisecond)
	}
}

// runAsync runs src and returns the channel its result comes on.
func runAsync(h *scriptHarness, src string) <-chan error {
	done := make(chan error, 1)
	go func() { done <- h.run(src) }()
	return done
}

func TestNoticeBetweenTheKeysOfACommandKeepsTheFirst(t *testing.T) {
	notice := make(chan struct{})
	h := newHarness(t, harnessOpts{notice: notice})
	s := newFeedSession(t)
	h.sc.Session = s
	done := runAsync(h, twoKeyScript)

	s.keys <- []byte("/")
	waitOutput(t, s, "<gap>")
	close(notice)
	time.Sleep(50 * time.Millisecond)
	s.keys <- []byte("W")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run = %v, want the command to run", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the script did not finish")
	}
	if out := s.output(); !strings.Contains(out, "[/W]") {
		t.Errorf("output = %q, want the command /W", out)
	}
}

// A notice held for the second key of a command stops the script when the
// wait for that key runs out with none.
func TestNoticeHeldForASecondKeyStopsTheScriptAfterIt(t *testing.T) {
	notice := make(chan struct{})
	h := newHarness(t, harnessOpts{notice: notice})
	s := newFeedSession(t)
	h.sc.Session = s
	done := runAsync(h, `var k = v3.console.readkey();
v3.console.write("<gap>");
var next = v3.console.readkey(300);
v3.console.write("[" + k + next + "]");
for (;;) v3.console.readkey(66);`)

	// The wait for the second key starts after the first key is sent, so
	// it cannot end sooner than 300 ms after sent.
	sent := time.Now()
	s.keys <- []byte("/")
	waitOutput(t, s, "<gap>")
	fired := time.Now()
	close(notice)
	select {
	case err := <-done:
		if !errors.Is(err, ErrNotice) {
			t.Fatalf("Run = %v, want ErrNotice", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the notice never stopped the script")
	}
	// Stopped as the wait for the second key ran out, not before.
	if took := time.Since(sent); took < 300*time.Millisecond {
		t.Errorf("stopped %v after the first key, before the 300 ms wait ended", took)
	}
	if took := time.Since(fired); took > time.Second {
		t.Errorf("stopped %v after the notice, want as the 300 ms wait ended", took)
	}
}

// A notice is held for a timed key wait after a key no longer than
// noticeKeyHold, however fast keys keep coming.
func TestNoticeHeldNoLongerThanTheHold(t *testing.T) {
	notice := make(chan struct{})
	h := newHarness(t, harnessOpts{notice: notice})
	s := newFeedSession(t)
	h.sc.Session = s
	done := runAsync(h, `v3.console.readkey(); v3.console.write("<go>"); for (;;) v3.console.readkey(500);`)

	s.keys <- []byte("x")
	waitOutput(t, s, "<go>")
	fired := time.Now()
	close(notice)
	stop := make(chan struct{})
	var typing sync.WaitGroup
	typing.Add(1)
	go func() {
		defer typing.Done()
		for {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
				select {
				case s.keys <- []byte("x"):
				default:
				}
			}
		}
	}()
	defer func() { close(stop); typing.Wait() }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrNotice) {
			t.Fatalf("Run = %v, want ErrNotice", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("keys typed without pause held the notice off for good")
	}
	if took := time.Since(fired); took > noticeKeyHold+500*time.Millisecond {
		t.Errorf("stopped %v after the notice, want about %v", took, noticeKeyHold)
	}
}

// A prompt stopped by a notice writes nothing after its own text.
func TestPromptStoppedByANoticeWritesNothing(t *testing.T) {
	for name, c := range map[string]struct{ call, text string }{
		"yesno": {`v3.console.yesno("Sure")`, "Sure (Y/n)? "},
		"noyes": {`v3.console.noyes("Sure")`, "Sure (N/y)? "},
		"pause": {`v3.console.pause()`, "\r\n[Press any key] "},
	} {
		t.Run(name, func(t *testing.T) {
			notice := make(chan struct{})
			close(notice)
			h := newHarness(t, harnessOpts{notice: notice})
			if err := h.run(c.call + ";"); !errors.Is(err, ErrNotice) {
				t.Fatalf("Run = %v, want ErrNotice", err)
			}
			if out := h.output(); out != c.text {
				t.Errorf("output = %q, want %q", out, c.text)
			}
		})
	}
}

// A notice sent once, rather than closed, that is held for the second key of
// a command stops the script at the wait after that key.
func TestNoticeSentOnceAndHeldStaysDue(t *testing.T) {
	notice := make(chan struct{}, 1)
	h := newHarness(t, harnessOpts{notice: notice})
	s := newFeedSession(t)
	h.sc.Session = s
	done := runAsync(h, twoKeyScript+`
for (;;) v3.console.readkey(66);`)

	s.keys <- []byte("/")
	waitOutput(t, s, "<gap>")
	notice <- struct{}{}
	// Wait for the engine to take the notice, so it is held in the gap.
	deadline := time.Now().Add(5 * time.Second)
	for len(notice) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("the engine never took the notice")
		}
		time.Sleep(time.Millisecond)
	}
	s.keys <- []byte("W")
	select {
	case err := <-done:
		if !errors.Is(err, ErrNotice) {
			t.Fatalf("Run = %v, want ErrNotice", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the notice was lost")
	}
	if out := s.output(); !strings.Contains(out, "[/W]") {
		t.Errorf("output = %q, want the command /W first", out)
	}
}
