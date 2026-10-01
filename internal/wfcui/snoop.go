package wfcui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/muesli/cancelreader"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
)

const escWindow = 300 * time.Millisecond

const (
	snoopWatch = "watch"
	snoopType  = "type"
	snoopChat  = "chat"
)

// snoopControl drives the server-side state behind the snoop screen.
type snoopControl interface {
	TypeIn(on bool) error
	Chat(start bool) error
}

// rawTerm is the sysop's terminal.
type rawTerm interface {
	MakeRaw() (restore func(), err error)
	Size() (w, h int, err error)
}

type stdRaw struct{}

func (stdRaw) MakeRaw() (func(), error) {
	fd := int(os.Stdin.Fd())
	st, err := term.MakeRaw(fd)
	if err != nil {
		return func() {}, err
	}
	return func() { _ = term.Restore(fd, st) }, nil
}

func (stdRaw) Size() (int, int, error) { return term.GetSize(int(os.Stdout.Fd())) }

// snoopResult is the tea.Msg returned when the snoop screen ends.
type snoopResult struct {
	node   int
	reason string
}

// snoopCmd is a tea.ExecCommand that shows a caller's screen on the sysop's
// terminal.
type snoopCmd struct {
	st   *admin.SnoopStream
	ctl  snoopControl
	node int
	raw  rawTerm

	stdin  io.Reader
	stdout io.Writer

	// openIn wraps stdin in a reader whose pending read can be canceled.
	openIn func(io.Reader) (cancelreader.CancelReader, error)

	// resize delivers terminal size changes; nil means watch SIGWINCH.
	resize chan struct{}

	mu           sync.Mutex // guards stdout, mode, statusOn, lastErr, w, h, bar state
	mode         string
	statusOn     bool
	statusManual bool // the sysop toggled the bar, so size no longer decides
	lastErr      string
	w, h         int // sysop terminal size

	startChat bool // begin in chat, as when answering a page

	typeBeforeChat bool // type-in was on when chat started
	barRow         int  // row the bar was last drawn on, 0 if none
	regionH        int  // bottom of the scroll region set on the sysop terminal, 0 if none
	eraseRow       int  // row to clear at the next safe point, 0 if none
	track          seqTracker

	result snoopResult
}

func newSnoopCmd(st *admin.SnoopStream, ctl snoopControl, node int, raw rawTerm) *snoopCmd {
	return &snoopCmd{st: st, ctl: ctl, node: node, raw: raw, mode: snoopWatch,
		stdin: os.Stdin, stdout: os.Stdout, openIn: cancelreader.NewReader}
}

// plainReader is the fallback when no cancelable reader can be built; its
// reads cannot be interrupted.
type plainReader struct{ io.Reader }

func (plainReader) Cancel() bool { return false }
func (plainReader) Close() error { return nil }

func (c *snoopCmd) SetStdin(r io.Reader)  { c.stdin = r }
func (c *snoopCmd) SetStdout(w io.Writer) { c.stdout = w }
func (c *snoopCmd) SetStderr(io.Writer)   {}

// Result reports why Run returned.
func (c *snoopCmd) Result() snoopResult { return c.result }

func (c *snoopCmd) write(p string) {
	c.mu.Lock()
	_, _ = io.WriteString(c.stdout, p)
	c.mu.Unlock()
}

// Run shows the caller's screen until Alt-X, the caller leaving or sysop
// input ending.
func (c *snoopCmd) Run() error {
	c.result = snoopResult{node: c.node}
	restore, err := c.raw.MakeRaw()
	if err != nil {
		return err
	}
	defer restore()

	c.w, c.h, err = c.raw.Size()
	if err != nil || c.w <= 0 || c.h <= 0 {
		c.w, c.h = c.st.Header.Width, c.st.Header.Height
		c.statusOn = false
	} else {
		c.statusOn = c.h > c.st.Header.Height
	}
	if c.startChat {
		if err := c.ctl.Chat(true); err != nil {
			c.lastErr = err.Error()
		} else {
			c.mode = snoopChat
		}
	}
	c.write("\x1b[0m\x1b[2J\x1b[H")
	c.mu.Lock()
	c.track.reset(c.st.Header.Width, c.st.Header.Height)
	c.mu.Unlock()
	c.drawStatus()

	outDone := make(chan struct{})
	go c.pump(outDone)

	cr, err := c.openIn(c.stdin)
	if err != nil {
		cr = plainReader{c.stdin}
	}
	var resize <-chan struct{} = c.resize
	if c.resize == nil {
		ch, stop := watchResize()
		defer stop()
		resize = ch
	}

	done := make(chan struct{})
	in := make(chan byte)
	inEnd := make(chan struct{})
	readerGone := make(chan struct{})
	go func() {
		defer close(readerGone)
		c.readStdin(cr, in, inEnd, done)
	}()

	resizeGone := make(chan struct{})
	go func() {
		defer close(resizeGone)
		c.followResize(resize, done)
	}()

	reason := c.loop(in, inEnd, outDone)

	// Cancel the pending stdin read so it takes no more input. When the
	// reader cannot be canceled the goroutine stays parked until the next
	// byte arrives and drops it.
	close(done)
	if cr.Cancel() {
		<-readerGone
	}
	_ = cr.Close()
	<-resizeGone

	c.mu.Lock()
	mode := c.mode
	c.mu.Unlock()
	c.mu.Lock()
	typeHeld := mode == snoopType || (mode == snoopChat && c.typeBeforeChat)
	c.mu.Unlock()
	if mode == snoopChat {
		_ = c.ctl.Chat(false)
	}
	if typeHeld {
		_ = c.ctl.TypeIn(false)
	}
	_ = c.st.Close()
	<-outDone

	c.result.reason = reason
	c.mu.Lock()
	_, _ = io.WriteString(c.stdout, "\x1b[r\x1b[0m\x1b[2J")
	c.mu.Unlock()
	return nil
}

// followResize re-reads the terminal size on each resize event until done
// closes.
func (c *snoopCmd) followResize(resize <-chan struct{}, done <-chan struct{}) {
	for {
		select {
		case <-resize:
			c.onResize()
		case <-done:
			return
		}
	}
}

// onResize adopts the new terminal size. The bar follows the default
// visibility rule unless the sysop toggled it, moves to the new last row,
// and the row it left is cleared.
func (c *snoopCmd) onResize() {
	w, h, err := c.raw.Size()
	if err != nil || w <= 0 || h <= 0 {
		return
	}
	c.mu.Lock()
	if w == c.w && h == c.h {
		c.mu.Unlock()
		return
	}
	c.w, c.h = w, h
	if !c.statusManual {
		c.statusOn = h > c.st.Header.Height
	}
	if c.barRow != 0 && (!c.statusOn || c.barRow != h) {
		if c.barRow <= h {
			c.eraseRow = c.barRow
		}
		c.barRow = 0
	}
	c.mu.Unlock()
	c.drawStatus()
}

// readStdin sends stdin bytes to in until done closes or stdin ends.
func (c *snoopCmd) readStdin(r io.Reader, in chan<- byte, end chan<- struct{}, done <-chan struct{}) {
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			select {
			case in <- buf[0]:
			case <-done:
				return
			}
		}
		if err != nil {
			select {
			case <-done:
			default:
				close(end)
			}
			return
		}
		select {
		case <-done:
			return
		default:
		}
	}
}

// pump copies the caller's output to the sysop terminal.
func (c *snoopCmd) pump(done chan<- struct{}) {
	defer close(done)
	cp437 := c.st.Header.OutputMode == "cp437"
	buf := make([]byte, 4096)
	for {
		n, err := c.st.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if cp437 {
				// ESC sequences pass through byte for byte and the rest of
				// a sequence split across reads is ASCII, so each chunk
				// converts on its own.
				chunk = ansi.CP437BytesToUTF8(chunk)
			}
			c.mu.Lock()
			_, _ = c.stdout.Write(chunk)
			c.track.feed(chunk)
			c.mu.Unlock()
			c.drawStatus()
		}
		if err != nil {
			return
		}
	}
}

// loop runs the hotkey parser and returns the reason Run ends.
func (c *snoopCmd) loop(in <-chan byte, inEnd, outDone <-chan struct{}) string {
	gone := fmt.Sprintf("node %d disconnected", c.node)
	for {
		var b byte
		select {
		case b = <-in:
		case <-inEnd:
			return "input closed"
		case <-outDone:
			return gone
		}
		if b != 0x1b {
			c.forward(string([]byte{b}))
			continue
		}
		timer := time.NewTimer(escWindow)
		select {
		case b = <-in:
			timer.Stop()
		case <-timer.C:
			c.forward("\x1b")
			continue
		case <-inEnd:
			timer.Stop()
			return "input closed"
		case <-outDone:
			timer.Stop()
			return gone
		}
		switch b {
		case 't', 'T':
			c.toggleType()
		case 'c', 'C':
			c.toggleChat()
		case 'h', 'H':
			c.toggleStatus()
		case 'x', 'X':
			return ""
		case 0x1b:
			c.forward("\x1b")
		default:
			c.forward("\x1b" + string([]byte{b}))
		}
	}
}

// forward sends keys to the caller in type and chat modes.
func (c *snoopCmd) forward(s string) {
	c.mu.Lock()
	m := c.mode
	c.mu.Unlock()
	if m == snoopWatch {
		return
	}
	if _, err := io.WriteString(c.st, s); err != nil {
		c.setErr(err)
	}
}

func (c *snoopCmd) toggleType() {
	c.mu.Lock()
	m := c.mode
	c.mu.Unlock()
	switch m {
	case snoopWatch:
		if err := c.ctl.TypeIn(true); err != nil {
			c.setErr(err)
			return
		}
		c.setMode(snoopType)
	case snoopType:
		if err := c.ctl.TypeIn(false); err != nil {
			c.setErr(err)
		}
		c.setMode(snoopWatch)
	}
}

func (c *snoopCmd) toggleChat() {
	c.mu.Lock()
	m := c.mode
	c.mu.Unlock()
	if m == snoopChat {
		if err := c.ctl.Chat(false); err != nil {
			if lostKeyboard(err) {
				// The caller ended chat, which released the keyboard.
				c.mu.Lock()
				c.typeBeforeChat = false
				c.mode = snoopWatch
				c.lastErr = "chat ended by caller"
				c.mu.Unlock()
				c.drawStatus()
				return
			}
			c.setErr(err)
		}
		c.mu.Lock()
		back := snoopWatch
		if c.typeBeforeChat {
			back = snoopType
		}
		c.mu.Unlock()
		c.setMode(back)
		return
	}
	if err := c.ctl.Chat(true); err != nil {
		c.setErr(err)
		return
	}
	c.mu.Lock()
	c.typeBeforeChat = m == snoopType
	c.mu.Unlock()
	c.setMode(snoopChat)
}

// lostKeyboard reports whether err says the sysop no longer holds the
// keyboard. Over RPC the snoop sentinels arrive as plain text.
func lostKeyboard(err error) bool {
	for _, e := range []error{snoop.ErrNotHolder, snoop.ErrNotWatching} {
		if errors.Is(err, e) || strings.Contains(err.Error(), e.Error()) {
			return true
		}
	}
	return false
}

func (c *snoopCmd) toggleStatus() {
	c.mu.Lock()
	c.statusOn = !c.statusOn
	c.statusManual = true
	if !c.statusOn && c.barRow != 0 {
		c.eraseRow = c.barRow
		c.barRow = 0
	}
	c.mu.Unlock()
	c.drawStatus()
}

func (c *snoopCmd) setMode(m string) {
	c.mu.Lock()
	c.mode = m
	c.lastErr = ""
	c.mu.Unlock()
	c.drawStatus()
}

func (c *snoopCmd) setErr(err error) {
	c.mu.Lock()
	c.lastErr = err.Error()
	c.mu.Unlock()
	c.drawStatus()
}

// wantRegion reports whether the sysop terminal needs a scroll region that
// stops above the bar row. Without one, a line feed on the caller's last row
// moves the cursor below it instead of scrolling the mirrored screen.
func (c *snoopCmd) wantRegion() bool { return c.h > c.st.Header.Height }

// drawStatus paints the bar on the sysop terminal's last row, then puts the
// cursor and text attributes back where the caller's output left them, using
// the tracked position instead of the terminal's save slot, which belongs to
// the caller. It waits for a point where the caller's output is between
// sequences and characters and the position is known; the pump calls it again
// after every chunk.
func (c *snoopCmd) drawStatus() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.h <= 0 || c.w <= 0 {
		return
	}
	callerH := c.st.Header.Height
	want := c.wantRegion()
	regionOff := !want && c.regionH != 0
	regionSet := want && (c.regionH != callerH || c.track.cur.regionDirty)
	if c.eraseRow == 0 && !c.statusOn && !regionOff && !regionSet {
		return
	}
	if !c.track.safe() || !c.track.cur.canRestore() {
		return
	}
	back := c.track.cur.restore()
	switch {
	case regionOff:
		// DECSTBM homes the cursor, so the tracked position goes back after it.
		_, _ = fmt.Fprintf(c.stdout, "\x1b[r%s", back)
		c.regionH = 0
	case regionSet:
		_, _ = fmt.Fprintf(c.stdout, "\x1b[1;%dr%s", callerH, back)
		c.regionH = callerH
		c.track.cur.top, c.track.cur.bot = 1, callerH
		c.track.cur.regionDirty = false
	}
	if c.eraseRow != 0 {
		_, _ = fmt.Fprintf(c.stdout, "\x1b[%d;1H\x1b[0m\x1b[2K%s", c.eraseRow, back)
		c.eraseRow = 0
	}
	if !c.statusOn {
		return
	}
	hd := c.st.Header
	text := fmt.Sprintf(" NODE %d · %s · %dx%d · %s", c.node, hd.Handle, hd.Width, hd.Height, strings.ToUpper(c.mode))
	if c.lastErr != "" {
		text += " · " + c.lastErr
	}
	text += " · Alt-T type · Alt-C chat · Alt-X exit "
	r := []rune(text)
	if len(r) > c.w {
		r = r[:c.w]
	}
	text = string(r) + strings.Repeat(" ", c.w-len(r))
	_, _ = fmt.Fprintf(c.stdout, "\x1b[%d;1H\x1b[0;30;47m%s\x1b[0m%s", c.h, text, back)
	c.barRow = c.h
}

// seqTracker follows the caller's output to tell when it is between escape
// sequences and complete UTF-8 characters, and where the caller's cursor is.
type seqTracker struct {
	state  int
	osc    int    // bytes seen in the current OSC
	tail   []byte // trailing bytes of the last chunk, for rune completeness
	params []byte // parameter bytes of the CSI being read
	interm bool   // the CSI has an intermediate byte
	cur    cursor
}

const (
	stGround = iota
	stEsc
	stCharset // after ESC ( or ESC )
	stCSI
	stOSC
	stOSCEsc
)

func (t *seqTracker) feed(p []byte) {
	for _, b := range p {
		t.step(b)
	}
	t.tail = append(t.tail, p...)
	if len(t.tail) > utf8.UTFMax {
		t.tail = append(t.tail[:0], t.tail[len(t.tail)-utf8.UTFMax:]...)
	}
}

// maxOSC bounds an operating system command; past it the tracker gives up
// and returns to ground.
const maxOSC = 512

// maxParams bounds the parameter text kept for one CSI.
const maxParams = 64

// reset starts tracking a caller screen of w by h at the home position.
func (t *seqTracker) reset(w, h int) { t.cur.reset(w, h) }

func (t *seqTracker) step(b byte) {
	if b == 0x18 || b == 0x1a { // CAN and SUB abort any sequence
		t.state = stGround
		return
	}
	switch t.state {
	case stGround:
		if b == 0x1b {
			t.state = stEsc
			t.cur.cutRune()
		} else {
			t.cur.ground(b)
		}
	case stEsc:
		switch b {
		case '[':
			t.state = stCSI
			t.params, t.interm = t.params[:0], false
		case ']':
			t.state = stOSC
			t.osc = 0
		case '(', ')':
			t.state = stCharset
		case 0x1b:
		default:
			t.cur.esc(b)
			t.state = stGround
		}
	case stCharset:
		t.state = stGround
	case stCSI:
		switch {
		case b >= 0x40 && b <= 0x7e:
			t.state = stGround
			t.cur.csi(b, string(t.params), t.interm)
		case b >= 0x30 && b <= 0x3f:
			if len(t.params) < maxParams {
				t.params = append(t.params, b)
			}
		case b >= 0x20 && b <= 0x2f:
			t.interm = true
		}
	case stOSC:
		switch b {
		case 0x07:
			t.state = stGround
		case 0x1b:
			t.state = stOSCEsc
		default:
			if t.osc++; t.osc > maxOSC {
				t.state = stGround
			}
		}
	case stOSCEsc:
		if b == '\\' {
			t.state = stGround
			return
		}
		// Any other byte aborts the OSC and is read as the byte after ESC.
		t.state = stEsc
		t.step(b)
	}
}

func (t *seqTracker) safe() bool {
	if t.state != stGround {
		return false
	}
	// A rune is incomplete when the last start byte has too few bytes after it.
	for i := len(t.tail) - 1; i >= 0; i-- {
		if utf8.RuneStart(t.tail[i]) {
			return t.tail[i] < utf8.RuneSelf || utf8.FullRune(t.tail[i:])
		}
	}
	return len(t.tail) == 0
}
