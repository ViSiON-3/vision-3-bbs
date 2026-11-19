package editor

import (
	"io"
	"slices"
	"sync"
	"testing"
	"time"
)

// replyLog collects what a ReplyTrap took out of the input.
type replyLog struct {
	mu         sync.Mutex
	replies    []string
	incomplete []string
}

func (l *replyLog) add(p []byte, complete bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if complete {
		l.replies = append(l.replies, string(p))
	} else {
		l.incomplete = append(l.incomplete, string(p))
	}
}

func (l *replyLog) all() (whole, cut []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.replies...), append([]string(nil), l.incomplete...)
}

// testReadWait bounds a read the test expects to succeed; it turns a lost
// byte into a failure rather than a hang.
const testReadWait = 30 * time.Second

// readBounded runs read, closing pw and failing the test if it takes longer
// than testReadWait.
func readBounded[T any](t *testing.T, pw *io.PipeWriter, read func() T) T {
	t.Helper()
	got := make(chan T, 1)
	go func() { got <- read() }()
	select {
	case v := <-got:
		return v
	case <-time.After(testReadWait):
		_ = pw.Close()
		t.Fatal("read did not finish: input was lost")
		panic("unreachable")
	}
}

// readKeys reads n keys from ih, failing the test on an error.
func readKeys(t *testing.T, ih *InputHandler, pw *io.PipeWriter, n int) []int {
	t.Helper()
	type res struct {
		keys []int
		err  error
	}
	r := readBounded(t, pw, func() res {
		var keys []int
		for range n {
			k, err := ih.ReadKey()
			if err != nil {
				return res{keys, err}
			}
			keys = append(keys, k)
		}
		return res{keys, nil}
	})
	if r.err != nil {
		t.Fatalf("ReadKey after %v: %v", r.keys, r.err)
	}
	return r.keys
}

// readBytes reads n raw bytes from ih.
func readBytes(t *testing.T, ih *InputHandler, pw *io.PipeWriter, n int) string {
	t.Helper()
	return readBounded(t, pw, func() string {
		var got []byte
		for range n {
			b, err := ih.readByte()
			if err != nil {
				break
			}
			got = append(got, b)
		}
		return string(got)
	})
}

// send writes chunks to pw with a pause between each, so each is its own
// read.
func send(pw *io.PipeWriter, pause time.Duration, chunks ...string) {
	go func() {
		for i, c := range chunks {
			if i > 0 {
				time.Sleep(pause)
			}
			if _, err := pw.Write([]byte(c)); err != nil {
				return
			}
		}
	}()
}

// Device attributes replies are taken out of the input wherever they fall
// among typed keys, whole or split across reads, and handed to the trap.
func TestReplyTrapTakesRepliesOutOfInput(t *testing.T) {
	tests := []struct {
		name   string
		chunks []string
		keys   []int
		want   []string
	}{
		{"primary reply", []string{"\x1b[?62;4;22c", "x"}, []int{'x'}, []string{"\x1b[?62;4;22c"}},
		{"cterm replies", []string{"\x1b[<0;1;2;3;4c\x1b[=67;84;101;114;109;1;316cx"}, []int{'x'}, []string{"\x1b[<0;1;2;3;4c", "\x1b[=67;84;101;114;109;1;316c"}},
		{"keys around a reply", []string{"a\x1b[?1;2cb"}, []int{'a', 'b'}, []string{"\x1b[?1;2c"}},
		{"split after ESC", []string{"\x1b", "[?62;4c", "y"}, []int{'y'}, []string{"\x1b[?62;4c"}},
		{"split in the parameters", []string{"\x1b[?6", "2;4", "c", "y"}, []int{'y'}, []string{"\x1b[?62;4c"}},
		{"arrow key", []string{"\x1b[A", "z"}, []int{int(KeyArrowUp), 'z'}, nil},
		{"Esc then a reply", []string{"\x1b\x1b[?62;4cz"}, []int{int(KeyEsc), 'z'}, []string{"\x1b[?62;4c"}},
		// SyncTERM's audio feature replies (cterm.html, "APC SyncTERM:Q; Ps ST").
		{"audio replies", []string{"\x1b[=7;100;1n\x1b[=7;101;1;5;1nx"}, []int{'x'}, []string{"\x1b[=7;100;1n", "\x1b[=7;101;1;5;1n"}},
		{"audio reply split", []string{"\x1b[=7;1", "01;1;5", ";0n", "y"}, []int{'y'}, []string{"\x1b[=7;101;1;5;0n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr, pw := io.Pipe()
			defer pw.Close()
			var log replyLog
			ih := NewTrappedInputHandler(pr, NewReplyTrap(time.Minute, log.add))
			defer ih.Close()
			send(pw, 30*time.Millisecond, tt.chunks...)
			if got := readKeys(t, ih, pw, len(tt.keys)); !slices.Equal(got, tt.keys) {
				t.Fatalf("keys = %v, want %v", got, tt.keys)
			}
			if r, _ := log.all(); !slices.Equal(r, tt.want) {
				t.Fatalf("replies = %q, want %q", r, tt.want)
			}
		})
	}
}

// A reply whose tail comes later than the Esc wait is still a reply once
// its marker has arrived: the tail does not become typed input.
func TestReplyTrapHoldsRecognizedReply(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	var log replyLog
	trap := NewReplyTrap(0, log.add) // not expecting: the shortest Esc wait
	ih := NewTrappedInputHandler(pr, trap)
	defer ih.Close()
	send(pw, 300*time.Millisecond, "\x1b[?62;", "4c", "x")
	if got := readKeys(t, ih, pw, 1); got[0] != 'x' {
		t.Fatalf("key = %q, want 'x'", rune(got[0]))
	}
	if r, _ := log.all(); !slices.Equal(r, []string{"\x1b[?62;4c"}) {
		t.Fatalf("replies = %q", r)
	}
}

// After StopExpecting a whole reply is still taken, and a lone Esc is read
// promptly.
func TestReplyTrapAfterExpecting(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	var log replyLog
	trap := NewReplyTrap(time.Minute, log.add)
	trap.StopExpecting()
	ih := NewTrappedInputHandler(pr, trap)
	defer ih.Close()
	send(pw, 0, "\x1b[?62;4cx")
	if got := readKeys(t, ih, pw, 1); got[0] != 'x' {
		t.Fatalf("key = %q, want 'x'", rune(got[0]))
	}
	send(pw, 0, "\x1b")
	start := time.Now()
	if got := readKeys(t, ih, pw, 1); got[0] != int(KeyEsc) {
		t.Fatalf("key = %d, want Esc", got[0])
	}
	if d := time.Since(start); d > DefaultEscTimeout+5*time.Second {
		t.Fatalf("Esc took %v", d)
	}
	if r, _ := log.all(); !slices.Equal(r, []string{"\x1b[?62;4c"}) {
		t.Fatalf("replies = %q", r)
	}
}

// An Esc or ESC [ that no reply follows goes on as typed.
func TestReplyTrapGivesBackKeys(t *testing.T) {
	for _, in := range []string{"\x1b", "\x1b["} {
		pr, pw := io.Pipe()
		var log replyLog
		ih := NewTrappedInputHandler(pr, NewReplyTrap(time.Minute, log.add))
		send(pw, 0, in)
		if got := readBytes(t, ih, pw, len(in)); got != in {
			t.Errorf("got %q, want %q back", got, in)
		}
		ih.Close()
		_ = pw.Close()
	}
}

// A reply cut off before its final byte is dropped, not typed.
func TestReplyTrapDropsUnfinishedReply(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	var log replyLog
	ih := NewTrappedInputHandler(pr, NewReplyTrap(time.Minute, log.add))
	defer ih.Close()
	send(pw, 0, "\x1b[?6x")
	if got := readBytes(t, ih, pw, 1); got != "x" {
		t.Fatalf("got %q, want only the key", got)
	}
	if _, cut := log.all(); !slices.Equal(cut, []string{"\x1b[?6"}) {
		t.Fatalf("incomplete = %q", cut)
	}
}

// A reply split across a handler stopping and the next one starting is
// finished by the next handler, so its tail is not typed input.
func TestReplyTrapCarriesReplyAcrossHandlers(t *testing.T) {
	r := newInterruptibleReader()
	var log replyLog
	trap := NewReplyTrap(time.Minute, log.add)
	first := NewTrappedInputHandler(r, trap)
	r.feed([]byte("\x1b[?6"))
	// The first handler reads the prefix and holds it.
	for deadline := time.Now().Add(testReadWait); !r.drained(); {
		if time.Now().After(deadline) {
			t.Fatal("the first handler did not read the prefix")
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // from the reader to the filter
	unread, _ := first.CloseAndTakeUnread()
	if len(unread) != 0 {
		t.Fatalf("unread = %q, want the prefix kept by the trap", unread)
	}
	pr, pw := io.Pipe()
	defer pw.Close()
	second := NewTrappedInputHandler(pr, trap)
	defer second.Close()
	send(pw, 0, "2;4cx")
	if got := readKeys(t, second, pw, 1); got[0] != 'x' {
		t.Fatalf("key = %q, want 'x'", rune(got[0]))
	}
	if r, _ := log.all(); !slices.Equal(r, []string{"\x1b[?62;4c"}) {
		t.Fatalf("replies = %q", r)
	}
}

// Closing a trapped handler gives back what it read and nobody consumed.
func TestReplyTrapCloseAndTakeUnread(t *testing.T) {
	pr, pw := io.Pipe()
	ih := NewTrappedInputHandler(pr, NewReplyTrap(time.Minute, func([]byte, bool) {}))
	_, _ = pw.Write([]byte("ab\x1b["))
	_ = pw.Close()
	<-ih.done
	unread, _ := ih.CloseAndTakeUnread()
	if string(unread) != "ab\x1b[" {
		t.Fatalf("unread = %q", unread)
	}
}

// Closing does not hang when more input is queued than the handler holds
// and nobody is reading it.
func TestReplyTrapCloseWithFullQueue(t *testing.T) {
	ts := newInterruptibleReader()
	ih := NewTrappedInputHandler(ts, NewReplyTrap(time.Minute, func([]byte, bool) {}))
	big := make([]byte, 1000)
	for i := range big {
		big[i] = 'k'
	}
	ts.feed(big)
	time.Sleep(100 * time.Millisecond)
	done := make(chan []byte, 1)
	go func() {
		unread, _ := ih.CloseAndTakeUnread()
		done <- unread
	}()
	select {
	case unread := <-done:
		if len(unread) == 0 {
			t.Fatal("no unread bytes returned")
		}
	case <-time.After(testReadWait):
		t.Fatal("CloseAndTakeUnread hung on a full queue")
	}
}

// StripReplies removes whole replies and leaves keys, including an Esc.
func TestReplyTrapStripReplies(t *testing.T) {
	var log replyLog
	trap := NewReplyTrap(0, log.add)
	got := trap.StripReplies([]byte("a\x1b[?62;4cb\x1b[Ac\x1b"))
	if string(got) != "ab\x1b[Ac\x1b" {
		t.Fatalf("got %q", got)
	}
	if r, _ := log.all(); !slices.Equal(r, []string{"\x1b[?62;4c"}) {
		t.Fatalf("replies = %q", r)
	}
}

// interruptibleReader is a session-like reader whose reads end when the
// handler closes, as SSH and telnet sessions' do.
type interruptibleReader struct {
	data chan []byte
	mu   sync.Mutex
	buf  []byte
	intr <-chan struct{}
}

func newInterruptibleReader() *interruptibleReader {
	return &interruptibleReader{data: make(chan []byte, 16)}
}

func (r *interruptibleReader) feed(p []byte) { r.data <- p }

// drained reports whether every byte fed has been read.
func (r *interruptibleReader) drained() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.data) == 0 && len(r.buf) == 0
}

func (r *interruptibleReader) SetReadInterrupt(ch <-chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.intr = ch
}

func (r *interruptibleReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	intr := r.intr
	if len(r.buf) == 0 {
		r.mu.Unlock()
		select {
		case d := <-r.data:
			r.mu.Lock()
			r.buf = d
		case <-intr:
			return 0, io.ErrClosedPipe
		}
	}
	defer r.mu.Unlock()
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

// A reply carried to the next handler keeps its deadline: one that ran out
// while no handler was reading is dropped, and the keys after it are typed.
func TestReplyTrapExpiredCarryIsDropped(t *testing.T) {
	var log replyLog
	trap := NewReplyTrap(time.Minute, log.add)
	trap.keepPending([]byte("\x1b[?62;"), time.Now().Add(-time.Second))
	pr, pw := io.Pipe()
	defer pw.Close()
	ih := NewTrappedInputHandler(pr, trap)
	defer ih.Close()
	send(pw, 0, "42\r")
	if got := readKeys(t, ih, pw, 3); !slices.Equal(got, []int{'4', '2', int(KeyEnter)}) {
		t.Fatalf("keys = %v, want 4, 2, Enter", got)
	}
	if _, cut := log.all(); !slices.Equal(cut, []string{"\x1b[?62;"}) {
		t.Fatalf("incomplete = %q", cut)
	}
}

// A reply whose ESC came alone, later than the trap waits, is still not a
// key: ReadKey skips it.
func TestReadKeySkipsReplyAfterLoneEsc(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	trap := NewReplyTrap(0, func([]byte, bool) {})
	ih := NewTrappedInputHandler(pr, trap)
	defer ih.Close()
	send(pw, 100*time.Millisecond, "\x1b", "[?62;4c", "x")
	if got := readKeys(t, ih, pw, 1); got[0] != 'x' {
		t.Fatalf("key = %q, want 'x'", rune(got[0]))
	}
}

// Kept keys that end partway through a reply hand the rest of it to the
// next handler, which finishes it from the session.
func TestReplyTrapStripRepliesCarriesPartial(t *testing.T) {
	var log replyLog
	trap := NewReplyTrap(0, log.add)
	if got := trap.StripReplies([]byte("a\x1b[?6")); string(got) != "a" {
		t.Fatalf("got %q, want the key alone", got)
	}
	pr, pw := io.Pipe()
	defer pw.Close()
	ih := NewTrappedInputHandler(pr, trap)
	defer ih.Close()
	send(pw, 0, "2;4cx")
	if got := readKeys(t, ih, pw, 1); got[0] != 'x' {
		t.Fatalf("key = %q, want 'x'", rune(got[0]))
	}
	if r, _ := log.all(); !slices.Equal(r, []string{"\x1b[?62;4c"}) {
		t.Fatalf("replies = %q", r)
	}
}

// Keys typed as a carried reply runs out are keys, whether the trap's timer
// or the keys reach it first.
func TestReplyTrapCarryExpiringAsKeysArrive(t *testing.T) {
	var log replyLog
	trap := NewReplyTrap(time.Minute, log.add)
	trap.keepPending([]byte("\x1b[?62;"), time.Now().Add(30*time.Millisecond))
	pr, pw := io.Pipe()
	defer pw.Close()
	ih := NewTrappedInputHandler(pr, trap)
	defer ih.Close()
	go func() {
		time.Sleep(30 * time.Millisecond)
		_, _ = pw.Write([]byte("42\r"))
	}()
	if got := readKeys(t, ih, pw, 3); !slices.Equal(got, []int{'4', '2', int(KeyEnter)}) {
		t.Fatalf("keys = %v, want 4, 2, Enter", got)
	}
}

// Kept keys that finish a reply carried from a stopped handler lose it.
func TestReplyTrapStripRepliesFinishesCarried(t *testing.T) {
	var log replyLog
	trap := NewReplyTrap(0, log.add)
	trap.keepPending([]byte("\x1b[?6"), time.Now().Add(time.Minute))
	if got := trap.StripReplies([]byte("2;4cx")); string(got) != "x" {
		t.Fatalf("got %q, want the key alone", got)
	}
	if r, _ := log.all(); !slices.Equal(r, []string{"\x1b[?62;4c"}) {
		t.Fatalf("replies = %q", r)
	}
}

// ReadKey skips any number of replies in a row.
func TestReadKeySkipsManyReplies(t *testing.T) {
	in := make([]byte, 0, 20000)
	for range 2000 {
		in = append(in, "\x1b[?62;4c"...)
	}
	in = append(in, 'x')
	pr, pw := io.Pipe()
	defer pw.Close()
	ih := NewInputHandler(pr)
	defer ih.Close()
	send(pw, 0, string(in))
	if got := readKeys(t, ih, pw, 1); got[0] != 'x' {
		t.Fatalf("key = %q, want 'x'", rune(got[0]))
	}
}

// A carried reply that a kept key cuts short is dropped, and the key is
// kept.
func TestReplyTrapStripRepliesDropsInterruptedCarry(t *testing.T) {
	var log replyLog
	trap := NewReplyTrap(0, log.add)
	trap.keepPending([]byte("\x1b[?6"), time.Now().Add(time.Minute))
	if got := trap.StripReplies([]byte("x")); string(got) != "x" {
		t.Fatalf("got %q, want the key alone", got)
	}
	if _, cut := log.all(); !slices.Equal(cut, []string{"\x1b[?6"}) {
		t.Fatalf("incomplete = %q", cut)
	}
}

// A carried reply still unfinished after the kept keys keeps the deadline
// it was carried with.
func TestReplyTrapStripRepliesKeepsCarriedDeadline(t *testing.T) {
	trap := NewReplyTrap(0, func([]byte, bool) {})
	expires := time.Now().Add(time.Minute)
	trap.keepPending([]byte("\x1b[?6"), expires)
	if got := trap.StripReplies([]byte("2;")); len(got) != 0 {
		t.Fatalf("got %q, want nothing", got)
	}
	p, until := trap.takePending()
	if string(p) != "\x1b[?62;" || !until.Equal(expires) {
		t.Fatalf("pending %q until %v, want %q until %v", p, until, "\x1b[?62;", expires)
	}
}

// A reply whose tail comes after the trap gave up on it does not swallow
// keys: what follows is typed. The bound is tailGap.
func TestReplyTrapGivesUpAfterTailGap(t *testing.T) {
	var log replyLog
	trap := NewReplyTrap(0, log.add)
	trap.keepPending([]byte("\x1b[?62;"), time.Now().Add(20*time.Millisecond))
	time.Sleep(40 * time.Millisecond)
	if got := trap.StripReplies([]byte("42")); string(got) != "42" {
		t.Fatalf("got %q, want the digits typed", got)
	}
}

// While a cursor position report is expected the trap takes the first one,
// whole or split, and then lets the same shape through as a key; keys that
// only start like one are untouched.
func TestReplyTrapCursorReport(t *testing.T) {
	for _, tc := range []struct {
		name   string
		chunks []string
		keys   []int
		want   []string
	}{
		{"report then Shift-F3", []string{"\x1b[5;2R", "\x1b[1;2Rx"}, nil, []string{"\x1b[5;2R"}},
		{"split report", []string{"\x1b[5", ";2", "R", "x"}, []int{'x'}, []string{"\x1b[5;2R"}},
		{"Ctrl-Up is a key", []string{"\x1b[1;5A", "x"}, []int{int(KeyArrowUp), 'x'}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pr, pw := io.Pipe()
			defer pw.Close()
			var log replyLog
			trap := NewReplyTrap(time.Minute, log.add)
			trap.ExpectCursorReport(time.Minute, nil)
			ih := NewTrappedInputHandler(pr, trap)
			defer ih.Close()
			send(pw, 30*time.Millisecond, tc.chunks...)
			if tc.keys == nil {
				// The second report-shaped sequence is a key: its bytes
				// reach the reader.
				if got := readBytes(t, ih, pw, 7); got != "\x1b[1;2Rx" {
					t.Fatalf("got %q, want Shift-F3 and x", got)
				}
			} else if got := readKeys(t, ih, pw, len(tc.keys)); !slices.Equal(got, tc.keys) {
				t.Fatalf("keys = %v, want %v", got, tc.keys)
			}
			if r, _ := log.all(); !slices.Equal(r, tc.want) {
				t.Fatalf("replies = %q, want %q", r, tc.want)
			}
		})
	}
}

// With no report expected, the shape is a key.
func TestReplyTrapCursorReportNotExpected(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	var log replyLog
	ih := NewTrappedInputHandler(pr, NewReplyTrap(time.Minute, log.add))
	defer ih.Close()
	send(pw, 0, "\x1b[5;2R")
	if got := readBytes(t, ih, pw, 6); got != "\x1b[5;2R" {
		t.Fatalf("got %q", got)
	}
}

// A report-shaped sequence the trap's accept turns down goes on as typed,
// and the report that follows is still taken.
func TestReplyTrapCursorReportAccept(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	var log replyLog
	trap := NewReplyTrap(time.Minute, log.add)
	trap.ExpectCursorReport(time.Minute, func(r []byte) bool { return string(r) != "\x1b[1;2R" })
	ih := NewTrappedInputHandler(pr, trap)
	defer ih.Close()
	send(pw, 30*time.Millisecond, "\x1b[1;2R", "\x1b[2;2Rx")
	if got := readBytes(t, ih, pw, 7); got != "\x1b[1;2Rx" {
		t.Fatalf("got %q, want the key and x", got)
	}
	if r, _ := log.all(); !slices.Equal(r, []string{"\x1b[2;2R"}) {
		t.Fatalf("replies = %q", r)
	}
}

// While a report is expected, ESC [ and digits wait past the Esc wait for
// the final byte.
func TestReplyTrapSlowSplitCursorReport(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	var log replyLog
	trap := NewReplyTrap(0, log.add)
	trap.ExpectCursorReport(time.Minute, nil)
	ih := NewTrappedInputHandler(pr, trap)
	defer ih.Close()
	send(pw, 400*time.Millisecond, "\x1b[2;", "2Rx")
	if got := readKeys(t, ih, pw, 1); got[0] != 'x' {
		t.Fatalf("key = %q, want 'x'", rune(got[0]))
	}
	if r, _ := log.all(); !slices.Equal(r, []string{"\x1b[2;2R"}) {
		t.Fatalf("replies = %q", r)
	}
}

// An audio reply handed back with a script's keys is taken out too, and a
// key parser skips one the trap let through.
func TestReplyTrapAudioReplies(t *testing.T) {
	var log replyLog
	trap := NewReplyTrap(0, log.add)
	if got := trap.StripReplies([]byte("a\x1b[=7;100;0nb")); string(got) != "ab" {
		t.Fatalf("got %q", got)
	}
	if r, _ := log.all(); !slices.Equal(r, []string{"\x1b[=7;100;0n"}) {
		t.Fatalf("replies = %q", r)
	}
	if !IsReplyCSI([]byte("=7;101;1;5;1n")) {
		t.Error("IsReplyCSI does not know the audio reply")
	}
	// The cursor report and DSR keep their own shapes: no marker.
	if IsReplyCSI([]byte("0n")) {
		t.Error("IsReplyCSI takes a DSR reply without a marker")
	}
}
