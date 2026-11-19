package menu

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
)

// The replies are the forms documented at syncterm.bbsdev.net/cterm.html
// ("CSI Ps c" and "CSI < Ps c") and by xterm's ctlseqs for DA1.
func TestSixelFromReplies(t *testing.T) {
	const cterm = "\x1b[=67;84;101;114;109;"
	tests := []struct {
		name         string
		ext, primary string
		want         bool
	}{
		{"primary lists 4", "", "\x1b[?62;4;22c", true},
		{"primary lists 4 last", "", "\x1b[?63;1;2;4c", true},
		{"primary answers without 4", "", "\x1b[?62;1;2;6;22c", false},
		{"4 only inside other parameters", "", "\x1b[?64;14;42c", false},
		{"44 is not 4", "", "\x1b[?62;44c", false},
		{"VT132 class 4 is not an attribute", "", "\x1b[?4;2c", false},
		{"CTerm reply with feature 4", "\x1b[<0;1;2;3;4;5;6;7c", cterm + "1;156c", true},
		{"CTerm reply without feature 4", "\x1b[<0;1;2;3;5;6;7;8c", cterm + "1;156c", false},
		{"extended 14 and 24 are not 4", "\x1b[<0;14;24c", cterm + "1;156c", false},
		{"CTerm 1.188", "", cterm + "1;188c", false},
		{"CTerm 1.189", "", cterm + "1;189c", true},
		{"CTerm 1.206", "", cterm + "1;206c", true},
		{"CTerm 1.316", "", cterm + "1;316c", true},
		{"CTerm 2.0", "", cterm + "2;0c", true},
		{"CTerm with fork parameters", "", cterm + "1;156;4c", false},
		{"IcyTERM's DA is not CTerm", "", "\x1b[=73;99;121;84;101;114;109;0;4;1c", false},
		{"silent", "", "", false},
		{"extended only", "\x1b[<0;4c", "", true},
		{"extended reply must start with 0", "\x1b[<1;4c", cterm + "1;156c", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sixelFromReplies([]byte(tt.ext), []byte(tt.primary)); got != tt.want {
				t.Errorf("sixelFromReplies(%q, %q) = %v, want %v", tt.ext, tt.primary, got, tt.want)
			}
		})
	}
}

// probeSession is a terminal the test answers for when it chooses. Like the
// SSH and telnet sessions, its reads can be interrupted, so a stopped input
// handler stops reading.
type probeSession struct {
	*testSession
	term string
	data chan []byte

	mu        sync.Mutex
	pending   []byte
	interrupt <-chan struct{}
}

func newProbeSession(term string) *probeSession {
	return &probeSession{testSession: newTestSession(""), term: term, data: make(chan []byte, 16)}
}

func (s *probeSession) SetReadInterrupt(ch <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interrupt = ch
}

func (s *probeSession) Read(p []byte) (int, error) {
	s.mu.Lock()
	intr := s.interrupt
	if len(s.pending) == 0 {
		s.mu.Unlock()
		select {
		case d := <-s.data:
			s.mu.Lock()
			s.pending = d
		case <-intr:
			return 0, io.ErrClosedPipe
		}
	}
	defer s.mu.Unlock()
	n := copy(p, s.pending)
	s.pending = s.pending[n:]
	return n, nil
}

func (s *probeSession) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.testSession.Write(p)
}

func (s *probeSession) written() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out.String()
}

func (s *probeSession) Pty() (ssh.Pty, <-chan ssh.Window, bool) {
	return ssh.Pty{Term: s.term}, nil, true
}

// reply sends chunks as the terminal, pausing between them so each is its
// own read.
func (s *probeSession) reply(chunks ...string) {
	go func() {
		for i, c := range chunks {
			if i > 0 {
				time.Sleep(20 * time.Millisecond)
			}
			s.data <- []byte(c)
		}
	}()
}

// readKeyBounded reads the next key at the session's prompt, failing with
// an idle timeout rather than hanging if the key was lost.
func readKeyBounded(s ssh.Session) (int, error) {
	ih := getSessionIH(s)
	ih.SetSessionIdleTimeout(30 * time.Second)
	return ih.ReadKey()
}

func (s *probeSession) cleanup(t *testing.T) {
	t.Cleanup(func() {
		resetSessionIH(s)
		ClearSessionIdleTimeout(s)
	})
}

// syncLog is a slog handler's output that tests can read while the probe
// may still log from the input goroutine.
type syncLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *syncLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *syncLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func newSyncLog() (*syncLog, *slog.Logger) {
	l := &syncLog{}
	return l, slog.New(slog.NewTextHandler(l, nil))
}

func TestProbeTerminal(t *testing.T) {
	const cterm = "\x1b[=67;84;101;114;109;"
	tests := []struct {
		name      string
		encoding  bool
		chunks    []string
		wantMode  ansi.OutputMode
		wantSixel bool
	}{
		{"primary lists 4", true, []string{"\x1b[2;2R\x1b[?62;4;22c"}, ansi.OutputModeUTF8, true},
		{"primary without 4", true, []string{"\x1b[2;2R\x1b[?62;1;22c"}, ansi.OutputModeUTF8, false},
		{"4 only inside another parameter", true, []string{"\x1b[2;2R\x1b[?64;14;42c"}, ansi.OutputModeUTF8, false},
		{"CTerm with feature 4", true, []string{"\x1b[2;4R\x1b[<0;1;2;3;4;5;6;7c" + cterm + "1;156c"}, ansi.OutputModeCP437, true},
		{"CTerm without feature 4", true, []string{"\x1b[2;4R\x1b[<0;1;2;3c" + cterm + "1;156c"}, ansi.OutputModeCP437, false},
		{"CTerm 1.189 to 1.206", true, []string{"\x1b[2;4R" + cterm + "1;200c"}, ansi.OutputModeCP437, true},
		{"DA but no cursor report", true, []string{"\x1b[?62;1c"}, ansi.OutputModeCP437, false},
		{"split reply", true, []string{"\x1b[2;", "2R\x1b", "[?6", "2;4", "c"}, ansi.OutputModeUTF8, true},
		{"encoding forced, sixel still asked", false, []string{"\x1b[?62;4c"}, ansi.OutputModeAuto, true},
		{"secondary reply does not stand for the primary", true, []string{"\x1b[2;2R\x1b[>0;1;0c", "\x1b[?62;4c"}, ansi.OutputModeUTF8, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newProbeSession("xterm")
			s.cleanup(t)
			s.reply(tt.chunks...)
			_, log := newSyncLog()
			mode := ProbeTerminal(s, 10*time.Second, tt.encoding, log)
			if mode != tt.wantMode {
				t.Errorf("mode = %v, want %v", mode, tt.wantMode)
			}
			if got := sessionSixel(s, nil); got != tt.wantSixel {
				t.Errorf("sessionSixel = %v, want %v", got, tt.wantSixel)
			}
			want := ansi.DAExtendedQuery + audioQueries + ansi.DAPrimaryQuery + ansi.ProbeLineClear
			if tt.encoding {
				want = ansi.EncodingProbe + ansi.DAExtendedQuery + audioQueries + ansi.DAPrimaryQuery + ansi.EncodingProbeClear
			}
			if got := s.written(); got != want {
				t.Errorf("wrote %q, want %q", got, want)
			}
		})
	}
}

// The extended query goes first, so the primary reply is the last answer
// and the probe can stop at it.
func TestDAQueriesOrder(t *testing.T) {
	if ansi.DAExtendedQuery != "\x1b[<c" || ansi.DAPrimaryQuery != "\x1b[c" {
		t.Fatalf("queries = %q, %q", ansi.DAExtendedQuery, ansi.DAPrimaryQuery)
	}
}

// The wait ends when the primary reply arrives, not at the cursor report or
// the timeout.
func TestProbeTerminalStopsAtPrimaryReply(t *testing.T) {
	s := newProbeSession("xterm")
	s.cleanup(t)
	_, log := newSyncLog()
	done := make(chan ansi.OutputMode, 1)
	go func() { done <- ProbeTerminal(s, time.Minute, true, log) }()
	s.reply("\x1b[2;2R")
	select {
	case <-done:
		t.Fatal("probe ended at the cursor report, before the primary reply")
	case <-time.After(300 * time.Millisecond):
	}
	s.reply("\x1b[?62;4c")
	select {
	case mode := <-done:
		if mode != ansi.OutputModeUTF8 || !sessionSixel(s, nil) {
			t.Errorf("mode = %v, sixel = %v", mode, sessionSixel(s, nil))
		}
	case <-time.After(30 * time.Second):
		t.Fatal("probe did not end at the primary reply")
	}
}

// A terminal that answers device attributes but not the cursor query does
// not hold the probe to its timeout.
func TestProbeTerminalPrimaryWithoutCursorReport(t *testing.T) {
	s := newProbeSession("xterm")
	s.cleanup(t)
	s.reply("\x1b[?62;1c")
	_, log := newSyncLog()
	start := time.Now()
	mode := ProbeTerminal(s, time.Minute, true, log)
	if d := time.Since(start); d > 30*time.Second {
		t.Fatalf("probe took %v", d)
	}
	if mode != ansi.OutputModeCP437 || sessionSixel(s, nil) {
		t.Errorf("mode = %v, sixel = %v; want CP437, false", mode, sessionSixel(s, nil))
	}
}

// Keys typed during the probe reach the login prompt when the encoding is
// forced, as they did when no probe ran then.
func TestProbeTerminalKeepsKeysWhenEncodingForced(t *testing.T) {
	s := newProbeSession("xterm")
	s.cleanup(t)
	s.reply("x\x1b[?62;4c")
	_, log := newSyncLog()
	ProbeTerminal(s, 10*time.Second, false, log)
	if k, err := readKeyBounded(s); err != nil || k != 'x' {
		t.Fatalf("ReadKey = %q, %v; want 'x'", rune(k), err)
	}
}

// Keys a script or door left unread are handed back without any reply
// that came while it ran.
func TestTypeAheadDropsReplies(t *testing.T) {
	s := newProbeSession("xterm")
	s.cleanup(t)
	_, log := newSyncLog()
	ProbeTerminal(s, 50*time.Millisecond, false, log)
	stashTypeAhead(s, []byte("\x1b[?62;4cx"))
	if k, err := readKeyBounded(s); err != nil || k != 'x' {
		t.Fatalf("ReadKey = %q, %v; want 'x'", rune(k), err)
	}
}

// A terminal that answers nothing costs the timeout and is not sixel
// capable, whatever type it announced.
func TestProbeTerminalSilent(t *testing.T) {
	for _, term := range []string{"syncterm", "xterm"} {
		t.Run(term, func(t *testing.T) {
			s := newProbeSession(term)
			s.cleanup(t)
			logs, log := newSyncLog()
			start := time.Now()
			mode := ProbeTerminal(s, 100*time.Millisecond, true, log)
			if d := time.Since(start); d < 100*time.Millisecond {
				t.Errorf("probe returned after %v, before its timeout", d)
			}
			if mode != ansi.OutputModeCP437 || sessionSixel(s, nil) || sessionAudio(s) {
				t.Errorf("mode = %v, sixel = %v, audio = %v; want CP437, false, false", mode, sessionSixel(s, nil), sessionAudio(s))
			}
			if out := logs.String(); !strings.Contains(out, "cursor=none") || !strings.Contains(out, "extended=none") || !strings.Contains(out, "primary=none") ||
				!strings.Contains(out, "libsndfile=none") || !strings.Contains(out, "wav_u8=none") {
				t.Errorf("log does not record that there was no reply:\n%s", out)
			}
		})
	}
}

// A sixel answer that comes after the connect wait, whole or split, is not
// typed input, turns sixel on for the rest of the call, and Konfig's Auto
// setting shows it.
func TestProbeTerminalLateReply(t *testing.T) {
	for _, tc := range []struct {
		name   string
		chunks []string
	}{
		{"whole", []string{"\x1b[<0;4c\x1b[?62;4cx"}},
		{"split", []string{"\x1b[?6", "2;", "4c", "x"}},
		{"SyncTERM", []string{"\x1b[<0;1;2;3;4;5;6;7c\x1b[=67;84;101;114;109;1;326cx"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newProbeSession("xterm")
			s.cleanup(t)
			logs, log := newSyncLog()
			ProbeTerminal(s, 50*time.Millisecond, false, log)
			if sessionSixel(s, nil) {
				t.Fatal("sixel on before any answer")
			}
			s.reply(tc.chunks...)
			// What the login prompt reads next is the caller's key; the
			// replies before it have been handled by then.
			k, err := readKeyBounded(s)
			if err != nil || k != 'x' {
				t.Fatalf("ReadKey = %q, %v; want 'x'", rune(k), err)
			}
			if !sessionSixel(s, nil) {
				t.Error("late answer did not turn sixel on")
			}
			st := &konfigState{c: &cmdCtx{e: konfigTestExecutor(t), s: s, currentUser: &user.User{}}}
			if got, want := sixelValue(st).text, "Auto (On)"; got != want {
				t.Errorf("Konfig shows Sixel as %q, want %q", got, want)
			}
			if out := logs.String(); !strings.Contains(out, "late terminal reply") {
				t.Errorf("late reply not logged:\n%s", out)
			}
		})
	}
}

// A late answer read by a handler made before the session ended stores
// nothing for the ended session.
func TestProbeTerminalLateReplyAfterSessionEnd(t *testing.T) {
	s := newProbeSession("xterm")
	s.cleanup(t)
	_, log := newSyncLog()
	ProbeTerminal(s, 50*time.Millisecond, false, log)
	getSessionIH(s) // made while the session's trap is still registered
	ClearSessionIdleTimeout(s)
	s.reply("\x1b[=7;100;1n\x1b[=7;101;1;5;1n\x1b[?62;4cx")
	if k, err := readKeyBounded(s); err != nil || k != 'x' {
		t.Fatalf("ReadKey = %q, %v; want 'x'", rune(k), err)
	}
	if _, ok := sessionSixelCaps.Load(s); ok {
		t.Error("late sixel answer stored for an ended session")
	}
	if _, ok := sessionAudioCaps.Load(s); ok {
		t.Error("late audio answer stored for an ended session")
	}
}

// Kept keys that end partway through a reply, with the rest still to come
// from the terminal, give the prompt only the keys.
func TestTypeAheadEndingInPartialReply(t *testing.T) {
	s := newProbeSession("xterm")
	s.cleanup(t)
	_, log := newSyncLog()
	ProbeTerminal(s, 50*time.Millisecond, false, log)
	stashTypeAhead(s, []byte("a\x1b[?6"))
	s.reply("2;4cx")
	for _, want := range []int{'a', 'x'} {
		if k, err := readKeyBounded(s); err != nil || k != want {
			t.Fatalf("ReadKey = %q, %v; want %q", rune(k), err, rune(want))
		}
	}
}

// A reply split across the end of the probe is not typed input either: the
// probe's handler stops with the start of it, and the login prompt's
// handler gets the rest. Finished after the wait, it is a late answer and
// turns sixel on.
func TestProbeTerminalReplySplitAcrossDeadline(t *testing.T) {
	s := newProbeSession("xterm")
	s.cleanup(t)
	_, log := newSyncLog()
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.data <- []byte("\x1b[?6")
	}()
	ProbeTerminal(s, 150*time.Millisecond, false, log)
	s.reply("2;4c", "x")
	if k, err := readKeyBounded(s); err != nil || k != 'x' {
		t.Fatalf("ReadKey = %q, %v; want 'x'", rune(k), err)
	}
	if !sessionSixel(s, nil) {
		t.Error("a reply finished after the deadline did not turn sixel on")
	}
}

// The log names the replies with control bytes escaped.
func TestProbeTerminalLogsReplies(t *testing.T) {
	s := newProbeSession("xterm")
	s.cleanup(t)
	s.reply("\x1b[<0;1;4c\x1b[?62;4c")
	logs, log := newSyncLog()
	ProbeTerminal(s, 10*time.Second, false, log)
	out := logs.String()
	for _, want := range []string{`\x1b[<0;1;4c`, `\x1b[?62;4c`, "sixel=true"} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %s:\n%s", want, out)
		}
	}
	if strings.ContainsRune(out, 0x1b) {
		t.Errorf("log holds a raw ESC:\n%q", out)
	}
}

// The probe leaves no reader on the session for sessionHandler to fight.
func TestProbeTerminalStopsItsReader(t *testing.T) {
	s := newProbeSession("xterm")
	s.cleanup(t)
	s.reply("\x1b[2;2R\x1b[?62;4c")
	_, log := newSyncLog()
	ProbeTerminal(s, 10*time.Second, true, log)
	if _, ok := sessionInputHandlers.Load(s); ok {
		t.Fatal("ProbeTerminal left the session input handler running")
	}
}

// The caller's setting forces sixel on or off over the probe's answer.
func TestSessionSixelOverride(t *testing.T) {
	for _, tc := range []struct {
		probed  bool
		setting string
		want    bool
	}{
		{false, "", false},
		{true, "", true},
		{false, user.SixelOn, true},
		{true, user.SixelOff, false},
		{true, user.SixelOn, true},
		{false, user.SixelOff, false},
	} {
		s := newTestSession("")
		storeSessionSixel(s, tc.probed)
		u := &user.User{PreferredSixel: tc.setting}
		if got := sessionSixel(s, u); got != tc.want {
			t.Errorf("probed %v, setting %q: sessionSixel = %v, want %v", tc.probed, tc.setting, got, tc.want)
		}
		// A door's wrapper reads the transport session's answer.
		if got := sessionSixel(&watchedSession{Session: s}, u); got != tc.want {
			t.Errorf("wrapped, probed %v, setting %q: sessionSixel = %v, want %v", tc.probed, tc.setting, got, tc.want)
		}
		ClearSessionIdleTimeout(s)
	}
}

// Nothing decides sixel from the announced terminal type.
func TestSessionSixelIgnoresTerminalName(t *testing.T) {
	s := &matrixGraphicSession{testSession: newTestSession(""), term: "syncterm"}
	if sessionSixel(s, nil) {
		t.Fatal("a session named syncterm with no probe answer is sixel capable")
	}
}

// audioQueries is what the probe sends to ask about audio, in the
// order measured against real terminals: whether libsndfile is present, then whether WAV with 8-bit
// unsigned PCM (format 1;5) plays, with autowrap off around them so a
// terminal that draws them as text keeps that text on the row the probe
// erases.
const audioQueries = "\x1b[?7l" + "\x1b_SyncTERM:Q;libsndfile\x1b\\" + "\x1b_SyncTERM:Q;libsndfileFormat;1;5\x1b\\" + "\x1b[?7h"

// Audio is on only when the terminal says libsndfile is present and the
// board's format plays. The replies are the ones recorded from
// SyncTERM 1.9 and master, the Win32 and Win64 nightlies and IcyTERM 0.9.2.
func TestProbeTerminalAudio(t *testing.T) {
	const (
		cterm333 = "\x1b[=67;84;101;114;109;1;333c"
		lib1     = "\x1b[=7;100;1n"
		lib0     = "\x1b[=7;100;0n"
		wav1     = "\x1b[=7;101;1;5;1n"
		wav0     = "\x1b[=7;101;1;5;0n"
	)
	tests := []struct {
		name      string
		term      string
		chunks    []string
		wantAudio bool
	}{
		{"SyncTERM with libsndfile", "syncterm", []string{lib1 + wav1 + cterm333}, true},
		{"IcyTERM, not CTerm, whatever its name", "xterm", []string{lib1 + wav1 + "\x1b[=73;99;121;84;101;114;109;0;9;2c"}, true},
		{"libsndfile missing", "syncterm", []string{lib0 + wav0 + cterm333}, false},
		{"format unsupported", "syncterm", []string{lib1 + wav0 + cterm333}, false},
		{"format answered, libsndfile not", "syncterm", []string{wav1 + cterm333}, false},
		{"libsndfile answered, format not", "syncterm", []string{lib1 + cterm333}, false},
		{"SyncTERM 1.9 answers DA only", "syncterm", []string{"\x1b[=67;84;101;114;109;1;326c"}, false},
		{"xterm answers DA only", "xterm", []string{"\x1b[?64;1;2;6;9;15;16;17;18;21;22;28c"}, false},
		{"split replies", "syncterm", []string{"\x1b[=7;1", "00;1n\x1b", "[=7;101;1;5", ";1n\x1b[=67;84;1", "01;114;109;1;333c"}, true},
		{"audio after the primary reply is not counted", "syncterm", []string{cterm333 + lib1 + wav1}, false},
		// Not an answer: no 0 or 1 before n. It does not take the slot of
		// the whole reply that follows.
		{"malformed reply then a whole one", "syncterm", []string{"\x1b[=7;100;n" + lib1 + "\x1b[=7;101;1;5;n" + wav1 + cterm333}, true},
		{"availability other than 0 or 1", "syncterm", []string{"\x1b[=7;100;2n\x1b[=7;101;1;5;11n" + cterm333}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newProbeSession(tt.term)
			s.cleanup(t)
			s.reply(append(tt.chunks, "x")...)
			logs, log := newSyncLog()
			ProbeTerminal(s, 10*time.Second, false, log)
			if got := sessionAudio(s); got != tt.wantAudio {
				t.Errorf("sessionAudio = %v, want %v\n%s", got, tt.wantAudio, logs.String())
			}
			// No reply byte reaches the login prompt.
			if k, err := readKeyBounded(s); err != nil || k != 'x' {
				t.Fatalf("ReadKey = %q, %v; want 'x'", rune(k), err)
			}
			if sessionAudio(&watchedSession{Session: s}) != tt.wantAudio {
				t.Error("a door's wrapper does not read the transport session's answer")
			}
		})
	}
}

// The audio queries go between the two device attributes queries, so the
// primary reply is still the last one and connect waits for nothing more.
func TestProbeTerminalAudioQueryOrder(t *testing.T) {
	s := newProbeSession("xterm")
	s.cleanup(t)
	s.reply("\x1b[?62;1c")
	_, log := newSyncLog()
	ProbeTerminal(s, 10*time.Second, true, log)
	want := ansi.EncodingProbe + ansi.DAExtendedQuery + audioQueries + ansi.DAPrimaryQuery + ansi.EncodingProbeClear
	if got := s.written(); got != want {
		t.Errorf("wrote %q, want %q", got, want)
	}
}

// Audio answers that come after the connect wait, whole or split, are not
// typed input and turn audio on for the rest of the call.
func TestProbeTerminalLateAudioReply(t *testing.T) {
	for _, tc := range []struct {
		name   string
		chunks []string
	}{
		{"whole", []string{"\x1b[=7;100;1n\x1b[=7;101;1;5;1nx"}},
		{"split", []string{"\x1b[=7;10", "0;1n\x1b[=7;", "101;1;5;1n", "x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newProbeSession("syncterm")
			s.cleanup(t)
			logs, log := newSyncLog()
			ProbeTerminal(s, 50*time.Millisecond, false, log)
			s.reply(tc.chunks...)
			if k, err := readKeyBounded(s); err != nil || k != 'x' {
				t.Fatalf("ReadKey = %q, %v; want 'x'", rune(k), err)
			}
			if !sessionAudio(s) {
				t.Error("late answer did not turn audio on")
			}
			if out := logs.String(); !strings.Contains(out, `\x1b[=7;101;1;5;1n`) {
				t.Errorf("late reply not logged:\n%s", out)
			}
		})
	}
}

// The log records the audio replies with control bytes escaped, and the
// answer.
func TestProbeTerminalLogsAudioReplies(t *testing.T) {
	s := newProbeSession("xterm")
	s.cleanup(t)
	s.reply("\x1b[=7;100;1n\x1b[=7;101;1;5;0n\x1b[=67;84;101;114;109;1;333c")
	logs, log := newSyncLog()
	ProbeTerminal(s, 10*time.Second, false, log)
	out := logs.String()
	for _, want := range []string{`libsndfile="\\x1b[=7;100;1n"`, `wav_u8="\\x1b[=7;101;1;5;0n"`, "audio=false"} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %s:\n%s", want, out)
		}
	}
	if strings.ContainsRune(out, 0x1b) {
		t.Errorf("log holds a raw ESC:\n%q", out)
	}
}

// Nothing decides audio from the announced terminal type.
func TestSessionAudioIgnoresTerminalName(t *testing.T) {
	s := &matrixGraphicSession{testSession: newTestSession(""), term: "syncterm"}
	if sessionAudio(s) {
		t.Fatal("a session named syncterm with no probe answer has audio")
	}
}

// The encoding follows how far the probe's glyph moved the cursor on the
// probe's row, and nothing else: silence or a malformed report is CP437,
// whatever the terminal is called.
func TestProbeTerminalEncoding(t *testing.T) {
	const da = "\x1b[?62;1c"
	for _, tc := range []struct {
		name   string
		term   string
		chunks []string
		want   ansi.OutputMode
		cursor string // the cursor reply as the log shows it
	}{
		{"one column", "syncterm", []string{"\x1b[2;2R" + da}, ansi.OutputModeUTF8, `cursor=\x1b[2;2R `},
		{"two columns", "xterm", []string{"\x1b[2;3R" + da}, ansi.OutputModeCP437, `cursor=\x1b[2;3R `},
		{"three columns", "xterm-256color", []string{"\x1b[2;4R" + da}, ansi.OutputModeCP437, `cursor=\x1b[2;4R `},
		{"no movement", "xterm", []string{"\x1b[2;1R" + da}, ansi.OutputModeCP437, `cursor=\x1b[2;1R `},
		{"column out of range", "xterm", []string{"\x1b[2;9R" + da}, ansi.OutputModeCP437, `cursor=\x1b[2;9R `},
		{"another row", "xterm", []string{"\x1b[7;2R" + da}, ansi.OutputModeCP437, `cursor=\x1b[7;2R `},
		{"malformed report", "xterm", []string{"\x1b[2;R" + da}, ansi.OutputModeCP437, `cursor=\x1b[2;R `},
		{"row out of range", "xterm", []string{"\x1b[999999999999999999999999999999;2R" + da}, ansi.OutputModeCP437, `cursor=\x1b[999999999999999999999999999999;2R `},
		{"split report", "xterm", []string{"\x1b[", "2;", "2R", da}, ansi.OutputModeUTF8, `cursor=\x1b[2;2R `},
		{"silent, modern name", "xterm-256color", nil, ansi.OutputModeCP437, "cursor=none "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newProbeSession(tc.term)
			s.cleanup(t)
			s.reply(tc.chunks...)
			logs, log := newSyncLog()
			timeout := 10 * time.Second
			if tc.chunks == nil {
				timeout = 100 * time.Millisecond
			}
			if got := ProbeTerminal(s, timeout, true, log); got != tc.want {
				t.Errorf("mode = %v, want %v", got, tc.want)
			}
			out := logs.String()
			if !strings.Contains(out, tc.cursor) || strings.ContainsRune(out, 0x1b) {
				t.Errorf("log lacks %s, escaped:\n%s", tc.cursor, out)
			}
		})
	}
}

// Shift-F3 (CSI 1 ; 2 R) pressed during the probe is a key: it does not
// stand for the cursor report, and it reaches the prompt.
func TestProbeTerminalShiftF3IsAKey(t *testing.T) {
	s := newProbeSession("xterm")
	s.cleanup(t)
	s.reply("\x1b[1;2R")
	logs, log := newSyncLog()
	if mode := ProbeTerminal(s, 150*time.Millisecond, true, log); mode != ansi.OutputModeCP437 {
		t.Fatalf("mode = %v, want CP437", mode)
	}
	// The key was not taken for the report.
	if out := logs.String(); !strings.Contains(out, "cursor=none ") {
		t.Fatalf("log does not show the report still missing:\n%s", out)
	}
	ih := getSessionIH(s)
	ih.SetSessionIdleTimeout(30 * time.Second)
	// The encoding probe drops keys typed during it, so send the key again
	// after it, while the report is still expected.
	s.reply("\x1b[1;2R")
	b, ok, err := ih.PendingByte(30 * time.Second)
	if err != nil || !ok || b != 0x1b {
		t.Fatalf("next byte = %q, %v, %v; want the key's ESC", b, ok, err)
	}
}

// A cursor report split by more than the Esc wait, while it is expected,
// is still taken; a key that starts the same way is still a key.
func TestProbeTerminalSlowSplitReport(t *testing.T) {
	s := newProbeSession("xterm")
	s.cleanup(t)
	_, log := newSyncLog()
	ProbeTerminal(s, 50*time.Millisecond, true, log)
	go func() {
		s.data <- []byte("\x1b[2;")
		time.Sleep(400 * time.Millisecond)
		s.data <- []byte("2R")
		s.data <- []byte("\x1b[1;5A")
	}()
	if k, err := readKeyBounded(s); err != nil || k != int(editor.KeyArrowUp) {
		t.Fatalf("ReadKey = %d, %v; want Ctrl-Up as Up", k, err)
	}
}

// A cursor report that comes after the login matrix has ended, whole or
// split, is not typed input and does not change the encoding.
func TestProbeTerminalLateCursorReport(t *testing.T) {
	for _, tc := range []struct {
		name   string
		chunks []string
	}{
		{"whole", []string{"\x1b[2;2Rx"}},
		{"split", []string{"\x1b[2", ";2", "R", "x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newProbeSession("xterm")
			s.cleanup(t)
			logs, log := newSyncLog()
			if mode := ProbeTerminal(s, 50*time.Millisecond, true, log); mode != ansi.OutputModeCP437 {
				t.Fatalf("silent probe gave %v, want CP437", mode)
			}
			lateProbe(s).settle() // the login matrix has ended
			s.reply(tc.chunks...)
			if k, err := readKeyBounded(s); err != nil || k != 'x' {
				t.Fatalf("ReadKey = %q, %v; want 'x'", rune(k), err)
			}
			if out := logs.String(); !strings.Contains(out, `\x1b[2;2R`) || !strings.Contains(out, "encoding left as decided") {
				t.Errorf("late report not logged as left alone:\n%s", out)
			}
		})
	}
}

// Once the cursor report is in, a key with the same shape (Shift-F3 is
// CSI 1;2R) is a key again.
func TestProbeTerminalOneCursorReport(t *testing.T) {
	s := newProbeSession("xterm")
	s.cleanup(t)
	s.reply("\x1b[2;2R\x1b[?62;1c") // the DA reply ends the probe
	_, log := newSyncLog()
	if mode := ProbeTerminal(s, 10*time.Second, true, log); mode != ansi.OutputModeUTF8 {
		t.Fatalf("mode = %v, want UTF-8 from the first report", mode)
	}
	// A shape the probe would take while it expected a report: with the
	// report in, every byte reaches the reader.
	const again = "\x1b[2;3R"
	s.reply(again)
	ih := getSessionIH(s)
	ih.SetSessionIdleTimeout(30 * time.Second)
	var got []byte
	for range len(again) {
		if _, ok, err := ih.PendingByte(30 * time.Second); err != nil || !ok {
			t.Fatalf("after %q: no byte, %v", got, err)
		}
		b, err := ih.ReadByteUntil(nil)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, b)
	}
	if string(got) != again {
		t.Fatalf("read %q, want %q", got, again)
	}
}

// A cursor report after the primary reply, later than the old 2 s grace,
// is still taken out of the input while the report is expected.
func TestProbeTerminalReportLongAfterDA(t *testing.T) {
	s := newProbeSession("xterm")
	s.cleanup(t)
	s.reply("\x1b[?62;1c")
	logs, log := newSyncLog()
	if mode := ProbeTerminal(s, 10*time.Second, true, log); mode != ansi.OutputModeCP437 {
		t.Fatalf("mode = %v, want CP437", mode)
	}
	go func() {
		time.Sleep(2500 * time.Millisecond)
		s.data <- []byte("\x1b[2;2Rx")
	}()
	if k, err := readKeyBounded(s); err != nil || k != 'x' {
		t.Fatalf("ReadKey = %q, %v; want 'x'", rune(k), err)
	}
	if out := logs.String(); !strings.Contains(out, `\x1b[2;2R`) {
		t.Errorf("late report not logged:\n%s", out)
	}
}
