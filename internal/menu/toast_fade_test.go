package menu

import (
	"bytes"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/toast"
)

// wireLog records what the toast overlay writes, and passes it on to the
// screen.
type wireLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
	to  interface{ Write([]byte) (int, error) }
}

func (l *wireLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	l.buf.Write(p)
	l.mu.Unlock()
	return l.to.Write(p)
}

func (l *wireLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// fadeEnv is a toast env with fading on through 8,7,15 at step, and the
// overlay's writes recorded.
func fadeEnv(t *testing.T, fade bool, step time.Duration) (*toastEnv, *wireLog) {
	t.Helper()
	env := newToastEnv(t)
	cfg := env.e.GetServerConfig()
	cfg.ToastFade, cfg.ToastFadeMs = fade, int(step/time.Millisecond)
	env.e.SetServerConfig(cfg)
	env.e.SetTheme(config.ThemeConfig{ToastFadeColors: "8,7,15"})
	log := &wireLog{to: env.s}
	SetSessionOutput(env.s, log)
	return env, log
}

// fadeFrame is a fade step as the overlay draws it: text in colour c.
func fadeFrame(c int, text string) string {
	return string(ansi.ReplacePipeCodes([]byte(map[int]string{8: "|08", 7: "|07", 15: "|15"}[c]))) + text
}

// indexesInOrder reports whether each of parts appears in s after the one
// before it.
func indexesInOrder(s string, parts ...string) bool {
	at := 0
	for _, p := range parts {
		i := strings.Index(s[at:], p)
		if i < 0 {
			return false
		}
		at += i + len(p)
	}
	return true
}

// A fading toast comes in through the ramp, shows in its own colours, goes
// out through the ramp reversed, and then the screen is restored. The step
// is well above any host's timer resolution, so the real clock cannot cost
// a frame; TestToastFadeSurvivesCoarseTimers covers the shortest step.
func TestToastFadesInAndOut(t *testing.T) {
	env, log := fadeEnv(t, true, 50*time.Millisecond)
	toastDwell = time.Second
	keys := env.readKey()
	env.sess.AddPage("Hello there")
	waitFadeRestored(t, env, log)
	assertFadeFrames(t, env, log)
	select {
	case k := <-keys:
		t.Fatalf("read returned %d with no key pressed", k)
	default:
	}
}

// windowsTick is the default Windows timer and clock resolution.
const windowsTick = 15625 * time.Microsecond

// coarseTimers gives the toast a virtual clock that moves in windowsTick
// steps, as Windows' timer and clock do: a wait ends on the first tick at
// or after its due time, and writes take no time. It does not read the
// host's clock, so the host's load cannot change the outcome.
func coarseTimers(t *testing.T) {
	t.Helper()
	var mu sync.Mutex
	now := time.Now().Truncate(windowsTick)
	toastNow = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	toastWait = func(_ *editor.InputHandler, d time.Duration) editor.WaitOutcome {
		mu.Lock()
		defer mu.Unlock()
		due := now.Add(d)
		if now = due.Truncate(windowsTick); now.Before(due) {
			now = now.Add(windowsTick)
		}
		return editor.WaitElapsed
	}
	t.Cleanup(func() { toastWait, toastNow = (*editor.InputHandler).WaitFor, time.Now })
}

// With every wait running late, as on Windows' 15.6ms timer, the fade at
// its shortest step still draws every frame in and out before the deadline.
func TestToastFadeSurvivesCoarseTimers(t *testing.T) {
	coarseTimers(t) // first, so its cleanup runs after the env has stopped reading
	env, log := fadeEnv(t, true, 20*time.Millisecond)
	toastDwell = 600 * time.Millisecond
	env.readKey()
	env.sess.AddPage("Hello there")
	waitFadeRestored(t, env, log)
	assertFadeFrames(t, env, log)
}

// waitFadeRestored waits for the row under the toast to be written back.
// The screen was drawn before log was attached, so the only BOTTOM TEXT in
// log is the restore.
func waitFadeRestored(t *testing.T, env *toastEnv, log *wireLog) {
	t.Helper()
	toastWaitFor(t, "the screen restored", func() bool {
		return strings.Contains(log.String(), "BOTTOM TEXT") && strings.Contains(env.term.Row(24), "BOTTOM TEXT")
	})
}

// assertFadeFrames checks that log holds the fade in, the toast in its own
// colours, and the fade out, in that order, before the restore.
func assertFadeFrames(t *testing.T, env *toastEnv, log *wireLog) {
	t.Helper()
	text := ">> Hello there"
	full := string(ansi.ReplacePipeCodes([]byte(env.e.toastText(toast.Toast{Text: "Hello there"}))))
	if !indexesInOrder(log.String(),
		fadeFrame(8, text), fadeFrame(7, text), fadeFrame(15, text),
		full,
		fadeFrame(15, text), fadeFrame(7, text), fadeFrame(8, text),
		"BOTTOM TEXT") {
		t.Errorf("frames missing or out of order:\n%q", log.String())
	}
}

// A key during the fade stops it there and restores the screen at once,
// and the key reaches the read.
func TestToastFadeStopsOnKey(t *testing.T) {
	env, log := fadeEnv(t, true, 400*time.Millisecond)
	toastDwell = 10 * time.Second
	keys := env.readKey()
	env.sess.AddPage("Hello there")
	text := ">> Hello there"
	toastWaitFor(t, "the first fade step", func() bool { return strings.Contains(log.String(), fadeFrame(8, text)) })

	env.s.Send("x")
	select {
	case k := <-keys:
		if k != 'x' {
			t.Errorf("read key %d, want x", k)
		}
	case <-time.After(screenWait):
		t.Fatal("key never reached the read")
	}
	toastWaitFor(t, "the screen restored", func() bool { return strings.Contains(env.term.Row(24), "BOTTOM TEXT") })
	if got := log.String(); strings.Contains(got, fadeFrame(7, text)) || strings.Contains(got, fadeFrame(15, text)) {
		t.Errorf("fade went on after the key:\n%q", got)
	}
}

// With fading off, a toast appears and goes with no fade steps at all.
func TestToastFadeOff(t *testing.T) {
	env, log := fadeEnv(t, false, 20*time.Millisecond)
	toastDwell = 100 * time.Millisecond
	env.readKey()
	env.sess.AddPage("Hello there")
	toastWaitFor(t, "the toast shown and gone", func() bool {
		return strings.Contains(log.String(), "Hello there") && strings.Contains(env.term.Row(24), "BOTTOM TEXT")
	})
	for _, c := range []int{8, 7, 15} {
		if strings.Contains(log.String(), fadeFrame(c, ">> Hello there")) {
			t.Errorf("fade step %d drawn with fading off", c)
		}
	}
}

// A dwell too short for the fade shows the toast without one, rather than
// cutting it short.
func TestToastFadeSkippedWhenDwellTooShort(t *testing.T) {
	env, log := fadeEnv(t, true, 100*time.Millisecond)
	toastDwell = 300 * time.Millisecond // the fade alone would take 600ms
	env.readKey()
	env.sess.AddPage("Hello there")
	toastWaitFor(t, "the toast shown and gone", func() bool {
		return strings.Contains(log.String(), "Hello there") && strings.Contains(env.term.Row(24), "BOTTOM TEXT")
	})
	if strings.Contains(log.String(), fadeFrame(8, ">> Hello there")) {
		t.Error("faded within a dwell too short for it")
	}
}

// A sysop chat request or a window change during the fade (an interrupted
// wait) hands the screen back at once: the fade stops and the row is
// restored, with no more frames drawn over it.
func TestToastFadeStopsOnInterrupt(t *testing.T) {
	env, log := fadeEnv(t, true, 400*time.Millisecond)
	toastDwell = 10 * time.Second
	env.readKey()
	env.sess.AddPage("Hello there")
	text := ">> Hello there"
	toastWaitFor(t, "the first fade step", func() bool { return strings.Contains(log.String(), fadeFrame(8, text)) })

	start := time.Now()
	getSessionIH(env.s).Interrupt()
	toastWaitFor(t, "the screen restored", func() bool { return strings.Contains(env.term.Row(24), "BOTTOM TEXT") })
	if took := time.Since(start); took > 300*time.Millisecond {
		t.Errorf("restore took %s after the interruption", took)
	}
	time.Sleep(500 * time.Millisecond) // a further step would have drawn by now
	if got := log.String(); strings.Contains(got, fadeFrame(7, text)) || strings.Contains(got, fadeFrame(15, text)) {
		t.Errorf("fade went on after the interruption:\n%q", got)
	}
}

// slowWriter delays every write, as a slow link does, and records the
// longest write, which on a coarse timer runs past delay.
type slowWriter struct {
	*wireLog
	delay   time.Duration
	longest *atomic.Int64
}

func (s slowWriter) Write(p []byte) (int, error) {
	began := time.Now()
	time.Sleep(s.delay)
	n, err := s.wireLog.Write(p)
	if took := int64(time.Since(began)); took > s.longest.Load() {
		s.longest.Store(took)
	}
	return n, err
}

// Slow output does not stretch a fading toast past its dwell: the frames
// and waits share one deadline, so the row is back about when the dwell
// says, however long each write takes.
func TestToastFadeKeepsToDeadline(t *testing.T) {
	env, log := fadeEnv(t, true, 50*time.Millisecond)
	toastDwell = 600 * time.Millisecond
	sw := slowWriter{wireLog: log, delay: 150 * time.Millisecond, longest: new(atomic.Int64)}
	SetSessionOutput(env.s, sw)
	env.readKey()
	start := time.Now()
	env.sess.AddPage("Hello there")
	toastWaitFor(t, "the toast drawn", func() bool { return strings.Contains(log.String(), "Hello there") })
	toastWaitFor(t, "the screen restored", func() bool { return strings.Contains(env.term.Row(24), "BOTTOM TEXT") })
	took := time.Since(start)
	// The fade in and the toast's own frame use up the dwell. Without the
	// deadline the three fade-out frames would still be drawn, about 1.25s
	// in all; with it, the dwell, the write in flight when it passes, and
	// the restore, with each write as long as the longest one really took
	// and 100ms for the wake-up, the poll and a coarse clock.
	write := time.Duration(sw.longest.Load())
	if limit := 600*time.Millisecond + 2*write + 100*time.Millisecond; took > limit {
		t.Errorf("toast took %s against a 600ms dwell (limit %s, longest write %s)", took, limit, write)
	}
	// The toast's own frame ends past the deadline, so no fade-out frame
	// may follow it: each colour shows once, in the fade in, at most.
	for _, c := range []int{8, 7, 15} {
		if n := strings.Count(log.String(), fadeFrame(c, ">> Hello there")); n > 1 {
			t.Errorf("colour %d drawn %d times: a fade-out frame started past the deadline:\n%q", c, n, log.String())
		}
	}
}

// countingSlowWriter counts writes and takes delay over each.
type countingSlowWriter struct {
	delay time.Duration
	n     int
}

func (c *countingSlowWriter) Write(p []byte) (int, error) {
	time.Sleep(c.delay)
	c.n++
	return len(p), nil
}

// A frame write that crosses the deadline ends the fade at once: no wait
// after it, and no further frame. It ends as elapsed even with an
// interruption waiting, since the deadline passed first.
func TestFadeToastStopsWhenAWriteCrossesTheDeadline(t *testing.T) {
	env, _ := fadeEnv(t, true, 80*time.Millisecond)
	ih := getSessionIH(env.s)
	w := &countingSlowWriter{delay: 150 * time.Millisecond}
	lay := toastDraw{row: 24, col: 1, line: "Hello", mode: ansi.OutputModeUTF8}
	ih.Interrupt()

	start := time.Now()
	out := fadeToast(ih, lay, w, []int{8, 7, 15}, 80*time.Millisecond, false, start, start.Add(100*time.Millisecond))
	took := time.Since(start)
	if out != editor.WaitElapsed {
		t.Errorf("ended %v, want elapsed", out)
	}
	if w.n != 1 {
		t.Errorf("%d frames drawn, want 1", w.n)
	}
	// The one write (150ms) and nothing after it: a wait of the step on top
	// would make it 230ms.
	if took > 200*time.Millisecond {
		t.Errorf("took %s: waited after the write that crossed the deadline", took)
	}
}

// A toast whose read has no time left by the time it would be drawn is
// not drawn: it stays queued for the next wait.
func TestToastNotDrawnPastDeadline(t *testing.T) {
	env, log := fadeEnv(t, false, 20*time.Millisecond)
	env.sess.AddPage("Hello there")
	ih := getSessionIH(env.s)
	// A clock that moves on a millisecond each time it is read: the 1ns
	// limit has passed by the time the toast would be drawn, on any
	// platform's timer resolution.
	base, mu, calls := time.Now(), sync.Mutex{}, 0
	toastNow = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		calls++
		return base.Add(time.Duration(calls) * time.Millisecond)
	}
	t.Cleanup(func() { toastNow = time.Now })
	env.e.serviceToasts(env.s, ih, env.sess, time.Nanosecond)
	if strings.Contains(log.String(), "Hello there") {
		t.Error("drawn after the read's time ran out")
	}
	if page, ok := env.sess.PeekPage(); !ok || page != "Hello there" {
		t.Errorf("toast not kept for later: %q %v", page, ok)
	}
}
