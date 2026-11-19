package menu

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/pace"
	"golang.org/x/term"
)

// slowAnswer is the slow terminal's answer to a primary device attributes
// query: a VT220 that draws sixel.
const slowAnswer = "\x1b[?62;4c"

// slowTerminal is a caller's terminal behind a connection that holds
// capacity bytes, which the terminal reads at rate bytes a second. It
// answers each primary device attributes query when it reads it, as a
// terminal answers in the order it reads, unless it is muted. What the
// board writes is recorded (written) when the board writes it, before the
// connection takes it.
type slowTerminal struct {
	*probeSession
	rate, capacity int
	delay          time.Duration // how long after reading a query it answers
	split          bool          // each answer is sent as ESC, then the rest 30 ms later
	answerQ        chan time.Time
	frameTimes     []time.Time // when each sixel frame was written

	mu      sync.Mutex
	space   *sync.Cond
	queued  []byte // written, not yet read by the terminal
	tail    []byte // the end of what the terminal read, for a query split across reads
	muted   bool
	held    int // queries read while muted
	answers int // answers sent
	closed  bool
}

func newSlowTerminal(t *testing.T, rate, capacity int) *slowTerminal {
	s := &slowTerminal{probeSession: newProbeSession("xterm"), rate: rate, capacity: capacity}
	s.space = sync.NewCond(&s.mu)
	s.answerQ = make(chan time.Time, 4096)
	stop := make(chan struct{})
	go s.read(stop)
	go s.answer(stop)
	t.Cleanup(func() {
		close(stop)
		s.mu.Lock()
		s.closed = true
		s.space.Broadcast()
		s.mu.Unlock()
		resetSessionIH(s)
		ClearSessionIdleTimeout(s)
	})
	return s
}

// Write takes p as a connection does: as much as there is room for, and
// the rest as the terminal reads.
func (s *slowTerminal) Write(p []byte) (int, error) {
	_, _ = s.probeSession.Write(p)
	s.mu.Lock()
	defer s.mu.Unlock()
	if bytes.Contains(p, []byte("\x1bP")) {
		s.frameTimes = append(s.frameTimes, time.Now())
	}
	n := 0
	for n < len(p) {
		for len(s.queued) >= s.capacity && !s.closed {
			s.space.Wait()
		}
		if s.closed {
			return n, io.ErrClosedPipe
		}
		m := min(len(p)-n, s.capacity-len(s.queued))
		s.queued = append(s.queued, p[n:n+m]...)
		n += m
	}
	return n, nil
}

// read is the terminal reading the connection, every 10 ms.
func (s *slowTerminal) read(stop <-chan struct{}) {
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		s.mu.Lock()
		k := min(len(s.queued), s.rate/100)
		seen := append(s.tail, s.queued[:k]...)
		s.queued = append(s.queued[:0], s.queued[k:]...)
		s.space.Broadcast()
		queries := bytes.Count(seen, []byte(ansi.DAPrimaryQuery))
		s.tail = append([]byte(nil), seen[max(0, len(seen)-2):]...)
		if s.muted {
			s.held += queries
			queries = 0
		}
		s.answers += queries
		due := time.Now().Add(s.delay)
		s.mu.Unlock()
		for range queries {
			s.answerQ <- due
		}
	}
}

// answer sends the answers read queries are owed, in order, each when it
// is due.
func (s *slowTerminal) answer(stop <-chan struct{}) {
	for {
		var due time.Time
		select {
		case <-stop:
			return
		case due = <-s.answerQ:
		}
		time.Sleep(time.Until(due))
		s.mu.Lock()
		split := s.split
		s.mu.Unlock()
		if split {
			s.data <- []byte(slowAnswer[:1])
			time.Sleep(30 * time.Millisecond)
			s.data <- []byte(slowAnswer[1:])
			continue
		}
		s.data <- []byte(slowAnswer)
	}
}

// mute stops the terminal answering; unmute sends the answers it held and
// answers again.
func (s *slowTerminal) mute() {
	s.mu.Lock()
	s.muted = true
	s.mu.Unlock()
}

func (s *slowTerminal) unmute() {
	s.mu.Lock()
	held := s.held
	s.muted, s.held = false, 0
	s.answers += held
	s.mu.Unlock()
	for range held {
		s.data <- []byte(slowAnswer)
	}
}

// unanswered returns how many queries the board has written that the
// terminal has not answered: the frames it has not answered for. Each
// frame is followed by a query, also when nothing in the picture changed
// and no sixel was sent; the probe's query and its answer cancel out.
func (s *slowTerminal) unanswered() int {
	queries := strings.Count(s.written(), ansi.DAPrimaryQuery)
	s.mu.Lock()
	defer s.mu.Unlock()
	return queries - s.answers
}

// pictureMatrix makes env's login matrix the picture scene effects
// describes, with the shipped logo as its picture.
func pictureMatrix(t *testing.T, env *menuEnv, effects string) {
	t.Helper()
	set := chatcovMatrixSet(t, env)
	logo, err := os.ReadFile("../../menus/vision3/data/vision3-logo.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(set, "ansi", "PDMATRIX.PNG"), logo, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(set, "ansi", "PDMATRIX.EFX"), []byte(effects), 0o644); err != nil {
		t.Fatal(err)
	}
}

// startSlowMatrix probes s, which answers it as a sixel terminal, and runs
// the login matrix on it.
func startSlowMatrix(t *testing.T, env *menuEnv, s *slowTerminal) <-chan matrixResult {
	t.Helper()
	_, log := newSyncLog()
	ProbeTerminal(s, 2*time.Second, false, log)
	if !sessionSixel(s, nil) {
		t.Fatal("the slow terminal's answer did not turn sixel on")
	}
	done := make(chan matrixResult, 1)
	go func() {
		action, _, err := env.e.RunMatrixScreen(s, term.NewTerminal(s, ""), env.um, 1, ansi.OutputModeCP437, 80, 24)
		done <- matrixResult{action, err}
	}()
	return done
}

// actedWithin waits up to d for the board to write want after mark bytes of
// output, and reports how long it took.
func actedWithin(t *testing.T, s *slowTerminal, mark int, want string, d time.Duration) {
	t.Helper()
	start := time.Now()
	for !strings.Contains(s.written()[mark:], want) {
		if time.Since(start) > d {
			t.Fatalf("key not acted on within %v: %q never written", d, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A terminal that reads 20 KB a second has every key acted on within a
// second, at any point in 30 seconds of the animated matrix, and never has
// more than pace.InFlight frames it has not answered for.
func TestMatrixActsOnKeysBehindASlowReader(t *testing.T) {
	env := newMenuEnv(t)
	pictureMatrix(t, env, `{"heat":true,"textFade":true,"projectText":true}`)
	// The connection holds 256 KB: the frames that may be unanswered
	// (pace.InFlight, up to about 80 KB each) fit in it. One that holds
	// less still blocks the write of the last of them while the terminal
	// reads the part that does not fit.
	s := newSlowTerminal(t, 20_000, 256<<10)
	done := startSlowMatrix(t, env, s)
	start := time.Now()
	for _, at := range []time.Duration{2 * time.Second, 10 * time.Second, 17 * time.Second, 24 * time.Second} {
		time.Sleep(time.Until(start.Add(at)))
		if n := s.unanswered(); n > pace.InFlight {
			t.Fatalf("%d frames unanswered, want at most %d", n, pace.InFlight)
		}
		mark := len(s.written())
		s.reply("A") // check access
		actedWithin(t, s, mark, "Enter your username to check access", time.Second)
		s.reply("\r") // no name: back to the matrix
	}
	time.Sleep(time.Until(start.Add(30 * time.Second)))
	s.reply("D")
	select {
	case r := <-done:
		if r.err != nil || r.action != "DISCONNECT" {
			t.Fatalf("matrix action=%q err=%v, want DISCONNECT", r.action, r.err)
		}
	case <-time.After(time.Second):
		t.Fatal("D at 30 s not acted on within 1 s")
	}
}

// animateScript draws the logo with a moving heat haze as fast as the
// canvas lets it, reads keys between frames, prints each key it reads and
// ends on q.
const animateScript = `var c = v3.gfx.canvas(480, 160, 'logo.png');
var t = 0;
for (;;) {
	c.drawImage('logo.png', 0, 0);
	c.heat(t, 0.05);
	t += 0.1;
	v3.console.write("\x1b[1;1H");
	c.presentDelta();
	var k = v3.console.readkey(50);
	if (k === 'q') break;
	if (k) v3.console.print("[key " + k + "]");
}`

// startScriptDoor probes s, which answers as a sixel terminal, and runs src
// on it as a v3_script door with the shipped logo as logo.png in its data
// directory.
func startScriptDoor(t *testing.T, s *slowTerminal, src string) <-chan error {
	t.Helper()
	env := newMenuEnv(t)
	dir, data := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "anim.js"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	logo, err := os.ReadFile("../../menus/vision3/data/vision3-logo.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "logo.png"), logo, 0o644); err != nil {
		t.Fatal(err)
	}
	s.ctx = &testCtx{Context: context.Background()}
	_, log := newSyncLog()
	ProbeTerminal(s, 2*time.Second, false, log)
	ctx := &DoorCtx{
		Executor: env.e, Session: s, User: doorUserInfo{ID: 1, Handle: "Tester", ScreenWidth: 80, ScreenHeight: 24},
		NodeNumber: 1, SessionStartTime: time.Now(), OutputMode: ansi.OutputModeCP437, DoorName: "ANIM",
		Config:        config.DoorConfig{Type: "v3_script", Script: "anim.js", WorkingDirectory: dir},
		ScriptDataDir: data, watch: newDoorWatch(0, time.Time{}),
	}
	done := make(chan error, 1)
	go func() { done <- executeV3ScriptDoor(ctx) }()
	return done
}

// A script animating a canvas on a terminal that reads 20 KB a second has
// every key acted on within a second.
func TestScriptCanvasActsOnKeysBehindASlowReader(t *testing.T) {
	s := newSlowTerminal(t, 20_000, 128<<10)
	done := startScriptDoor(t, s, animateScript)
	start := time.Now()
	for i, at := range []time.Duration{2 * time.Second, 6 * time.Second, 10 * time.Second} {
		time.Sleep(time.Until(start.Add(at)))
		mark := len(s.written())
		k := string(rune('a' + i))
		s.reply(k)
		actedWithin(t, s, mark, "[key "+k+"]", time.Second)
	}
	if n := strings.Count(s.written(), "\x1bP"); n < 3 {
		t.Errorf("%d frames drawn in 10 s, want the animation to go on", n)
	}
	s.reply("q")
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("q not acted on within 1 s")
	}
}

// rateRun is the matrix running on a terminal that reads at once, for
// measuring its frame rate.
type rateRun struct {
	s    *slowTerminal
	done <-chan matrixResult
}

// startRateRun starts the matrix on a terminal that reads at once and
// answers as answering sets it up. ungated runs it as main did, with no
// gate.
func startRateRun(t *testing.T, ungated bool, answering func(*slowTerminal)) rateRun {
	env := newMenuEnv(t)
	pictureMatrix(t, env, `{"heat":true,"textFade":true,"projectText":true}`)
	s := newSlowTerminal(t, 1<<30, 1<<30)
	if answering != nil {
		s.mu.Lock()
		answering(s)
		s.mu.Unlock()
	}
	_, log := newSyncLog()
	ProbeTerminal(s, 2*time.Second, false, log)
	if ungated {
		sessionFrameGates.Delete(s)
	}
	done := make(chan matrixResult, 1)
	go func() {
		action, _, err := env.e.RunMatrixScreen(s, term.NewTerminal(s, ""), env.um, 1, ansi.OutputModeCP437, 80, 24)
		done <- matrixResult{action, err}
	}()
	return rateRun{s, done}
}

// rate returns the frames r drew a second between from and to, and the
// longest time between two of them.
func (r rateRun) rate(from, to time.Time) (float64, time.Duration) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	frames, gap := 0, time.Duration(0)
	var last time.Time
	for _, at := range r.s.frameTimes {
		if at.Before(from) || at.After(to) {
			continue
		}
		if frames > 0 {
			gap = max(gap, at.Sub(last))
		}
		frames, last = frames+1, at
	}
	return float64(frames) / to.Sub(from).Seconds(), gap
}

// A terminal that answers at once, one that answers each frame 40 ms
// after reading it (as one that takes 40 ms to draw a frame does), and one
// whose every answer comes in two reads, ESC and then the rest 30 ms
// later, all get at least 85% of the frame rate the matrix draws at with
// no pacing, and no gap between frames over three times the longest
// unpaced one. All four run at once, so they share the machine's load.
func TestMatrixFrameRateForPromptTerminal(t *testing.T) {
	cases := []struct {
		name      string
		answering func(*slowTerminal)
	}{
		{"prompt", nil},
		{"40 ms late", func(s *slowTerminal) { s.delay = 40 * time.Millisecond }},
		{"split", func(s *slowTerminal) { s.split = true }},
	}
	unpacedRun := startRateRun(t, true, nil)
	runs := make([]rateRun, len(cases))
	for i, c := range cases {
		runs[i] = startRateRun(t, false, c.answering)
	}
	time.Sleep(time.Second) // past the first, whole frames
	from := time.Now()
	time.Sleep(5 * time.Second)
	to := time.Now()
	for _, r := range append([]rateRun{unpacedRun}, runs...) {
		r.s.reply("D")
		endMatrix(t, r.done)
	}
	unpaced, unpacedGap := unpacedRun.rate(from, to)
	for i, c := range cases {
		paced, gap := runs[i].rate(from, to)
		t.Logf("%s: %.1f frames a second paced, %.1f unpaced; longest gap %v paced, %v unpaced", c.name, paced, unpaced, gap, unpacedGap)
		if paced < unpaced*0.85 {
			t.Errorf("%s: %.1f frames a second, below 85%% of unpaced %.1f", c.name, paced, unpaced)
		}
		if gap > 3*unpacedGap {
			t.Errorf("%s: a gap of %v between frames, over three times the longest unpaced gap %v", c.name, gap, unpacedGap)
		}
	}
}

// A terminal that stops answering gets no further frames while it is
// silent and keeps its picture; its choices still move and select, and
// frames resume when it answers.
func TestMatrixHoldsFramesForSilentTerminal(t *testing.T) {
	env := newMenuEnv(t)
	pictureMatrix(t, env, `{"heat":true,"textFade":true,"projectText":true}`)
	s := newSlowTerminal(t, 1<<30, 1<<30)
	done := startSlowMatrix(t, env, s)
	frames := func() int { return strings.Count(s.written(), "\x1bP") }
	time.Sleep(time.Second)
	s.mute()
	// The frames unanswered when it fell silent are the last: wait until
	// the board has sent as many as may be unanswered.
	deadline := time.Now().Add(5 * time.Second)
	for s.unanswered() != pace.InFlight {
		if time.Now().After(deadline) {
			t.Fatal("the terminal never read the query after the last frame")
		}
		time.Sleep(10 * time.Millisecond)
	}
	held := frames()
	if held == 0 {
		t.Fatal("no frames drawn before the terminal fell silent")
	}

	// Two moves draw nothing over the held picture; Enter then selects
	// the third choice, check access.
	mark := len(s.written())
	s.reply(keyDown, keyDown)
	time.Sleep(300 * time.Millisecond)
	if out := s.written()[mark:]; out != "" {
		t.Fatalf("moves while silent wrote %q, want nothing", out)
	}
	s.reply("\r")
	actedWithin(t, s, mark, "Enter your username to check access", time.Second)
	s.reply("\r")
	time.Sleep(3 * time.Second)
	if n := frames(); n != held {
		t.Fatalf("%d frames sent while the terminal was silent, want none", n-held)
	}
	if n := s.unanswered(); n != pace.InFlight {
		t.Fatalf("%d frames unanswered while silent, want the %d sent last", n, pace.InFlight)
	}

	s.unmute()
	deadline = time.Now().Add(5 * time.Second)
	for frames() < held+2 {
		if time.Now().After(deadline) {
			t.Fatal("frames did not resume when the terminal answered")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.reply("D")
	endMatrix(t, done)
}

// A move in the picture scene shows only as a picture change: nothing of
// the text matrix (its choices or a screen clear) is sent before the next
// frame, on a terminal that answers at once and on one that answers each
// frame 300 ms late.
func TestMatrixMoveSendsOnlyAPicture(t *testing.T) {
	for _, delay := range []time.Duration{0, 300 * time.Millisecond} {
		env := newMenuEnv(t)
		pictureMatrix(t, env, `{"heat":true,"textFade":true,"projectText":true}`)
		s := newSlowTerminal(t, 1<<30, 1<<30)
		done := startSlowMatrix(t, env, s)
		s.mu.Lock()
		s.delay = delay
		s.mu.Unlock()
		time.Sleep(time.Second)
		for i := range 6 {
			mark := len(s.written())
			if i%2 == 0 {
				s.reply(keyDown)
			} else {
				s.reply(keyUp)
			}
			deadline := time.Now().Add(2 * time.Second)
			for !strings.Contains(s.written()[mark:], "\x1bP") {
				if time.Now().After(deadline) {
					t.Fatalf("delay %v, move %d: no frame within 2 s", delay, i)
				}
				time.Sleep(5 * time.Millisecond)
			}
			out := s.written()[mark:]
			before := out[:strings.Index(out, "\x1bP")]
			for _, text := range []string{"Journey onward.", "Create an account.", "Check your access.", "Disconnect.", ansi.ClearScreen()} {
				if strings.Contains(before, text) {
					t.Fatalf("delay %v, move %d: %q sent before the frame: %q", delay, i, text, before)
				}
			}
			time.Sleep(150 * time.Millisecond)
		}
		s.reply("D")
		endMatrix(t, done)
	}
}

// A script that animates without reading keys still has its frames
// answered, so it gets more than the first.
func TestScriptCanvasPacedWithoutKeyReads(t *testing.T) {
	s := newSlowTerminal(t, 1<<30, 1<<30)
	done := startScriptDoor(t, s, `var c = v3.gfx.canvas(480, 160, 'logo.png');
for (var t = 0; t < 4; t += 0.1) {
	c.drawImage('logo.png', 0, 0);
	c.heat(t, 0.05);
	v3.console.write("\x1b[1;1H");
	c.presentDelta();
	v3.util.sleep(50);
}`)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(handlerWait):
		t.Fatal("script did not finish")
	}
	if n := strings.Count(s.written(), "\x1bP"); n < 10 {
		t.Errorf("%d of 40 frames drawn, want the answers to let at least 10 go", n)
	}
}

// No frame goes while the probe's primary reply is owed, however late.
// An answer cut short still lets the next frame go, and a probe whose own
// primary reply was cut short leaves the next one to the frames.
func TestFrameAnswersCutShort(t *testing.T) {
	_, log := newSyncLog()
	newProbe := func(primary string) *daReplies {
		d := &daReplies{log: log, lateUntil: time.Now().Add(time.Minute), frames: pace.NewGate(), answered: make(chan struct{})}
		d.frames.HoldWhile(d.awaitingPrimary)
		if primary != "" {
			d.add([]byte(primary), true)
		}
		return d
	}
	d := newProbe(slowAnswer)
	if !d.frames.Begin() {
		t.Fatal("no frame went after the probe's primary reply")
	}
	d.add([]byte("\x1b[?62;4"), false)
	if d.frames.Waiting() {
		t.Error("an answer cut short left the frame waiting")
	}

	d = newProbe("")
	d.add([]byte(ansi.AudioWAVU8Yes[:8]), false) // an audio answer cut short
	if d.frames.Begin() {
		t.Fatal("a frame went while the probe awaited its primary reply")
	}
	d.add([]byte("\x1b[?62"), false) // the probe's own, cut short
	if !d.frames.Begin() {
		t.Fatal("no frame went after the probe lost its primary reply")
	}
	d.add([]byte(slowAnswer), true)
	if d.frames.Waiting() {
		t.Error("the frame's answer went to a probe that had lost its own")
	}

	// The probe's own answer, after its window, is still the probe's.
	d = newProbe("")
	d.lateUntil = time.Now().Add(-time.Second)
	if d.frames.Begin() {
		t.Fatal("a frame went while the probe's answer was owed, after its window")
	}
	d.add([]byte(slowAnswer), true)
	if !d.frames.Begin() {
		t.Fatal("no frame went after the probe's late answer")
	}
	d.add([]byte(slowAnswer), true)
	if d.frames.Waiting() {
		t.Error("the frame's answer did not reach the gate")
	}
}
