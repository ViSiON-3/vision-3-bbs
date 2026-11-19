package scripting

import (
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/pace"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// helloTerminal is the caller's terminal in the Hello door's tests. It
// records what the door writes and when, and, when answers is set, answers
// each device attributes query after delay, as a terminal that has read the
// frame before it.
type helloTerminal struct {
	answers bool
	delay   time.Duration
	in      chan []byte
	done    chan struct{}
	began   time.Time

	mu          sync.Mutex
	out         []byte
	marks       []helloMark // the size of out after each write
	frames      int         // writes that carried a sixel picture
	queries     int
	answered    int
	maxUnanswer int // the most frames ever unanswered at once
}

// helloMark is how much the door had written, and when.
type helloMark struct {
	Millis int64
	Size   int
}

func newHelloTerminal(answers bool, delay time.Duration) *helloTerminal {
	return &helloTerminal{answers: answers, delay: delay, in: make(chan []byte, 4096), done: make(chan struct{}), began: time.Now()}
}

func (s *helloTerminal) Read(p []byte) (int, error) {
	select {
	case b := <-s.in:
		return copy(p, b), nil
	case <-s.done:
		return 0, io.EOF
	}
}

func (s *helloTerminal) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.out = append(s.out, p...)
	s.marks = append(s.marks, helloMark{time.Since(s.began).Milliseconds(), len(s.out)})
	if bytes.Contains(p, []byte("\x1bP")) {
		s.frames++
	}
	queries := bytes.Count(p, []byte(pace.Query))
	s.queries += queries
	s.maxUnanswer = max(s.maxUnanswer, s.queries-s.answered)
	s.mu.Unlock()
	if !s.answers {
		return len(p), nil
	}
	for range queries {
		time.AfterFunc(s.delay, func() {
			s.mu.Lock()
			s.answered++
			s.mu.Unlock()
			s.in <- []byte("\x1b[?62;4c")
		})
	}
	return len(p), nil
}

// counts returns the sixel frames and queries written, the most frames
// ever unanswered at once, and how long the terminal has been connected.
func (s *helloTerminal) counts() (frames, queries, unanswered int, connected time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.frames, s.queries, s.maxUnanswer, time.Since(s.began)
}

func (s *helloTerminal) output() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.out...)
}

// helloRun is one caller in the Hello door.
type helloRun struct {
	term *helloTerminal
	errc chan error
	eng  *Engine
}

// helloPrevious is the test caller's previous call.
var helloPrevious = time.Date(2026, 11, 18, 21, 14, 0, 0, time.Local)

// startHello runs scripts/hello-vision3.js for a caller called Tester on
// node 3 of Test BBS, with the door's data files in place when data is set.
// sixel is what the terminal answered to the sixel probe.
func startHello(t *testing.T, sixel, data bool, delay time.Duration) *helloRun {
	t.Helper()
	return startHelloAs(t, &user.User{ID: 7, Handle: "Tester", TimesCalled: 12, LastLogin: time.Now(), PreviousLogin: helloPrevious}, sixel, data, delay)
}

// startHelloAs is startHello for the caller cur, on an 80x24 screen unless
// cur has a screen size.
func startHelloAs(t *testing.T, cur *user.User, sixel, data bool, delay time.Duration) *helloRun {
	t.Helper()
	return startHelloWithAudio(t, cur, sixel, data, false, "", delay)
}

// startHelloWithAudio is startHelloAs for a terminal that answered the audio
// query when audio is set, with tune, when it is not empty, as the contents of
// the door's hello-vision3-tune.b64.
func startHelloWithAudio(t *testing.T, cur *user.User, sixel, data, audio bool, tune string, delay time.Duration) *helloRun {
	t.Helper()
	repo := filepath.Join("..", "..")
	dir := t.TempDir()
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(filepath.Join(scriptsDir, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := []string{"hello-vision3.js"}
	if data {
		files = append(files, filepath.Join("data", "hello-vision3-logo.png"), filepath.Join("data", "hello-vision3-palette.png"))
	}
	for _, name := range files {
		b, err := os.ReadFile(filepath.Join(repo, "scripts", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(scriptsDir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if tune != "" {
		if err := os.WriteFile(filepath.Join(scriptsDir, "data", "hello-vision3-tune.b64"), []byte(tune), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	term := newHelloTerminal(sixel, delay)
	gate := pace.NewGate()
	trap := editor.NewReplyTrap(0, func(reply []byte, complete bool) {
		if complete && pace.IsAnswer(reply) {
			gate.Answer()
		}
	})
	sc := &SessionContext{
		Session: term, OutputMode: ansi.OutputModeCP437, UserID: cur.ID, UserHandle: cur.Handle, TimesCalled: cur.TimesCalled,
		ScreenWidth: cmp.Or(cur.ScreenWidth, 80), ScreenHeight: cmp.Or(cur.ScreenHeight, 24), NodeNumber: 3, SessionStartTime: time.Now(),
		BoardName: "Test BBS", BBSVersion: "t", Sixel: sixel, Audio: audio,
		Frames: gate, StripReplies: trap.StripReplies, TakeReply: trap.Take,
	}
	eng := NewEngine(context.Background(), sc, ScriptConfig{Script: "hello-vision3.js", WorkingDir: scriptsDir},
		&Providers{CurrentUser: cur, UserMgr: user.NewUserMgrForTest(cur)})
	run := &helloRun{term: term, errc: make(chan error, 1), eng: eng}
	t.Cleanup(func() { close(term.done); eng.Close() })
	go func() { run.errc <- eng.Run("hello-vision3.js") }()
	return run
}

// stillRunning fails the test if the door has ended.
func (r *helloRun) stillRunning(t *testing.T) {
	t.Helper()
	select {
	case err := <-r.errc:
		t.Fatalf("the door ended with no key pressed (error: %v)", err)
	default:
	}
}

// pressKey sends key and waits for the door to end without an error.
func (r *helloRun) pressKey(t *testing.T, key string) {
	t.Helper()
	r.term.in <- []byte(key)
	select {
	case err := <-r.errc:
		if err != nil {
			t.Fatalf("the door ended with an error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the door did not end on a key")
	}
}

// helloCleared is what the door's last write ends with: attributes reset,
// the cursor shown, and the screen cleared.
const helloCleared = "\x1b[0m\x1b[?25h\x1b[2J\x1b[H"

// A terminal that did not answer the sixel probe gets the ANSI version:
// block lettering and the scroller, and no sixel picture.
func TestHelloVision3TextSendsNoSixel(t *testing.T) {
	run := startHello(t, false, true, 0)
	time.Sleep(helloSeconds(2))
	run.stillRunning(t)
	run.pressKey(t, " ")
	out := run.term.output()
	if bytes.Contains(out, []byte("\x1bP")) {
		t.Error("the ANSI version sent a sixel picture")
	}
	if bytes.Contains(out, []byte(pace.Query)) {
		t.Error("the ANSI version sent a frame query")
	}
	// CP437's full block and half blocks, as the block lettering is drawn.
	for _, block := range []byte{0xdb, 0xdc, 0xdf} {
		if !bytes.Contains(out, []byte{block}) {
			t.Errorf("no block character %#x: the handle is not in block lettering", block)
		}
	}
	if !bytes.HasSuffix(out, []byte(helloCleared)) {
		t.Errorf("the door did not end with the screen cleared: ...%q", out[max(0, len(out)-40):])
	}
	logHelloRate(t, "ANSI version", run.term)
	captureHello(t, run.term)
}

// With the sixel probe answered but the door's pictures missing, the door
// draws the ANSI version.
func TestHelloVision3WithoutItsPicturesIsText(t *testing.T) {
	run := startHello(t, true, false, 0)
	time.Sleep(500 * time.Millisecond)
	run.stillRunning(t)
	run.pressKey(t, "q")
	if out := run.term.output(); bytes.Contains(out, []byte("\x1bP")) || !bytes.Contains(out, []byte{0xdb}) {
		t.Error("without its data files the door did not draw the ANSI version alone")
	}
}

// A terminal that answered the sixel probe gets the picture, every frame
// through the paced present: each is followed by the query, and no more
// than pace.InFlight are ever unanswered, however slowly the terminal
// answers.
func TestHelloVision3PictureIsPaced(t *testing.T) {
	for _, delay := range []time.Duration{0, 400 * time.Millisecond} {
		t.Run(fmt.Sprintf("answers after %v", delay), func(t *testing.T) {
			run := startHello(t, true, true, delay)
			time.Sleep(helloSeconds(3))
			run.stillRunning(t)
			run.pressKey(t, "\x1b[A") // any key, an arrow included
			term := run.term
			out := term.output()
			frames, queries, unanswered, connected := term.counts()
			if frames < 2 {
				t.Fatalf("%d sixel frames in %v, want an animation", frames, connected)
			}
			if queries != frames {
				t.Errorf("%d frames and %d queries: a frame went outside the paced present", frames, queries)
			}
			if unanswered > pace.InFlight {
				t.Errorf("%d frames were unanswered at once, more than %d", unanswered, pace.InFlight)
			}
			// A terminal that answers after delay takes pace.InFlight frames
			// in each delay, and the first pace.InFlight at once.
			if limit := (int(connected/max(delay, 1)) + 2) * pace.InFlight; delay > 0 && frames > limit {
				t.Errorf("%d frames in %v went to a terminal that answers after %v, more than %d", frames, connected, delay, limit)
			}
			if !bytes.HasSuffix(out, []byte(helloCleared)) {
				t.Errorf("the door did not end with the screen cleared: ...%q", out[max(0, len(out)-40):])
			}
			logHelloRate(t, "picture", term)
			captureHello(t, term)
		})
	}
}

// The scroller carries the caller's own details in both versions: they are
// one message.
func TestHelloVision3ScrollerCarriesDetails(t *testing.T) {
	run := startHello(t, false, true, 0)
	time.Sleep(200 * time.Millisecond)
	run.pressKey(t, "x")
	v, err := run.eng.vm.RunString("message")
	if err != nil {
		t.Fatal(err)
	}
	message := v.String()
	last := strings.ToUpper(helloPrevious.Format("2 Jan 2006")) + " AT " + helloPrevious.Format("15:04")
	for _, want := range []string{"HELLO TESTER!", "WELCOME TO TEST BBS", "CALL NUMBER 12", "YOUR LAST CALL WAS " + last, "NODE 3"} {
		if !strings.Contains(message, want) {
			t.Errorf("the scroller's message lacks %q:\n%s", want, message)
		}
	}
}

// A first-time caller, and handles too long for one line of lettering, are
// drawn by both versions without an error. The handles are one that splits
// into two lines of block letters and one that does not.
func TestHelloVision3LongHandlesAndFirstCall(t *testing.T) {
	for _, handle := range []string{"Flying Dutchman 2000", "Supercalifragilisticexpialidocious"} {
		for _, sixel := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s sixel %v", handle, sixel), func(t *testing.T) {
				run := startHelloAs(t, &user.User{ID: 9, Handle: handle, TimesCalled: 1, LastLogin: time.Now()}, sixel, true, 0)
				time.Sleep(helloSeconds(2))
				run.stillRunning(t)
				run.pressKey(t, "\r")
				if got := bytes.Contains(run.term.output(), []byte("\x1bP")); got != sixel {
					t.Errorf("sixel picture sent = %v, want %v", got, sixel)
				}
				v, err := run.eng.vm.RunString("message")
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range []string{"HELLO " + strings.ToUpper(handle) + "!", "THIS IS YOUR FIRST CALL"} {
					if !strings.Contains(v.String(), want) {
						t.Errorf("the scroller's message lacks %q:\n%s", want, v)
					}
				}
				captureHello(t, run.term)
			})
		}
	}
}

// A screen smaller than 80x24 gets the greeting and the details as lines,
// whatever the terminal answered, and still waits for a key.
func TestHelloVision3SmallScreenIsLines(t *testing.T) {
	cur := &user.User{ID: 7, Handle: "Tes|04ter", TimesCalled: 12, LastLogin: time.Now(), PreviousLogin: helloPrevious, ScreenWidth: 40, ScreenHeight: 20}
	run := startHelloAs(t, cur, true, true, 0)
	time.Sleep(300 * time.Millisecond)
	run.stillRunning(t)
	run.pressKey(t, "x")
	out := run.term.output()
	if bytes.Contains(out, []byte("\x1bP")) {
		t.Error("a 40x20 screen was sent a sixel picture")
	}
	// The handle is written as it is: its | is not a colour code.
	for _, want := range []string{"Tes|04ter", "Test BBS", "Your last call was"} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("the lines lack %q:\n%q", want, out)
		}
	}
}

// The HELLO door in the stock door list runs the new script from
// scripts/, and the teaching example is still shipped.
func TestHelloDoorRunsHelloVision3(t *testing.T) {
	repo := filepath.Join("..", "..")
	for _, path := range []string{filepath.Join("templates", "configs", "doors.json")} {
		raw, err := os.ReadFile(filepath.Join(repo, path))
		if err != nil {
			t.Fatal(err)
		}
		var doors []struct {
			Code, Type, Script string
			WorkingDirectory   string `json:"working_directory"`
		}
		if err := json.Unmarshal(raw, &doors); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		found := false
		for _, d := range doors {
			if d.Code != "HELLO" {
				continue
			}
			found = true
			if d.Type != "v3_script" || d.Script != "hello-vision3.js" || d.WorkingDirectory != "scripts" {
				t.Errorf("%s: HELLO runs %s %q in %q, want v3_script hello-vision3.js in scripts", path, d.Type, d.Script, d.WorkingDirectory)
			}
		}
		if !found {
			t.Errorf("%s has no HELLO door", path)
		}
	}
	for _, name := range []string{"hello-vision3.js", filepath.Join("examples", "hello.js"),
		filepath.Join("data", "hello-vision3-logo.png"), filepath.Join("data", "hello-vision3-palette.png")} {
		if _, err := os.Stat(filepath.Join(repo, "scripts", name)); err != nil {
			t.Errorf("scripts/%s: %v", name, err)
		}
	}
}

// logHelloRate logs the frames and bytes a second the door sent.
func logHelloRate(t *testing.T, what string, term *helloTerminal) {
	t.Helper()
	term.mu.Lock()
	defer term.mu.Unlock()
	if len(term.marks) == 0 {
		return
	}
	seconds := float64(term.marks[len(term.marks)-1].Millis) / 1000
	t.Logf("%s: %d sixel frames and %d bytes in %.1f s: %.1f frames/s, %.0f bytes/s",
		what, term.frames, len(term.out), seconds, float64(term.frames)/seconds, float64(len(term.out))/seconds)
}

// helloSeconds is how long a captured run lasts: seconds, or the number in
// HELLO_VISION3_SECONDS when HELLO_VISION3_CAPTURE is set.
func helloSeconds(seconds int) time.Duration {
	if os.Getenv("HELLO_VISION3_CAPTURE") != "" {
		if n, err := strconv.Atoi(os.Getenv("HELLO_VISION3_SECONDS")); err == nil && n > 0 {
			seconds = n
		}
	}
	return time.Duration(seconds) * time.Second
}

// captureHello writes what the door sent to the directory named by
// HELLO_VISION3_CAPTURE, for drawing its frames as pictures: stream.bin is
// the bytes, and marks.json how many of them had been sent by when.
func captureHello(t *testing.T, term *helloTerminal) {
	t.Helper()
	dir := os.Getenv("HELLO_VISION3_CAPTURE")
	if dir == "" {
		return
	}
	term.mu.Lock()
	defer term.mu.Unlock()
	marks, err := json.Marshal(term.marks)
	if err != nil {
		t.Fatal(err)
	}
	name := strings.NewReplacer("/", "-", " ", "-").Replace(t.Name())
	for file, b := range map[string][]byte{name + ".stream.bin": term.out, name + ".marks.json": marks} {
		if err := os.WriteFile(filepath.Join(dir, file), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// placeholderTune is a WAV file in the board's audio format (8-bit unsigned
// PCM, 22050 Hz, mono) of n samples of a square wave, as base64 text. The
// tests make their own; no tune is committed.
func placeholderTune(n int) string {
	data := make([]byte, n)
	for i := range data {
		data[i] = 64 + byte(i/50%2)*128
	}
	b := []byte("RIFF\x00\x00\x00\x00WAVEfmt ")
	b = binary.LittleEndian.AppendUint32(b, 16)
	b = binary.LittleEndian.AppendUint16(b, 1)
	b = binary.LittleEndian.AppendUint16(b, 1)
	b = binary.LittleEndian.AppendUint32(b, 22050)
	b = binary.LittleEndian.AppendUint32(b, 22050)
	b = binary.LittleEndian.AppendUint16(b, 1)
	b = binary.LittleEndian.AppendUint16(b, 8)
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(n))
	b = append(b, data...)
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	return base64.StdEncoding.EncodeToString(b)
}

// helloUser is the caller the tune tests use.
func helloUser() *user.User {
	return &user.User{ID: 7, Handle: "Tester", TimesCalled: 12, LastLogin: time.Now(), PreviousLogin: helloPrevious}
}

// The audio sequences the door writes.
const (
	helloLoad  = "\x1b_SyncTERM:A;LoadBlob;S=253;"
	helloQueue = "\x1b_SyncTERM:A;Queue;C=3;S=253;L\x1b\\"
	helloFlush = "\x1b_SyncTERM:A;Flush;C=3;O=900\x1b\\"
)

// On a terminal that answered the audio query the door sends the tune once,
// after the first picture frame, looping, and stops it before its last write.
// The size runs up to the 4 MiB the board lets a script read, to show that
// v3.console.write carries a tune of that size unchanged.
func TestHelloVision3PlaysItsTune(t *testing.T) {
	for _, tc := range []struct {
		name  string
		sixel bool
		n     int
	}{{"ansi", false, 3000}, {"sixel", true, 3000}, {"sixel largest", true, 3<<20 - 44}, {"ansi largest", false, 3<<20 - 44}} {
		t.Run(tc.name, func(t *testing.T) {
			tune := placeholderTune(tc.n)
			if len(tune) > maxFSFileBytes {
				t.Fatalf("test tune is %d bytes, over what v3.fs.read reads", len(tune))
			}
			run := startHelloWithAudio(t, helloUser(), tc.sixel, true, true, tune, 0)
			time.Sleep(helloSeconds(1))
			run.stillRunning(t)
			run.pressKey(t, " ")
			out := string(run.term.output())
			want := helloLoad + tune + "\x1b\\" + helloQueue
			if n := strings.Count(out, want); n != 1 {
				t.Fatalf("the tune was sent %d times, want once, byte for byte, looping (output is %d bytes)", n, len(out))
			}
			if n := strings.Count(out, "SyncTERM:A;LoadBlob"); n != 1 {
				t.Errorf("%d sounds loaded", n)
			}
			if n := strings.Count(out, helloFlush); n != 1 || strings.Index(out, helloFlush) < strings.Index(out, helloQueue) {
				t.Errorf("the tune was not stopped once, after it started")
			}
			if tc.sixel {
				if frame := strings.Index(out, "\x1bP"); frame < 0 || strings.Index(out, helloLoad) < frame {
					t.Errorf("the tune (at %d) was not sent after a picture frame (at %d)", strings.Index(out, helloLoad), frame)
				}
			}
			if !strings.HasSuffix(out, helloCleared) {
				t.Errorf("the door did not end with the screen cleared: ...%q", out[max(0, len(out)-40):])
			}
		})
	}
}

// A terminal that did not answer the audio query is sent no audio bytes, with
// the tune there.
func TestHelloVision3SendsNoAudioWithoutTheAnswer(t *testing.T) {
	tune := placeholderTune(3000)
	for _, sixel := range []bool{false, true} {
		run := startHelloWithAudio(t, helloUser(), sixel, true, false, tune, 0)
		time.Sleep(helloSeconds(1))
		run.stillRunning(t)
		run.pressKey(t, " ")
		if out := run.term.output(); bytes.Contains(out, []byte("SyncTERM:A")) || bytes.Contains(out, []byte(tune[:40])) {
			t.Errorf("sixel=%v: audio bytes sent to a terminal that did not answer the audio query", sixel)
		}
	}
}

// With no tune file, or one that is not a WAV, the door is silent and runs on.
func TestHelloVision3WithoutAUsableTuneIsSilent(t *testing.T) {
	for name, tune := range map[string]string{"missing": "", "not a WAV": "bm90IGEgc291bmQ=", "only the prefix": "UklGR", "prefix with junk": "UklGRabcdefghijkl" + strings.Repeat("A", 100), "WAV text with a stray character": "UklGRvCuAQBXQVZFZm10IBAAAAABAAEAIlYAACJWAAABAAgAZGF0YQ!!!!!!!!!!!!!!!!!!!!!!!!"} {
		t.Run(name, func(t *testing.T) {
			run := startHelloWithAudio(t, helloUser(), true, true, true, tune, 0)
			time.Sleep(helloSeconds(1))
			run.stillRunning(t)
			run.pressKey(t, " ")
			out := run.term.output()
			if bytes.Contains(out, []byte("SyncTERM:A")) {
				t.Error("audio bytes sent without a usable tune")
			}
			if !bytes.HasSuffix(out, []byte(helloCleared)) {
				t.Error("the door did not run to its end")
			}
		})
	}
}

// The scripting engine tells a script whether the terminal answered the
// audio query, and only that.
func TestGfxAudioFlag(t *testing.T) {
	for _, audio := range []bool{false, true} {
		h := newHarness(t, harnessOpts{session: func(sc *SessionContext) { sc.Audio = audio }})
		if got := h.eval("v3.gfx.audio").ToBoolean(); got != audio {
			t.Errorf("v3.gfx.audio = %v with the answer %v", got, audio)
		}
	}
}

// The first picture frame is written before any of the tune. The test logs when
// it reaches a terminal that takes writes at once, with and without a tune of
// the largest size; over a real link the extra time is the tune's bytes over
// the link's rate, after the first frame.
func TestHelloVision3FirstFrameComesBeforeTune(t *testing.T) {
	first := func(tune string) (ms int64, size int, out string) {
		run := startHelloWithAudio(t, helloUser(), true, true, tune != "", tune, 0)
		time.Sleep(helloSeconds(1))
		run.stillRunning(t)
		run.pressKey(t, " ")
		term := run.term
		term.mu.Lock()
		defer term.mu.Unlock()
		for _, m := range term.marks {
			if bytes.Contains(term.out[:m.Size], []byte("\x1bP")) {
				return m.Millis, m.Size, string(term.out)
			}
		}
		t.Fatal("no sixel frame")
		return
	}
	msOff, bytesOff, _ := first("")
	msOn, bytesOn, out := first(placeholderTune(3<<20 - 44))
	t.Logf("first sixel frame: %d ms after %d bytes without a tune, %d ms after %d bytes with a 4 MiB one", msOff, bytesOff, msOn, bytesOn)
	if i, j := strings.Index(out, "\x1bP"), strings.Index(out, helloLoad); i < 0 || j < i {
		t.Errorf("the tune (at %d) was sent before the first picture frame (at %d)", j, i)
	}
	if bytesOn > bytesOff+1024 {
		t.Errorf("%d bytes were written up to the first frame with the tune and %d without it", bytesOn, bytesOff)
	}
}
