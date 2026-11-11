package menu

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
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
// out through the ramp reversed, and then the screen is restored.
func TestToastFadesInAndOut(t *testing.T) {
	env, log := fadeEnv(t, true, 20*time.Millisecond)
	toastDwell = 600 * time.Millisecond
	keys := env.readKey()
	env.sess.AddPage("Hello there")

	toastWaitFor(t, "the screen restored", func() bool {
		return strings.Contains(log.String(), fadeFrame(8, ">> Hello there")) &&
			strings.Count(log.String(), fadeFrame(8, ">> Hello there")) == 2 &&
			strings.Contains(env.term.Row(24), "BOTTOM TEXT")
	})
	text := ">> Hello there"
	full := string(ansi.ReplacePipeCodes([]byte(env.e.toastText(toast.Toast{Text: "Hello there"}))))
	if !indexesInOrder(log.String(),
		fadeFrame(8, text), fadeFrame(7, text), fadeFrame(15, text),
		full,
		fadeFrame(15, text), fadeFrame(7, text), fadeFrame(8, text)) {
		t.Errorf("frames out of order:\n%q", log.String())
	}
	select {
	case k := <-keys:
		t.Fatalf("read returned %d with no key pressed", k)
	default:
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

// slowWriter delays every write, as a slow link does.
type slowWriter struct {
	*wireLog
	delay time.Duration
}

func (s slowWriter) Write(p []byte) (int, error) {
	time.Sleep(s.delay)
	return s.wireLog.Write(p)
}

// Slow output does not stretch a fading toast past its dwell: the frames
// and waits share one deadline, so the row is back about when the dwell
// says, however long each write takes.
func TestToastFadeKeepsToDeadline(t *testing.T) {
	env, log := fadeEnv(t, true, 50*time.Millisecond)
	toastDwell = 600 * time.Millisecond
	SetSessionOutput(env.s, slowWriter{wireLog: log, delay: 80 * time.Millisecond})
	env.readKey()
	start := time.Now()
	env.sess.AddPage("Hello there")
	toastWaitFor(t, "the toast drawn", func() bool { return strings.Contains(log.String(), "Hello there") })
	toastWaitFor(t, "the screen restored", func() bool { return strings.Contains(env.term.Row(24), "BOTTOM TEXT") })
	// Six frames and a hold at 80ms a write would run well over a second
	// without the deadline; with it, the dwell plus one slow write or two.
	if took := time.Since(start); took > 900*time.Millisecond {
		t.Errorf("toast took %s against a 600ms dwell", took)
	}
}
