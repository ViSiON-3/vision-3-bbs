package editor

import (
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/safego"
)

// maxReplyLength bounds a device attributes reply the trap will hold. CTerm's
// longest documented reply, CSI < 0 ; 1 ; ... ; 8 c, is under 30 bytes.
const maxReplyLength = 128

// How long the trap holds bytes waiting for the next one.
const (
	// expectGap is the wait after an ESC while a reply is expected: long
	// enough for a reply split across reads.
	expectGap = 250 * time.Millisecond
	// idleGap is the wait after an ESC at other times. A reply sent in one
	// write is read in one burst, so this still takes it, and a lone Esc is
	// barely delayed.
	idleGap = 20 * time.Millisecond
	// tailGap is the wait for the rest of a reply once ESC [ and a reply
	// marker have arrived. No key sends that, so it can be long.
	tailGap = 2 * time.Second
)

// ReplyTrap takes the terminal's device attributes replies out of a
// session's input: CSI followed by one of ? = < >, digits and semicolons,
// and the final byte c; and SyncTERM's state reports, the same with the
// marker = and the final byte n, which answer the audio queries. No key
// sends CSI with one of those markers (the board enables no mouse
// reporting), so a reply that arrives after its query was given up on is
// never read as typed input.
//
// An ESC is held until the next byte shows whether a reply follows, for up
// to expectGap while replies are expected (see NewReplyTrap and
// StopExpecting) and idleGap otherwise; if none follows it goes on as typed.
// Once the marker has arrived the bytes are a reply: they are held up to
// tailGap for the rest, and dropped if it does not come. A reply cut off
// when a handler stops is carried to the session's next trapped handler.
//
// While a cursor position report is expected (ExpectCursorReport), the
// first one is taken as well.
//
// One trap serves a session for its life, across its input handlers.
type ReplyTrap struct {
	expectUntil atomic.Int64 // UnixNano; replies are expected until then
	cprUntil    atomic.Int64 // UnixNano; a cursor position report is expected until then
	cprAccept   atomic.Pointer[func([]byte) bool]
	onReply     func(reply []byte, complete bool)

	mu             sync.Mutex
	pending        []byte    // a reply begun under a handler that stopped
	pendingExpires time.Time // when pending is given up on
}

// NewReplyTrap returns a trap that expects replies for expect. onReply
// receives each reply taken, with complete false for one dropped before
// its final byte. It runs on the input goroutine and must not block.
func NewReplyTrap(expect time.Duration, onReply func(reply []byte, complete bool)) *ReplyTrap {
	t := &ReplyTrap{onReply: onReply}
	t.expectUntil.Store(time.Now().Add(expect).UnixNano())
	return t
}

// StopExpecting says no more replies are due; ESC is then held only for
// idleGap.
func (t *ReplyTrap) StopExpecting() { t.expectUntil.Store(0) }

// ExpectCursorReport says one cursor position report (CSI row ; col R) is
// due within d. Until it arrives, d passes or StopExpectingCursorReport is
// called, the trap takes the first sequence of that shape that accept
// approves. Some keys share the shape (xterm sends Shift-F3 as CSI 1 ; 2 R):
// one accept turns down goes on as typed. A nil accept approves any.
//
// While a report is expected, ESC [ and digits wait up to tailGap for the
// final byte that settles whether they are the report or a key, and go on
// as typed if they are a key or nothing comes.
func (t *ReplyTrap) ExpectCursorReport(d time.Duration, accept func(report []byte) bool) {
	t.cprAccept.Store(&accept)
	t.cprUntil.Store(time.Now().Add(d).UnixNano())
}

func (t *ReplyTrap) acceptsReport(report []byte) bool {
	if f := t.cprAccept.Load(); f != nil && *f != nil {
		return (*f)(report)
	}
	return true
}

// StopExpectingCursorReport says no cursor position report is due.
func (t *ReplyTrap) StopExpectingCursorReport() { t.cprUntil.Store(0) }

func (t *ReplyTrap) cprExpected() bool { return time.Now().UnixNano() < t.cprUntil.Load() }

func (t *ReplyTrap) escGap() time.Duration {
	if time.Now().UnixNano() < t.expectUntil.Load() || t.cprExpected() {
		return expectGap
	}
	return idleGap
}

// keepPending keeps a reply cut off at expires for the next handler.
func (t *ReplyTrap) keepPending(p []byte, expires time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending = append([]byte(nil), p...)
	t.pendingExpires = expires
}

// takePending returns a reply kept by keepPending and when it is given up
// on. One already past that is dropped, as it would have been had its
// handler kept running.
func (t *ReplyTrap) takePending() ([]byte, time.Time) {
	t.mu.Lock()
	p, expires := t.pending, t.pendingExpires
	t.pending = nil
	t.mu.Unlock()
	if len(p) == 0 {
		return nil, time.Time{}
	}
	if !time.Now().Before(expires) {
		t.onReply(p, false)
		return nil, time.Time{}
	}
	return p, expires
}

// StripReplies returns p without the whole replies in it, each passed to
// the trap. It is for input read outside the trap and handed back, such as
// a script's unread keys, which the session reads before anything new. A
// reply p ends partway through is kept for the next trapped handler to
// finish from the session, as a cut-off reply is.
//
// A reply already carried from a stopped handler came before p, so it is
// read ahead of p and keeps its deadline.
func (t *ReplyTrap) StripReplies(p []byte) []byte {
	carried, carriedExpires := t.takePending()
	if len(carried) > 0 {
		p = append(append([]byte(nil), carried...), p...)
	}
	out := make([]byte, 0, len(p))
	for i := 0; i < len(p); {
		if p[i] == 0x1b {
			n, m := replyAt(p[i:], false)
			if m == replyWhole {
				t.onReply(append([]byte(nil), p[i:i+n]...), true)
				i += n
				continue
			}
			if m == replyPartial && isReply(p[i:]) {
				expires := time.Now().Add(tailGap)
				if i == 0 && len(carried) > 0 {
					expires = carriedExpires
				}
				t.keepPending(p[i:], expires)
				break
			}
			if m == replyNo && isReply(p[i:i+n-1]) {
				// A reply cut short by a key: drop it, as the live
				// filter does, and look at the key afresh.
				t.onReply(append([]byte(nil), p[i:i+n-1]...), false)
				i += n - 1
				continue
			}
		}
		out = append(out, p[i])
		i++
	}
	return out
}

// replyAt reports how far the start of p matches a reply: the length of a
// whole one, or that p ends partway through one, or neither.
func replyAt(p []byte, cpr bool) (int, replyMatch) {
	for n := 1; n <= len(p) && n <= maxReplyLength; n++ {
		switch m := matchReply(p[:n], cpr); m {
		case replyWhole, replyNo:
			return n, m
		}
	}
	return len(p), replyPartial
}

// IsReplyCSI reports whether a CSI sequence, given as the bytes after
// ESC [ through its final byte, has the shape of a reply the trap takes. A
// reader that parses keys skips one the trap let through, such as a reply
// whose ESC came alone, longer before the rest than the trap waits.
func IsReplyCSI(seq []byte) bool {
	return matchReply(append([]byte{0x1b, '['}, seq...), false) == replyWhole
}

// replyMatch says how far bytes held from an ESC match a reply.
type replyMatch int

const (
	replyNo replyMatch = iota
	replyPartial
	replyWhole
)

// isReplyMarker reports whether b, after ESC [, marks a device attributes
// reply.
func isReplyMarker(b byte) bool { return b == '?' || b == '=' || b == '<' || b == '>' }

// matchReply matches held against a device attributes reply, CSI marker
// params c; a SyncTERM state report, CSI = params n; or, when cpr is true,
// a cursor position report, CSI params R with at least one parameter byte.
func matchReply(held []byte, cpr bool) replyMatch {
	final := byte('c')
	for i, b := range held {
		switch {
		case i == 0:
			if b != 0x1b {
				return replyNo
			}
		case i == 1:
			if b != '[' {
				return replyNo
			}
		case i == 2 && isReplyMarker(b):
		case i == 2 && cpr && (b >= '0' && b <= '9' || b == ';'):
			final = 'R'
		case i == 2:
			return replyNo
		case i == len(held)-1 && (b == final || final == 'c' && b == 'n' && held[2] == '='):
			return replyWhole
		case (b < '0' || b > '9') && b != ';':
			return replyNo
		}
	}
	if len(held) >= maxReplyLength {
		return replyNo
	}
	return replyPartial
}

// isReply reports whether held has its reply marker, so it can only be a
// reply.
//
// A partial cursor position report is not one: keys such as ESC [ 1 ; 5 A
// start the same way.
func isReply(held []byte) bool { return len(held) >= 3 && isReplyMarker(held[2]) }

// NewTrappedInputHandler is NewInputHandler with trap taking device
// attributes replies out of the input before any read sees them. A nil
// trap is NewInputHandler.
func NewTrappedInputHandler(input io.Reader, trap *ReplyTrap) *InputHandler {
	if trap == nil {
		return NewInputHandler(input)
	}
	ih := newInputHandler(input)
	raw := make(chan byte, 256)
	safego.Go(safego.KindInput, input, func() {
		defer close(raw)
		if ih.setReadInterrupt != nil {
			defer ih.setReadInterrupt(nil)
		}
		safego.TestPoint(safego.KindInput)
		buf := [1]byte{}
		for {
			n, err := input.Read(buf[:])
			if n > 0 {
				raw <- buf[0]
			}
			if err != nil {
				return
			}
		}
	}, nil)
	safego.Go(safego.KindInput, input, func() { ih.filterReplies(raw, trap) }, nil)
	return ih
}

// filterReplies passes bytes from raw to incoming, less the replies trap
// takes. It closes incoming and then done once raw is closed, which the
// reading goroutine does after clearing the read interrupt.
func (ih *InputHandler) filterReplies(raw <-chan byte, trap *ReplyTrap) {
	// A filter that panics (safego.Go) has incoming closed first (below), so
	// reads end at once. done then waits, on a goroutine of its own so the
	// panic is logged now, for the reader to stop and clear its read
	// interrupt: CloseAndWait returns once the reader is gone, as it does
	// when input ends.
	defer func() {
		if r := recover(); r != nil {
			safego.Go(safego.KindInput, nil, func() {
				for range raw {
				}
				close(ih.done)
			}, nil)
			panic(r) // for safego.Go to log
		}
		close(ih.done)
	}()
	defer close(ih.incoming)
	safego.TestPoint(safego.KindInput)
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	held, heldUntil := trap.takePending() // heldUntil: when a held reply is given up on
	if len(held) > 0 {
		timer.Reset(time.Until(heldUntil))
	}
	// settle gives up on held: a reply is dropped, anything else goes on
	// as typed.
	settle := func() {
		if isReply(held) {
			trap.onReply(append([]byte(nil), held...), false)
		} else {
			for _, b := range held {
				ih.incoming <- b
			}
		}
		held = held[:0]
		timer.Stop()
	}
	var take func(b byte)
	take = func(b byte) {
		if len(held) == 0 {
			if b != 0x1b {
				ih.incoming <- b
				return
			}
			held = append(held, b)
			timer.Reset(trap.escGap())
			return
		}
		// A reply past its deadline is given up on before b is looked at,
		// whichever of the timer and b the loop saw first.
		if isReply(held) && !heldUntil.IsZero() && !time.Now().Before(heldUntil) {
			settle()
			take(b)
			return
		}
		held = append(held, b)
		switch matchReply(held, trap.cprExpected()) {
		case replyWhole:
			if held[len(held)-1] == 'R' {
				if !trap.acceptsReport(held) {
					settle() // a key: it goes on as typed
					return
				}
				trap.StopExpectingCursorReport() // only one is due
			}
			trap.onReply(append([]byte(nil), held...), true)
			held = held[:0]
			timer.Stop()
		case replyPartial:
			if isReply(held) {
				heldUntil = time.Now().Add(tailGap)
				timer.Reset(tailGap)
			} else if len(held) >= 3 {
				// ESC [ and digits while a report is expected: the final
				// byte decides.
				timer.Reset(tailGap)
			} else {
				timer.Reset(trap.escGap())
			}
		default:
			// The last byte is not part of what was held, and may start
			// a reply itself, so it is looked at afresh.
			held = held[:len(held)-1]
			settle()
			take(b)
		}
	}
	for {
		select {
		case b, ok := <-raw:
			if !ok {
				if isReply(held) {
					trap.keepPending(held, heldUntil)
					held = held[:0]
				}
				settle()
				return
			}
			take(b)
		case <-timer.C:
			settle()
		}
	}
}
