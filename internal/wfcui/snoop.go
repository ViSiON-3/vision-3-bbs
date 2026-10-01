package wfcui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
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

	mu       sync.Mutex // guards stdout, mode, statusOn, lastErr
	mode     string
	statusOn bool
	lastErr  string
	w, h     int // sysop terminal size

	result snoopResult
}

func newSnoopCmd(st *admin.SnoopStream, ctl snoopControl, node int, raw rawTerm) *snoopCmd {
	return &snoopCmd{st: st, ctl: ctl, node: node, raw: raw, mode: snoopWatch,
		stdin: os.Stdin, stdout: os.Stdout}
}

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
	c.result = snoopResult{node: c.node}
	c.write("\x1b[0m\x1b[2J\x1b[H")
	c.drawStatus()

	outDone := make(chan struct{})
	go c.pump(outDone)

	done := make(chan struct{})
	in := make(chan byte)
	inEnd := make(chan struct{})
	go c.readStdin(in, inEnd, done)

	reason := c.loop(in, inEnd, outDone)

	// The stdin goroutine stays parked on the real stdin. Once done is
	// closed it drops the byte it is holding when the read returns, so a
	// keystroke typed in the instant of exit is lost but Bubble Tea gets
	// every later one.
	close(done)

	c.mu.Lock()
	mode := c.mode
	c.mu.Unlock()
	switch mode {
	case snoopType:
		_ = c.ctl.TypeIn(false)
	case snoopChat:
		_ = c.ctl.Chat(false)
	}
	_ = c.st.Close()
	<-outDone

	c.result.reason = reason
	c.mu.Lock()
	_, _ = io.WriteString(c.stdout, "\x1b[r\x1b[0m\x1b[2J")
	c.mu.Unlock()
	return nil
}

// readStdin sends stdin bytes to in until done closes or stdin ends.
func (c *snoopCmd) readStdin(in chan<- byte, end chan<- struct{}, done <-chan struct{}) {
	buf := make([]byte, 1)
	for {
		n, err := c.stdin.Read(buf)
		if n > 0 {
			select {
			case in <- buf[0]:
			case <-done:
				return
			}
		}
		if err != nil {
			close(end)
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
			c.setErr(err)
		}
		c.setMode(snoopWatch)
		return
	}
	if err := c.ctl.Chat(true); err != nil {
		c.setErr(err)
		return
	}
	c.setMode(snoopChat)
}

func (c *snoopCmd) toggleStatus() {
	c.mu.Lock()
	c.statusOn = !c.statusOn
	on := c.statusOn
	c.mu.Unlock()
	if on {
		c.drawStatus()
		return
	}
	c.write(fmt.Sprintf("\x1b7\x1b[%d;1H\x1b[0m\x1b[2K\x1b8", c.h))
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

// drawStatus paints the bar on the sysop terminal's last row and puts the
// cursor back where the caller's output left it.
func (c *snoopCmd) drawStatus() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.statusOn || c.h <= 0 || c.w <= 0 {
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
	_, _ = fmt.Fprintf(c.stdout, "\x1b7\x1b[%d;1H\x1b[0;30;47m%s\x1b[0m\x1b8", c.h, text)
}
