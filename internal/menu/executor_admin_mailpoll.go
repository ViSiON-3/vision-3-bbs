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
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// mailPollTimeout bounds a menu-started poll. v3mail gives each binkd call
// five minutes and a board may carry several networks, so this is generous;
// it only exists so a wedged hub cannot hold the sysop's node forever.
const mailPollTimeout = 20 * time.Minute

// runMailPoll runs `v3mail poll` and shows its output as it happens, so a
// sysop can send and fetch mail for every FTN and QWK network without a shell.
// Anything after RUN:MAILPOLL in the menu entry is passed on as v3mail flags,
// e.g. "RUN:MAILPOLL --network fsxnet".
//
// It shells out rather than polling in-process on purpose: v3mail owns the
// FTN and QWK dupe databases and the QWK REP lock, and the scheduled polls
// already go through it, so the menu gets exactly the same locking.
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
		write("|03Sending and fetching mail for every network. This can take a few minutes...|07\r\n\r\n")
		code, err := execMailPoll(v3mail, root, e.RootConfigPath, strings.Fields(args),
			&crlfWriter{w: terminalWriterFunc(func(p []byte) {
				_ = terminalio.WriteProcessedBytes(terminal, p, outputMode)
			})})
		write("\r\n" + rule)
		switch {
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

// execMailPoll runs `v3mail poll` from the BBS root with output copied to out,
// returning v3mail's exit code. err is set only when v3mail could not be run
// or was stopped by the timeout; a poll that ran and reported errors is a
// non-zero code with a nil err.
func execMailPoll(v3mail, root, configDir string, extra []string, out io.Writer) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mailPollTimeout)
	defer cancel()

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
	cmd.WaitDelay = 5 * time.Second

	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return -1, fmt.Errorf("gave up after %s", mailPollTimeout)
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

// terminalWriterFunc adapts a write callback to io.Writer.
type terminalWriterFunc func(p []byte)

func (f terminalWriterFunc) Write(p []byte) (int, error) {
	f(p)
	return len(p), nil
}

// crlfWriter turns bare LF line endings into CRLF, which a BBS terminal needs
// to return to column 0; v3mail writes plain "\n".
type crlfWriter struct {
	w      io.Writer
	lastCR bool
}

func (c *crlfWriter) Write(p []byte) (int, error) {
	var buf bytes.Buffer
	for _, b := range p {
		if b == '\n' && !c.lastCR {
			buf.WriteByte('\r')
		}
		buf.WriteByte(b)
		c.lastCR = b == '\r'
	}
	if _, err := c.w.Write(buf.Bytes()); err != nil {
		return 0, err
	}
	return len(p), nil
}
