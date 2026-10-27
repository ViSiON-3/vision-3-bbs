package menu

import (
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gliderlabs/ssh"
	"github.com/mattn/go-runewidth"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
)

// chatEnv is what split-screen chat needs from the executor. The input
// handler's break-in hook has no executor to hand, so NewExecutor installs
// one with SetSysopChatEnv.
type chatEnv struct {
	theme   func() *config.ThemeConfig
	strings func() *config.StringsConfig
	// menus is the menu set the chat screens are read from. Nil means only
	// the built-in layout is available.
	menus func() menuset.Set
	// caller returns the logged-in handle and saved screen size of the
	// session carrying tap; zero values when unknown.
	caller func(*snoop.Tap) (handle string, width, height int)
	// ended reports that chat on the session carrying tap is over, however
	// it ended. May be nil.
	ended func(*snoop.Tap)
	// holdDropped audits a type-in hold that ended with the chat because the
	// caller ended it. May be nil.
	holdDropped func(tap *snoop.Tap, sysop string, held time.Duration, injected int)
}

var sysopChat atomic.Pointer[chatEnv]

// SetSysopChatEnv installs the chat environment.
func SetSysopChatEnv(env chatEnv) { sysopChat.Store(&env) }

// chatPane is one party's typing area: the segments of the screen they type
// on, in order. The text scrolls up through the segments when the last one
// is full. They need not line up or be the same width, so scrolling is done
// here, segment by segment, rather than by a terminal scroll region.
type chatPane struct {
	w     io.Writer
	mode  ansi.OutputMode // the caller's terminal encoding
	segs  []chatSegment
	color int
	cur   int      // the segment holding the cursor
	line  []rune   // text on the cursor segment
	cells int      // display cells taken by line; the cursor is cells past the segment start
	shown [][]rune // text on every segment, kept for scrolling
	// pending is the start of a UTF-8 sequence split across writes; esc the
	// escape sequence being skipped, see the esc* constants.
	pending []byte
	esc     int
	afterCR bool // an LF or NUL straight after CR is part of the same newline
}

func newChatPane(w io.Writer, mode ansi.OutputMode, segs []chatSegment, color int) *chatPane {
	return &chatPane{w: w, mode: mode, segs: segs, color: color, shown: make([][]rune, len(segs))}
}

// Escape states: after ESC, inside a CSI sequence, and after ESC O.
const (
	escNone = iota
	escStart
	escCSI
	escSS3
)

// skipEscape consumes c when it belongs to an escape sequence, which is
// dropped whole. The state persists across put calls. CR and LF end it.
func (p *chatPane) skipEscape(c byte) bool {
	switch p.esc {
	case escNone:
		if c == 0x1B {
			p.esc = escStart
			p.pending = nil
			return true
		}
		return false
	case escStart:
		switch c {
		case '[':
			p.esc = escCSI
		case 'O':
			p.esc = escSS3
		case 0x1B:
		default:
			p.esc = escNone
			return false
		}
		return true
	case escCSI:
		switch {
		case c == '\r' || c == '\n':
			p.esc = escNone
			return false
		case c >= 0x40 && c <= 0x7E:
			p.esc = escNone
		}
		return true
	default:
		if c == '\r' || c == '\n' {
			p.esc = escNone
			return false
		}
		p.esc = escNone
		return true
	}
}

func (p *chatPane) seg() chatSegment { return p.segs[p.cur] }

// width is the cursor segment's width in cells.
func (p *chatPane) width() int { return p.seg().width() }

func (p *chatPane) moveTo() {
	s := p.seg()
	_, _ = fmt.Fprintf(p.w, "\x1b[%d;%dH%s", s.Row, s.From+p.cells, colorCodeToAnsi(p.color)) // best-effort display
}

// newline moves to the next segment. On the last one the text scrolls up a
// segment instead, and every segment is redrawn.
func (p *chatPane) newline() {
	p.shown[p.cur] = append(p.shown[p.cur][:0], p.line...)
	p.line = p.line[:0]
	p.cells = 0
	if p.cur < len(p.segs)-1 {
		p.cur++
		return
	}
	copy(p.shown, p.shown[1:])
	p.shown[len(p.shown)-1] = nil
	for i := range p.segs {
		p.redraw(i)
	}
}

// redraw blanks segment i and writes its text back, cut to the segment so a
// line that scrolled in from a wider segment cannot run into the art.
func (p *chatPane) redraw(i int) {
	s := p.segs[i]
	_, _ = fmt.Fprintf(p.w, "\x1b[%d;%dH%s%s\x1b[%d;%dH", s.Row, s.From, colorCodeToAnsi(p.color), strings.Repeat(" ", s.width()), s.Row, s.From) // best-effort display
	text, cells := p.shown[i], 0
	for n, r := range text {
		w, _ := p.cellWidth(r)
		if cells+w > s.width() {
			text = text[:n]
			break
		}
		cells += w
	}
	p.emit(text...)
}

// cellWidth is the number of terminal columns r takes in the pane and
// whether it can be shown at all. A CP437 terminal draws one cell per byte,
// and a rune with no CP437 mapping is shown as '?'. Control characters, and
// runes a CP437 terminal would take for one (the glyph-set codes below 0x20),
// are refused so neither side can send an escape sequence to the other.
func (p *chatPane) cellWidth(r rune) (int, bool) {
	if r < 0x20 || r == 0x7F || unicode.IsControl(r) {
		return 0, false
	}
	if p.mode == ansi.OutputModeCP437 {
		if b, ok := ansi.UnicodeToCP437[r]; ok && (b < 0x20 || b == 0x7F) {
			return 0, false
		}
		return 1, true
	}
	if w := runewidth.RuneWidth(r); w > 0 {
		return w, true
	}
	return 0, false
}

func (p *chatPane) emit(r ...rune) {
	_ = terminalio.WriteProcessedBytes(p.w, []byte(string(r)), p.mode)
}

// cellsOf is the display width of text.
func (p *chatPane) cellsOf(text []rune) int {
	n := 0
	for _, r := range text {
		w, _ := p.cellWidth(r)
		n += w
	}
	return n
}

// wrap starts a new line for a character that does not fit. The word being
// typed moves down with it unless it fills the whole line. The next line
// may be narrower than the one the word came from, so the word is put back
// rune by rune and breaks wherever it runs out of room.
func (p *chatPane) wrap() {
	var word []rune
	for i := len(p.line) - 1; i >= 0; i-- {
		if p.line[i] != ' ' {
			continue
		}
		word = append(word, p.line[i+1:]...)
		if len(word) > 0 {
			p.line = p.line[:i+1]
			p.cells -= p.cellsOf(word)
			p.moveTo()
			_, _ = io.WriteString(p.w, strings.Repeat(" ", p.cellsOf(word)))
		}
		break
	}
	p.newline()
	p.moveTo()
	for _, r := range word {
		w, _ := p.cellWidth(r)
		if p.cells+w > p.width() {
			p.newline()
			p.moveTo()
		}
		p.emit(r)
		p.line = append(p.line, r)
		p.cells += w
	}
}

// put writes text at the pane's cursor, word-wrapping at the pane width. b is
// UTF-8; a sequence split across calls is completed by the next one. CR, LF
// or CR LF starts a new line; BS and DEL erase the last character on the
// line. Other control bytes, including ESC, are dropped.
func (p *chatPane) put(b []byte) {
	p.moveTo()
	for _, c := range b {
		if p.skipEscape(c) {
			continue
		}
		afterCR := p.afterCR
		p.afterCR = false
		switch {
		case c == '\r' || c == '\n':
			p.pending = nil
			if c == '\n' && afterCR {
				continue
			}
			p.afterCR = c == '\r'
			p.newline()
			p.moveTo()
		case c == 0x00:
			p.afterCR = afterCR
		case c == 0x08 || c == 0x7F:
			p.pending = nil
			p.backspace()
		case c < 0x20:
			p.pending = nil
		default:
			var seq []byte
			if c < 0x80 {
				p.pending = nil
				seq = []byte{c}
			} else {
				_, seq, p.pending = ansi.DecodeExtendedKey(nil, ansi.OutputModeUTF8, c, p.pending)
				if seq == nil {
					continue
				}
			}
			r, _ := utf8.DecodeRune(seq)
			p.putRune(r)
		}
	}
}

func (p *chatPane) backspace() {
	if len(p.line) == 0 {
		return
	}
	w, _ := p.cellWidth(p.line[len(p.line)-1])
	p.line = p.line[:len(p.line)-1]
	p.cells -= w
	// Position absolutely: after a full-width line the terminal cursor sits
	// on the last column, one short of where cells says, so a relative BS
	// lands one cell short.
	p.moveTo()
	_, _ = io.WriteString(p.w, strings.Repeat(" ", w))
	p.moveTo()
}

func (p *chatPane) putRune(r rune) {
	w, ok := p.cellWidth(r)
	if !ok {
		return
	}
	if p.cells+w > p.width() {
		if r == ' ' {
			p.newline()
			p.moveTo()
			return
		}
		p.wrap()
		if p.cells+w > p.width() {
			p.newline()
			p.moveTo()
		}
	}
	p.emit(r)
	p.line = append(p.line, r)
	p.cells += w
}

type chatEventKind int

const (
	chatSysopBytes chatEventKind = iota
	chatEnd
	chatCallerGone
	chatMinute
)

type chatEvent struct {
	kind chatEventKind
	data []byte
}

// forwardChatEvents turns the tap's chat channels into events for
// ReadKeyOrEvent until stop closes. It never touches the input handler.
func forwardChatEvents(tap *snoop.Tap, end <-chan struct{}, events chan<- chatEvent, stop <-chan struct{}) {
	minute := time.NewTimer(untilNextMinute(time.Now()))
	defer minute.Stop()
	for {
		var ev chatEvent
		select {
		case <-minute.C:
			minute.Reset(untilNextMinute(time.Now()))
			ev = chatEvent{kind: chatMinute}
		case b := <-tap.ChatInput():
			ev = chatEvent{kind: chatSysopBytes, data: b}
		case <-end:
			ev = chatEvent{kind: chatEnd}
		case <-tap.Done():
			ev = chatEvent{kind: chatCallerGone}
		case <-stop:
			return
		}
		select {
		case events <- ev:
		case <-stop:
			return
		}
	}
}

func untilNextMinute(now time.Time) time.Duration {
	return now.Truncate(time.Minute).Add(time.Minute).Sub(now)
}

// chatClockWidth is the clock field at the right end of the divider.
const chatClockWidth = len(" 15:04 ")

func drawChatClock(w io.Writer, row, width int) {
	_, _ = fmt.Fprintf(w, "\x1b[%d;%dH\x1b[0;37m %s \x1b[0m", row, width-chatClockWidth+1, time.Now().Format("15:04")) // best-effort display
}

// runSysopChat draws the built-in split screen and relays both sides; see
// runSysopChatOn.
func runSysopChat(ih *editor.InputHandler, tap *snoop.Tap, w io.Writer, mode ansi.OutputMode, width, height int, sysopHandle, callerHandle string) {
	runSysopChatOn(ih, tap, w, mode, width, height, sysopHandle, callerHandle, nil)
}

// chatStatus draws a chat screen's fields: the names, time, date and the
// minutes since chat began. It is redrawn every minute.
type chatStatus struct {
	w           io.Writer
	mode        ansi.OutputMode
	sc          *chatLayout
	sysop, user string
	began       time.Time
}

func (c *chatStatus) draw() {
	color := c.sc.StatusColor
	if color == 0 {
		color = 7
	}
	now := time.Now()
	for _, f := range []struct {
		field *chatField
		value string
	}{
		{c.sc.Fields.SysopName, c.sysop},
		{c.sc.Fields.CallerName, c.user},
		{c.sc.Fields.Time, now.Format("15:04")},
		{c.sc.Fields.Date, now.Format("01/02/06")},
		{c.sc.Fields.Elapsed, strconv.Itoa(int(now.Sub(c.began).Minutes()))},
	} {
		if f.field == nil {
			continue
		}
		value := []byte(f.value)
		if f.field.Width > 0 {
			value = chatArtPad(f.value, f.field.Width)
		}
		_, _ = fmt.Fprintf(c.w, "\x1b[%d;%dH%s", f.field.Row, f.field.Col, colorCodeToAnsi(color)) // best-effort display
		_ = terminalio.WriteProcessedBytes(c.w, value, c.mode)
	}
}

// runSysopChatOn draws chat screen sc, or the built-in split screen when sc
// is nil, and relays both sides until the caller presses ESC twice, the sysop
// ends chat, or the caller disconnects. The caller's keys are read on this
// goroutine. When chat ends normally the caller's screen is redrawn from the
// tap's catch-up buffer; when the caller has gone nothing more is written.
func runSysopChatOn(ih *editor.InputHandler, tap *snoop.Tap, w io.Writer, mode ansi.OutputMode, width, height int, sysopHandle, callerHandle string, sc *chatLayout) {
	env := sysopChat.Load()
	if env == nil {
		return
	}
	snap, overflowed := tap.Snapshot()
	height = max(height, 5)
	width = max(width, 20)
	th, st := env.theme(), env.strings()
	if sc == nil || sc.art == nil {
		sc = builtinChatLayout(width, height)
	}
	sysopColor, callerColor := sc.Sysop.Color, sc.Caller.Color
	if sysopColor == 0 {
		sysopColor = th.ChatSysopColor
	}
	if callerColor == 0 {
		callerColor = th.ChatUserColor
	}
	sysop := newChatPane(w, mode, sc.Sysop.Lines, sysopColor)
	caller := newChatPane(w, mode, sc.Caller.Lines, callerColor)
	status := &chatStatus{w: w, mode: mode, sc: sc, sysop: sysopHandle, user: callerHandle, began: time.Now()}

	// refresh redraws what changes with the clock.
	var refresh func()
	if sc.art == nil {
		top := sc.dividerRow()
		bar := ansi.ReplacePipeCodes([]byte(fmt.Sprintf(st.SysopChatHeader, sysopHandle, callerHandle)))
		_, _ = fmt.Fprintf(w, "\x1b[0m\x1b[2J\x1b[%d;1H", top) // best-effort display
		_ = terminalio.WriteProcessedBytes(w, bar, mode)
		if fill := width - chatClockWidth - ansi.VisibleLength(string(bar)); fill > 0 {
			_ = terminalio.WriteProcessedBytes(w, []byte("\x1b[0;37m"+strings.Repeat("\xc4", fill)), mode)
		}
		refresh = func() { drawChatClock(w, top, width) }
	} else {
		text := func(v string) func(int) string { return func(int) string { return v } }
		art := chatArtFill(sc.art, map[string]func(int) string{"SYSOP": text(sysopHandle), "CALLER": text(callerHandle)})
		// The art is drawn for the screen's width; on a wider terminal its
		// wraps are made explicit at that width, as fitArt does for stock
		// 80-column art, so a designed 132-column screen is not cut at 80.
		if width > sc.Width {
			art = ansi.HardWrap(art, sc.Width, true)
		}
		_, _ = fmt.Fprint(w, "\x1b[0m\x1b[2J\x1b[H") // best-effort display
		_ = terminalio.WriteProcessedBytes(w, art, mode)
		refresh = status.draw
	}
	refresh()

	events := make(chan chatEvent)
	stop := make(chan struct{})
	fwdDone := make(chan struct{})
	go func() {
		defer close(fwdDone)
		forwardChatEvents(tap, tap.EndChat(), events, stop)
	}()
	defer func() {
		close(stop)
		<-fwdDone
	}()

	lastEsc := false
	var keyPending []byte
loop:
	for {
		k, ev, isEvent, err := editor.ReadKeyOrEvent(ih, events)
		if err != nil {
			return
		}
		if isEvent {
			switch ev.kind {
			case chatSysopBytes:
				sysop.put(ev.data)
				continue
			case chatEnd:
				// Tap.Close also ends chat; a caller who has gone gets no
				// redraw.
				select {
				case <-tap.Done():
					return
				default:
				}
				break loop
			case chatCallerGone:
				return
			case chatMinute:
				refresh()
				caller.moveTo()
				continue
			}
		}
		if k == editor.KeyEsc {
			if lastEsc {
				break loop
			}
			lastEsc = true
			continue
		}
		lastEsc = false
		switch {
		case k >= 0 && k < 0x80:
			keyPending = nil
			caller.put([]byte{byte(k)})
		case k >= 0x80 && k <= 0xFF:
			// A key byte arrives in the caller's encoding: CP437 is one
			// byte per character, UTF-8 a sequence a byte at a time.
			var text []byte
			text, _, keyPending = ansi.DecodeExtendedKey(nil, mode, byte(k), keyPending)
			caller.put(text)
		default:
			keyPending = nil
		}
	}

	_, _ = fmt.Fprint(w, "\x1b[r\x1b[0m\x1b[2J\x1b[H") // best-effort display
	if !overflowed {
		_, _ = w.Write(snap)
	} else {
		_ = terminalio.WriteProcessedBytes(w, ansi.ReplacePipeCodes([]byte(st.SysopChatBack)), mode)
	}
}

// serviceSysopChat is the input handler's break-in hook: it opens chat for a
// pending sysop request and logs it.
func serviceSysopChat(s ssh.Session, ih *editor.InputHandler, tap *snoop.Tap) {
	env := sysopChat.Load()
	v, ok := sessionOutputs.Load(s)
	if env == nil || !ok {
		return
	}
	if !tap.ChatBegan() {
		return
	}
	defer func() {
		if dropped, held, n := tap.ChatEnded(); dropped != "" {
			if env.holdDropped != nil {
				env.holdDropped(tap, dropped, held, n)
			}
		}
		if env.ended != nil {
			env.ended(tap)
		}
	}()

	handle, width, height := env.caller(tap)
	if handle == "" {
		handle = "caller"
	}
	if pty, _, isPty := s.Pty(); isPty {
		if width <= 0 {
			width = pty.Window.Width
		}
		if height <= 0 {
			height = pty.Window.Height
		}
	}
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 25
	}
	sysop := tap.KeyboardHolder()
	sc := chooseChatLayout(env, tap.ChatScreen(), width, height)
	start := time.Now()
	slog.Info("sysop chat started", "caller", handle, "sysop", sysop, "screen", sc.name)
	runSysopChatOn(ih, tap, v.(io.Writer), sessionOutputMode(s), width, height, sysop, handle, sc)
	took := time.Since(start)
	addChatCredit(s, took)
	slog.Info("sysop chat ended", "caller", handle, "sysop", sysop, "duration", took.Round(time.Second))
}
