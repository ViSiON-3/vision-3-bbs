package menu

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// mailPollCallTimeout is v3mail poll's default limit on one binkd call, and
// mailPollAllowance covers scanning, packing, tossing and QWK exchanges. A
// menu-started poll gets one call's worth per hub on top of the allowance, so
// a board with many hubs is not cut off part way while a wedged hub still
// cannot hold the sysop's node forever.
const (
	mailPollCallTimeout = 5 * time.Minute
	mailPollAllowance   = 10 * time.Minute
)

// mailPollStopGrace is how long v3mail has to finish up after being asked to
// stop (it stops calling hubs, and a toss in progress completes) before it is
// killed.
const mailPollStopGrace = 30 * time.Second

// mailPollDeadline returns how long a menu-started poll may run: the
// allowance plus one call per FTN hub with a hostname on an enabled network
// and one per enabled QWK network. A call is the --timeout given in args (the
// flags the menu entry passes to v3mail), or v3mail's default.
func mailPollDeadline(configDir string, args []string) time.Duration {
	perCall := mailPollArgTimeout(args)
	calls := 0
	if ftnCfg, err := config.LoadFTNConfig(configDir); err == nil {
		for _, nc := range ftnCfg.Networks {
			if !nc.InternalTosserEnabled {
				continue
			}
			for _, lnk := range nc.Links {
				if lnk.HostPort() != "" {
					calls++
				}
			}
		}
	}
	if qcfg, err := config.LoadQWKNetConfig(configDir); err == nil {
		for _, nc := range qcfg.Networks {
			if nc.Enabled {
				calls++
			}
		}
	}
	return mailPollAllowance + time.Duration(calls)*perCall
}

// mailPollArgTimeout returns the --timeout (or -timeout, with or without "=")
// in v3mail flags, or mailPollCallTimeout when there is none or it does not
// parse; v3mail then rejects a bad value itself.
func mailPollArgTimeout(args []string) time.Duration {
	perCall := mailPollCallTimeout
	for i := 0; i < len(args); i++ {
		name, value, hasValue := strings.Cut(strings.TrimLeft(args[i], "-"), "=")
		if name != "timeout" || !strings.HasPrefix(args[i], "-") {
			continue
		}
		if !hasValue {
			if i+1 >= len(args) {
				break
			}
			i++
			value = args[i]
		}
		if d, err := time.ParseDuration(value); err == nil && d > 0 {
			perCall = d
		}
	}
	return perCall
}

// runMailPoll runs `v3mail poll` and shows its output as it happens, so a
// sysop can send and fetch mail for every FTN and QWK network without a shell.
// Anything after RUN:MAILPOLL in the menu entry is passed on as v3mail flags,
// e.g. "RUN:MAILPOLL --network fsxnet".
//
// It shells out rather than polling in-process on purpose: v3mail owns the
// FTN and QWK dupe databases, the QWK toss and REP locks, and the poll lock
// that keeps two polls from running at once.
func runMailPoll(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	terminal := c.terminal
	outputMode := c.outputMode

	if c.currentUser == nil {
		return nil, "", nil
	}
	sysOpACS := fmt.Sprintf("S%d", e.GetServerConfig().SysOpLevel)
	if !checkACS(sysOpACS, c.currentUser, c.s, terminal, c.sessionStartTime) {
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Access denied.|07\r\n")), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	write := func(pipe string) {
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(pipe)), outputMode)
	}
	rule := "|08" + strings.Repeat("─", 79) + "|07\r\n"
	write(ansi.ClearScreen() + "|12Poll Mail Networks|07\r\n" + rule)

	root := filepath.Dir(e.RootConfigPath)
	v3mail := filepath.Join(root, "v3mail")
	if runtime.GOOS == "windows" {
		v3mail += ".exe"
	}
	if _, err := os.Stat(v3mail); err != nil {
		write(fmt.Sprintf("|01v3mail not found at %s|07\r\n", v3mail))
	} else {
		write("|03Sending and fetching mail for every network. This can take a few minutes.|07\r\n" +
			"|08Press |07ESC|08 or |07Q|08 to stop; mail already received is still tossed.|07\r\n\r\n")
		// Tied to the session, so a sysop who hangs up does not leave the
		// poll running with their node held.
		ctx, cancel := context.WithTimeout(c.s.Context(), mailPollDeadline(e.RootConfigPath, strings.Fields(args)))
		done := make(chan struct{})
		watched := make(chan struct{})
		go func() {
			defer close(watched)
			watchPollStopKey(getSessionIH(c.s), done, cancel)
		}()
		out := &crlfWriter{w: terminalWriterFunc(func(p []byte) {
			_ = terminalio.WriteProcessedBytes(terminal, p, outputMode)
		})}
		code, err := execMailPoll(ctx, v3mail, root, e.RootConfigPath, strings.Fields(args), out)
		close(done)
		<-watched // the pause prompt below must get the next key, not the watcher
		cancel()
		out.Flush()
		write("\r\n" + rule)
		switch {
		case errors.Is(err, errMailPollStopped):
			write("|14Poll stopped.|07\r\n")
		case err != nil:
			slog.Error("menu mail poll failed to run", "node", c.nodeNumber, "error", err)
			write(fmt.Sprintf("|01Poll did not finish: %v|07\r\n", err))
		case code != 0:
			write("|14Poll finished with errors — details above and in data/logs/v3mail.log|07\r\n")
		default:
			write("|10Poll complete.|07\r\n")
		}
	}

	pausePrompt := e.Strings().PauseString
	if pausePrompt == "" {
		pausePrompt = "\r\n|07Press |15[ENTER]|07 to continue... "
	}
	if err := writeCenteredPausePrompt(c.s, terminal, pausePrompt, outputMode, c.termWidth, c.termHeight); err != nil {
		return nil, "", err
	}
	return nil, "", nil
}

// pollKeyReader is the part of the session input the stop-key watcher uses.
type pollKeyReader interface {
	ReadKeyWithTimeout(d time.Duration) (int, error)
}

// pollKeyCheck is how often the stop-key watcher looks for done.
const pollKeyCheck = 200 * time.Millisecond

// watchPollStopKey calls stop when the sysop presses ESC or Q, or the session
// input ends, and returns once done is closed. It reads with a short timeout
// so it notices done promptly and leaves later keys to whatever reads next;
// a timed-out read consumes nothing.
func watchPollStopKey(in pollKeyReader, done <-chan struct{}, stop func()) {
	for {
		select {
		case <-done:
			return
		default:
		}
		k, err := in.ReadKeyWithTimeout(pollKeyCheck)
		switch {
		case errors.Is(err, editor.ErrIdleTimeout):
			continue
		case err != nil:
			stop() // disconnected; the session context ends the poll too
			<-done
			return
		case k == int(editor.KeyEsc) || k == 'q' || k == 'Q':
			stop()
			<-done
			return
		}
	}
}

// execMailPoll runs `v3mail poll` from the BBS root with output copied to out,
// returning v3mail's exit code. err is set only when v3mail could not be run
// or was stopped by ctx; a poll that ran and reported errors is a non-zero
// code with a nil err.
//
// When ctx ends, v3mail is sent SIGTERM rather than killed, so it stops its
// binkd call (a killed v3mail would leave binkd running) and finishes any
// toss in hand. It is killed only if it is still running mailPollStopGrace
// later.
func execMailPoll(ctx context.Context, v3mail, root, configDir string, extra []string, out io.Writer) (int, error) {
	start := time.Now()
	args := append([]string{"poll", "--config", configDir, "--data", filepath.Join(root, "data")}, extra...)
	cmd := exec.CommandContext(ctx, v3mail, args...)
	cmd.Dir = root
	// Keep v3mail's log records in its log file and off the sysop's screen;
	// only the poll's own progress lines are meant to be read here.
	cmd.Env = append(os.Environ(), "V3MAIL_NO_CONSOLE_LOG=1")
	// The same writer for both: exec then serializes writes, so stdout and
	// stderr lines never interleave mid-line.
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Cancel = func() error {
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			return cmd.Process.Kill() // Windows cannot deliver SIGTERM
		}
		return nil
	}
	cmd.WaitDelay = mailPollStopGrace

	err := cmd.Run()
	if errors.Is(err, exec.ErrWaitDelay) {
		// v3mail exited; a child it left behind held the output open.
		err = nil
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return -1, fmt.Errorf("gave up after %s", time.Since(start).Round(time.Second))
	case ctx.Err() != nil:
		return -1, errMailPollStopped
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}

// errMailPollStopped reports a poll stopped by the sysop or a disconnect.
var errMailPollStopped = errors.New("stopped")

// terminalWriterFunc adapts a write callback to io.Writer.
type terminalWriterFunc func(p []byte)

func (f terminalWriterFunc) Write(p []byte) (int, error) {
	f(p)
	return len(p), nil
}

// crlfWriter turns bare LF line endings into CRLF, which a BBS terminal needs
// to return to column 0; v3mail writes plain "\n". It passes on whole lines
// only, so a multi-byte character split across two pipe reads reaches the
// terminal's charset conversion in one piece; Flush sends any unfinished
// last line.
type crlfWriter struct {
	w       io.Writer
	lastCR  bool
	pending []byte
}

func (c *crlfWriter) Write(p []byte) (int, error) {
	for _, b := range p {
		if b == '\n' && !c.lastCR {
			c.pending = append(c.pending, '\r')
		}
		c.pending = append(c.pending, b)
		c.lastCR = b == '\r'
	}
	if i := bytes.LastIndexByte(c.pending, '\n'); i >= 0 {
		line := c.pending[:i+1]
		if _, err := c.w.Write(line); err != nil {
			return 0, err
		}
		c.pending = append(c.pending[:0], c.pending[i+1:]...)
	}
	return len(p), nil
}

// Flush writes any buffered partial line.
func (c *crlfWriter) Flush() {
	if len(c.pending) > 0 {
		_, _ = c.w.Write(c.pending)
		c.pending = c.pending[:0]
	}
}
