package menu

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gliderlabs/ssh"

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
}

var sysopChat atomic.Pointer[chatEnv]

// SetSysopChatEnv installs the chat environment.
func SetSysopChatEnv(env chatEnv) { sysopChat.Store(&env) }

// chatPane is one half of the split screen: rows first..last (1-based).
type chatPane struct {
	w           io.Writer
	first, last int
	width       int
	row, col    int
	color       int
}

func (p *chatPane) moveTo() {
	fmt.Fprintf(p.w, "\x1b[%d;%dH%s", p.row, p.col, colorCodeToAnsi(p.color))
}

func (p *chatPane) newline() {
	p.col = 1
	if p.row < p.last {
		p.row++
		return
	}
	fmt.Fprintf(p.w, "\x1b[%d;%dr\x1b[%d;1H\n\x1b[r", p.first, p.last, p.last)
}

// put writes printable ASCII at the pane's cursor, wrapping at the pane
// width. CR or LF starts a new line; BS and DEL erase back to column 1.
func (p *chatPane) put(b []byte) {
	p.moveTo()
	for _, c := range b {
		switch {
		case c == '\r' || c == '\n':
			p.newline()
			p.moveTo()
		case c == 0x08 || c == 0x7F:
			if p.col > 1 {
				p.col--
				_, _ = io.WriteString(p.w, "\b \b")
			}
		case c >= 0x20 && c < 0x7F:
			if p.col > p.width {
				p.newline()
				p.moveTo()
			}
			_, _ = p.w.Write([]byte{c})
			p.col++
		}
	}
}

type chatEventKind int

const (
	chatSysopBytes chatEventKind = iota
	chatEnd
	chatCallerGone
)

type chatEvent struct {
	kind chatEventKind
	data []byte
}

// forwardChatEvents turns the tap's chat channels into events for
// ReadKeyOrEvent until stop closes. It never touches the input handler.
func forwardChatEvents(tap *snoop.Tap, end <-chan struct{}, events chan<- chatEvent, stop <-chan struct{}) {
	for {
		var ev chatEvent
		select {
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
	th, st := env.theme(), env.strings()
	top := (height - 1) / 2
	sysop := &chatPane{w: w, first: 1, last: top, width: width, row: 1, col: 1, color: th.ChatSysopColor}
	caller := &chatPane{w: w, first: top + 2, last: height, width: width, row: top + 2, col: 1, color: th.ChatUserColor}

	bar := ansi.ReplacePipeCodes([]byte(fmt.Sprintf(st.SysopChatHeader, sysopHandle, callerHandle)))
	fmt.Fprintf(w, "\x1b[0m\x1b[2J\x1b[%d;1H", top+1)
	_ = terminalio.WriteProcessedBytes(w, bar, mode)
	if fill := width - ansi.VisibleLength(string(bar)); fill > 0 {
		_ = terminalio.WriteProcessedBytes(w, []byte("\x1b[0;37m"+strings.Repeat("\xc4", fill)), mode)
	}
	fmt.Fprint(w, "\x1b[0m")

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
		if k >= 0 && k < 0x80 {
			caller.put([]byte{byte(k)})
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
	defer tap.ChatEnded()

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
	slog.Info("sysop chat ended", "caller", handle, "sysop", sysop, "duration", time.Since(start).Round(time.Second))
}
