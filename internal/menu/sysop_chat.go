package menu

import (
	"fmt"
	"io"
	"log/slog"
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
	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
)

// chatEnv is what split-screen chat needs from the executor. The input
// handler's break-in hook has no executor to hand, so NewExecutor installs
// one with SetSysopChatEnv.
type chatEnv struct {
	theme   func() *config.ThemeConfig
	strings func() *config.StringsConfig
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

// chatPane is one half of the split screen: rows first..last (1-based).
type chatPane struct {
	w           io.Writer
	mode        ansi.OutputMode // the caller's terminal encoding
	first, last int
	width       int
	row, col    int
	color       int
	line        []rune // text on the cursor row
	cells       int    // display cells taken by line; col is cells+1
	pending     []byte // start of a UTF-8 sequence split across writes
	afterCR     bool   // an LF or NUL straight after CR is part of the same newline
}

func (p *chatPane) moveTo() {
	fmt.Fprintf(p.w, "\x1b[%d;%dH%s", p.row, p.col, colorCodeToAnsi(p.color))
}

func (p *chatPane) newline() {
	p.col = 1
	p.line = p.line[:0]
	p.cells = 0
	if p.row < p.last {
		p.row++
		return
	}
	fmt.Fprintf(p.w, "\x1b[%d;%dr\x1b[%d;1H\n\x1b[r", p.first, p.last, p.last)
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

// wrap starts a new line for a character that does not fit. The word being
// typed moves down with it unless it fills the whole line.
func (p *chatPane) wrap() {
	var word []rune
	wordCells := 0
	for i := len(p.line) - 1; i >= 0; i-- {
		if p.line[i] != ' ' {
			continue
		}
		word = append(word, p.line[i+1:]...)
		for _, r := range word {
			w, _ := p.cellWidth(r)
			wordCells += w
		}
		if len(word) > 0 {
			p.col = 1 + p.cells - wordCells
			p.moveTo()
			_, _ = io.WriteString(p.w, strings.Repeat(" ", wordCells))
		}
		break
	}
	p.newline()
	p.moveTo()
	p.emit(word...)
	p.line = append(p.line, word...)
	p.cells = wordCells
	p.col = p.cells + 1
}

// put writes text at the pane's cursor, word-wrapping at the pane width. b is
// UTF-8; a sequence split across calls is completed by the next one. CR, LF
// or CR LF starts a new line; BS and DEL erase the last character on the
// line. Other control bytes, including ESC, are dropped.
func (p *chatPane) put(b []byte) {
	p.moveTo()
	for _, c := range b {
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
	p.col -= w
	for range w {
		_, _ = io.WriteString(p.w, "\b")
	}
	_, _ = io.WriteString(p.w, strings.Repeat(" ", w))
	for range w {
		_, _ = io.WriteString(p.w, "\b")
	}
}

func (p *chatPane) putRune(r rune) {
	w, ok := p.cellWidth(r)
	if !ok {
		return
	}
	if p.cells+w > p.width {
		if r == ' ' {
			p.newline()
			p.moveTo()
			return
		}
		p.wrap()
	}
	p.emit(r)
	p.line = append(p.line, r)
	p.cells += w
	p.col += w
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
	fmt.Fprintf(w, "\x1b[%d;%dH\x1b[0;37m %s \x1b[0m", row, width-chatClockWidth+1, time.Now().Format("15:04"))
}

// runSysopChat draws the split screen and relays both sides until the caller
// presses ESC twice, the sysop ends chat, or the caller disconnects. The
// caller's keys are read on this goroutine. When chat ends normally the
// caller's screen is redrawn from the tap's catch-up buffer; when the caller
// has gone nothing more is written.
func runSysopChat(ih *editor.InputHandler, tap *snoop.Tap, w io.Writer, mode ansi.OutputMode, width, height int, sysopHandle, callerHandle string) {
	env := sysopChat.Load()
	if env == nil {
		return
	}
	snap, overflowed := tap.Snapshot()
	height = max(height, 5)
	width = max(width, 20)
	th, st := env.theme(), env.strings()
	top := (height - 1) / 2
	sysop := &chatPane{w: w, mode: mode, first: 1, last: top, width: width, row: 1, col: 1, color: th.ChatSysopColor}
	caller := &chatPane{w: w, mode: mode, first: top + 2, last: height, width: width, row: top + 2, col: 1, color: th.ChatUserColor}

	bar := ansi.ReplacePipeCodes([]byte(fmt.Sprintf(st.SysopChatHeader, sysopHandle, callerHandle)))
	fmt.Fprintf(w, "\x1b[0m\x1b[2J\x1b[%d;1H", top+1)
	_ = terminalio.WriteProcessedBytes(w, bar, mode)
	if fill := width - chatClockWidth - ansi.VisibleLength(string(bar)); fill > 0 {
		_ = terminalio.WriteProcessedBytes(w, []byte("\x1b[0;37m"+strings.Repeat("\xc4", fill)), mode)
	}
	drawChatClock(w, top+1, width)

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
				drawChatClock(w, top+1, width)
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

	fmt.Fprint(w, "\x1b[r\x1b[0m\x1b[2J\x1b[H")
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
	start := time.Now()
	slog.Info("sysop chat started", "caller", handle, "sysop", sysop)
	runSysopChat(ih, tap, v.(io.Writer), sessionOutputMode(s), width, height, sysop, handle)
	took := time.Since(start)
	addChatCredit(s, took)
	slog.Info("sysop chat ended", "caller", handle, "sysop", sysop, "duration", took.Round(time.Second))
}
