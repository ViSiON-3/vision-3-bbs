package transfer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// readInterrupter is implemented by session adapters that support unblocking
// a pending Read() call (e.g. TelnetSessionAdapter).
type readInterrupter interface {
	SetReadInterrupt(ch <-chan struct{})
}

// rawBinaryWriter is implemented by BBSSession (and TelnetSessionAdapter) to
// provide a write path that bypasses gliderlabs' \n→\r\n CRLF conversion.
// Without this, ZMODEM frame type byte 0x0A (ZDATA) is expanded to 0x0D 0x0A,
// shifting the header and causing CRC mismatches at the receiver.
type rawBinaryWriter interface {
	RawWrite(p []byte) (int, error)
}

// transferLocker is implemented by BBSSession to signal that a binary
// transfer is in progress. While active, background code (e.g. terminal
// repaint on window resize) must not write to the session.
type transferLocker interface {
	SetTransferActive(active bool)
}

// writeFunc adapts a func([]byte)(int,error) to the io.Writer interface.
type writeFunc func([]byte) (int, error)

func (f writeFunc) Write(p []byte) (int, error) { return f(p) }

// Adaptive chunk sizing constants for binary transfers.
const (
	adaptiveMinChunk    = 4096     // 4 KB — starting chunk size
	adaptiveMaxChunk    = 8192    // 8 KB — ceiling (SyncTerm chokes above this)
	adaptiveRampWrites  = 50      // double chunk size every N writes at current level
	adaptiveBackoffDiv  = 2       // halve chunk size on ZRPOS detection
)

// adaptiveCopy copies src → dst with dynamic chunk sizing that ramps up for
// throughput and backs off when the receiver signals trouble (via ZRPOS).
//
// The backoff signal is read from the backoff atomic: when the stdin goroutine
// detects a ZRPOS frame (receiver requesting retransmission), it increments
// backoff. adaptiveCopy checks this counter on each write and halves the chunk
// size when it changes, then resumes ramping after stabilizing.
//
// Ramp schedule: starts at 4 KB, doubles every ~50 writes, caps at 8 KB.
// Backoff: halves chunk size (floor 4 KB), resets ramp counter.
func adaptiveCopy(dst io.Writer, src io.Reader, backoff *atomic.Int32) (int64, error) {
	hasher := sha256.New()
	chunkSize := adaptiveMinChunk
	buf := make([]byte, adaptiveMaxChunk) // allocate max once, slice as needed
	var total int64
	var writesAtLevel int
	var lastBackoff int32

	for {
		// Check for backoff signal from stdin goroutine (ZRPOS detected).
		if backoff != nil {
			cur := backoff.Load()
			if cur != lastBackoff {
				lastBackoff = cur
				oldChunk := chunkSize
				chunkSize = chunkSize / adaptiveBackoffDiv
				if chunkSize < adaptiveMinChunk {
					chunkSize = adaptiveMinChunk
				}
				writesAtLevel = 0
				log.Printf("DEBUG: adaptiveCopy: ZRPOS backoff %d→%d bytes (signal #%d) at %d total bytes",
					oldChunk, chunkSize, cur, total)
			}
		}

		// Ramp up chunk size after sustained successful writes.
		writesAtLevel++
		if writesAtLevel >= adaptiveRampWrites && chunkSize < adaptiveMaxChunk {
			oldChunk := chunkSize
			chunkSize = chunkSize * 2
			if chunkSize > adaptiveMaxChunk {
				chunkSize = adaptiveMaxChunk
			}
			writesAtLevel = 0
			log.Printf("DEBUG: adaptiveCopy: ramp %d→%d bytes at %d total bytes", oldChunk, chunkSize, total)
		}

		nr, rerr := src.Read(buf[:chunkSize])
		if nr > 0 {
			nw, werr := dst.Write(buf[:nr])
			total += int64(nw)
			hasher.Write(buf[:nr])
			if werr != nil {
				log.Printf("DEBUG: adaptiveCopy: write error after %d bytes: %v", total, werr)
				return total, werr
			}
			if nw != nr {
				log.Printf("DEBUG: adaptiveCopy: short write %d/%d after %d total bytes", nw, nr, total)
				return total, io.ErrShortWrite
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				log.Printf("DEBUG: adaptiveCopy: finished. Bytes=%d ChunkSize=%d SHA256=%x",
					total, chunkSize, hasher.Sum(nil))
				return total, nil
			}
			log.Printf("DEBUG: adaptiveCopy: read error after %d bytes: %v", total, rerr)
			return total, rerr
		}
	}
}

// RunCommandDirect executes an external command with its stdin/stdout/stderr
// piped directly to the SSH session — no PTY allocated. This is essential for
// binary file-transfer protocols (ZMODEM, YMODEM, XMODEM) where a PTY's line
// discipline would corrupt the data stream.
//
// ctx controls cancellation and timeout: when ctx.Done() fires, the process is
// killed and the function returns ctx.Err(). Pass context.Background() for
// no timeout. Callers should use context.WithTimeout for transfer timeouts.
//
// stdinIdleTimeout, when > 0, kills the process if no bytes arrive from the
// client for that duration. Use this for receive (upload) mode: if the client
// never responds to the initial handshake (e.g. user cancels the SyncTerm
// upload dialog without sending a ZModem abort), the process would otherwise
// retry indefinitely. Pass 0 to disable.
func RunCommandDirect(ctx context.Context, s ssh.Session, cmd *exec.Cmd, stdinIdleTimeout time.Duration) error {
	// Signal that a binary transfer is active so background code (terminal
	// repaint on window resize) doesn't write to the session and corrupt
	// the binary stream.
	if tl, ok := s.(transferLocker); ok {
		tl.SetTransferActive(true)
		defer tl.SetTransferActive(false)
	}

	if ctx == nil {
		ctx = context.Background()
	}
	log.Printf("DEBUG: Starting command '%s' in DIRECT (no-PTY) mode", cmd.Path)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe for '%s': %w", cmd.Path, err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe for '%s': %w", cmd.Path, err)
	}
	// Capture stderr separately — NEVER merge it into stdout.
	// External protocol drivers (e.g. sexyz) write status/progress messages
	// to stderr; merging them into stdout corrupts the binary data stream
	// (ZModem frames, etc.) and causes transfers to fail.
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command '%s': %w", cmd.Path, err)
	}

	inputDone := make(chan struct{})
	outputDone := make(chan error, 1)

	// stdinActivity receives a signal each time bytes arrive from the session.
	// Used by the idle monitor goroutine below; nil when idle timeout is disabled.
	var stdinActivity chan struct{}
	if stdinIdleTimeout > 0 {
		stdinActivity = make(chan struct{}, 1)
	}

	// zrposBackoff is incremented by the stdin goroutine when it detects a
	// ZRPOS frame from the client (receiver requesting retransmission). The
	// output goroutine's adaptiveCopy reads this to halve its chunk size.
	var zrposBackoff atomic.Int32

	// session → command stdin
	// Uses a manual read loop (rather than io.Copy) so we can:
	//  1. Signal non-CAN stdin activity to the idle monitor.
	//  2. Detect ZModem abort (5+ consecutive CAN / 0x18 bytes) and kill the
	//     process immediately rather than waiting for the idle timer.
	//  3. Detect ZRPOS frames (receiver requesting retransmission due to data
	//     corruption) and signal the output goroutine to reduce chunk size.
	go func() {
		defer close(inputDone)
		buf := make([]byte, 32*1024)
		var total int64
		var cpErr error
		var canRun int  // consecutive CAN (0x18) bytes seen so far
		var killed bool // set once CAN abort fires; stops further writes
		var prevTail [6]byte // tail bytes from previous read for split-header detection
		var prevLen int
		for {
			nr, rerr := s.Read(buf)
			if nr > 0 {
				// Check for ZRPOS headers split across read boundaries.
				// Concatenate previous tail with start of current buffer.
				if prevLen > 0 {
					combined := make([]byte, prevLen+nr)
					copy(combined, prevTail[:prevLen])
					copy(combined[prevLen:], buf[:nr])
					// Only need to scan the overlap region (positions where a header could span)
					scanEnd := prevLen + 5 // max header is 6 bytes
					if scanEnd > len(combined) {
						scanEnd = len(combined)
					}
					for i := 0; i < scanEnd; i++ {
						b := combined[i]
						if b == 0x09 && i >= 3 {
							chunk := combined[i-3 : i+1]
							if chunk[0] == 0x2a && chunk[1] == 0x18 && chunk[2] == 0x41 {
								zrposBackoff.Add(1)
								log.Printf("DEBUG: (%s) ZRPOS binary header detected across boundary at offset %d (backoff #%d)",
									cmd.Path, total-int64(prevLen)+int64(i), zrposBackoff.Load())
							}
						}
						if b == '9' && i >= 5 {
							chunk := combined[i-5 : i+1]
							if chunk[0] == 0x2a && chunk[1] == 0x2a && chunk[2] == 0x18 &&
								chunk[3] == 0x42 && chunk[4] == '0' {
								zrposBackoff.Add(1)
								log.Printf("DEBUG: (%s) ZRPOS hex header detected across boundary at offset %d (backoff #%d)",
									cmd.Path, total-int64(prevLen)+int64(i), zrposBackoff.Load())
							}
						}
					}
				}
				// Save tail for next iteration
				if nr >= 6 {
					copy(prevTail[:], buf[nr-6:nr])
					prevLen = 6
				} else {
					copy(prevTail[:], buf[:nr])
					prevLen = nr
				}

				// Scan for consecutive CAN bytes, ZRPOS frames, and decide
				// whether this chunk counts as real file activity.
				hasNonCAN := false
				for i, b := range buf[:nr] {
					if b == 0x18 { // CAN
						canRun++
						if canRun >= 5 && !killed {
							killed = true
							log.Printf("DEBUG: (%s) ZModem CAN abort detected in stdin; killing process", cmd.Path)
							if cmd.Process != nil {
								_ = cmd.Process.Kill()
							}
							_ = stdoutPipe.Close()
						}
					} else {
						canRun = 0
						hasNonCAN = true
					}

					// Detect ZRPOS headers in the byte stream.
					// Hex header: 2A 2A 18 42 30 39 (ZDLE ZHEX "B" "09")
					//   → "**\x18B09" where 09 = ZRPOS frame type
					// Binary header: 2A 18 41 09 (ZPAD ZDLE ZBIN ZRPOS)
					if b == 0x09 && i >= 3 {
						chunk := buf[i-3 : i+1]
						if chunk[0] == 0x2a && chunk[1] == 0x18 && chunk[2] == 0x41 {
							// Binary ZRPOS header: * ZDLE A ZRPOS
							zrposBackoff.Add(1)
							log.Printf("DEBUG: (%s) ZRPOS binary header detected in stdin at offset %d (backoff #%d)",
								cmd.Path, total+int64(i), zrposBackoff.Load())
						}
					}
					if b == '9' && i >= 5 {
						chunk := buf[i-5 : i+1]
						if chunk[0] == 0x2a && chunk[1] == 0x2a && chunk[2] == 0x18 &&
							chunk[3] == 0x42 && chunk[4] == '0' {
							// Hex ZRPOS header: ** ZDLE B 0 9
							zrposBackoff.Add(1)
							log.Printf("DEBUG: (%s) ZRPOS hex header detected in stdin at offset %d (backoff #%d)",
								cmd.Path, total+int64(i), zrposBackoff.Load())
						}
					}
				}
				if killed {
					break
				}
				// Only reset the idle timer for real file data.
				if stdinActivity != nil && hasNonCAN {
					select {
					case stdinActivity <- struct{}{}:
					default:
					}
				}
				nw, werr := stdinPipe.Write(buf[:nr])
				total += int64(nw)
				if werr != nil {
					cpErr = werr
					break
				}
			}
			if rerr != nil {
				if rerr != io.EOF {
					cpErr = rerr
				}
				break
			}
		}
		log.Printf("DEBUG: (%s) direct stdin copy finished. Bytes: %d, Error: %v", cmd.Path, total, cpErr)
		_ = stdinPipe.Close()
	}()

	// Idle monitor: if no bytes arrive from the client within stdinIdleTimeout,
	// the user has likely cancelled their terminal uploader without sending a
	// ZModem abort (CAN sequence). Kill the process so the BBS doesn't loop
	// indefinitely re-offering the transfer.
	if stdinIdleTimeout > 0 {
		go func() {
			timer := time.NewTimer(stdinIdleTimeout)
			defer timer.Stop()
			for {
				select {
				case <-timer.C:
					log.Printf("DEBUG: (%s) no client stdin activity for %v; killing process", cmd.Path, stdinIdleTimeout)
					if cmd.Process != nil {
						_ = cmd.Process.Kill()
					}
					_ = stdoutPipe.Close() // unblock the output goroutine
					return
				case <-stdinActivity:
					// Client is active — reset the idle timer.
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					timer.Reset(stdinIdleTimeout)
				case <-inputDone:
					return // stdin goroutine finished cleanly
				case <-ctx.Done():
					return // main context cancelled; main select handles the kill
				}
			}
		}()
	}

	// command stdout → session (raw bytes, no CRLF conversion)
	// Use RawWrite when available to bypass gliderlabs' \n→\r\n expansion,
	// which corrupts ZMODEM and other binary transfer protocol streams.
	//
	// adaptiveCopy starts with small chunks (4 KB) and ramps up to 8 KB for
	// throughput. If the stdin goroutine detects a ZRPOS (retransmission
	// request), it signals backoff and the chunk size is halved.
	go func() {
		dst := io.Writer(s)
		if rw, ok := s.(rawBinaryWriter); ok {
			log.Printf("DEBUG: (%s) using RawWrite for binary-safe output (type=%T)", cmd.Path, s)
			dst = writeFunc(rw.RawWrite)
		} else {
			log.Printf("WARN: (%s) rawBinaryWriter assertion FAILED (type=%T), falling back to session.Write", cmd.Path, s)
		}
		n, cpErr := adaptiveCopy(dst, stdoutPipe, &zrposBackoff)
		log.Printf("DEBUG: (%s) direct stdout copy finished. Bytes: %d, Error: %v", cmd.Path, n, cpErr)
		outputDone <- cpErr
	}()

	// Race: ctx cancellation vs normal completion (outputDone then cmd.Wait).
	//
	// Once outputDone fires (output copy ended — either because the process
	// exited or because the session write failed after a user abort), give the
	// process a short grace period to exit on its own, then force-kill it.
	// Without this, cmd.Wait() can block indefinitely when the user aborts the
	// transfer in their terminal (e.g. SyncTerm cancel button) without closing
	// the connection: outputDone fires immediately but the process is still
	// alive trying to flush its final ZModem frames through a now-dead pipe.
	const postOutputGrace = 5 * time.Second
	type cmdResult struct {
		waitErr error
		copyErr error
	}
	cmdDone := make(chan cmdResult, 1)
	go func() {
		copyErr := <-outputDone
		waitDone := make(chan error, 1)
		go func() { waitDone <- cmd.Wait() }()
		select {
		case err := <-waitDone:
			cmdDone <- cmdResult{waitErr: err, copyErr: copyErr}
		case <-time.After(postOutputGrace):
			log.Printf("DEBUG: (%s) process still running %v after output closed; killing", cmd.Path, postOutputGrace)
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			cmdDone <- cmdResult{waitErr: <-waitDone, copyErr: copyErr}
		}
	}()

	var cmdErr error
	select {
	case <-ctx.Done():
		log.Printf("DEBUG: (%s) transfer cancelled or timed out, killing process", cmd.Path)
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = stdinPipe.Close()
		// Close stdoutPipe to unblock the output goroutine so outputDone fires.
		_ = stdoutPipe.Close()
		select {
		case <-cmdDone:
		case <-time.After(5 * time.Second):
			log.Printf("WARN: (%s) timed out waiting for command goroutine after cancel", cmd.Path)
		}
		cmdErr = ctx.Err()
	case res := <-cmdDone:
		cmdErr = res.waitErr
		if cmdErr == nil && res.copyErr != nil {
			cmdErr = fmt.Errorf("stdout copy failed: %w", res.copyErr)
		}
	}
	log.Printf("DEBUG: (%s) command finished (direct). Error: %v", cmd.Path, cmdErr)

	// When the transfer ended abnormally (killed or non-zero exit), send a
	// ZModem abort sequence to the client. This is necessary because after
	// killing sexyz, any ZRINIT frame already buffered in the stdout pipe is
	// still flushed to the client by the output goroutine — the client
	// (SyncTerm) detects the ZRINIT and re-opens the upload/download dialog.
	// Sending 8× CAN tells the client to abort its ZModem session and return
	// to terminal mode.
	if cmdErr != nil {
		zmodemAbort := append(bytes.Repeat([]byte{0x18}, 8), '\r', '\n')
		_, _ = s.Write(zmodemAbort)
	}

	// Log stderr output from the external transfer program.
	if stderrBuf.Len() > 0 {
		for _, line := range strings.Split(strings.TrimSpace(stderrBuf.String()), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				log.Printf("INFO: [%s stderr] %s", filepath.Base(cmd.Path), line)
			}
		}
	}

	// Close stdin pipe so the next write from the stdin goroutine will fail.
	_ = stdinPipe.Close()

	// Unblock the stdin goroutine's pending s.Read() call.  SetReadInterrupt
	// causes Read to return io.EOF (telnet) or ErrReadInterrupted (SSH).
	if ri, ok := s.(readInterrupter); ok {
		interruptCh := make(chan struct{})
		close(interruptCh)
		ri.SetReadInterrupt(interruptCh)
	}

	// Wait briefly for the stdin goroutine to notice the closed pipe / interrupt.
	select {
	case <-inputDone:
		log.Printf("DEBUG: (%s) stdin goroutine finished cleanly", cmd.Path)
	case <-time.After(2 * time.Second):
		log.Printf("WARN: (%s) stdin goroutine did not finish within 2s, proceeding", cmd.Path)
	}

	// CRITICAL: Clear the read interrupt and reset the connection deadline
	// BEFORE returning.  The interrupt causes TelnetConn.Read() to return
	// io.EOF.  If the interrupt is not cleared, the next InputHandler that
	// calls s.Read() will immediately get io.EOF and report "user disconnected".
	if ri, ok := s.(readInterrupter); ok {
		ri.SetReadInterrupt(nil)
	}

	// Brief pause to let the client's terminal finish its post-transfer
	// cleanup (SyncTerm/ZModem end-of-transfer signaling).
	time.Sleep(250 * time.Millisecond)

	// Drain any leftover protocol bytes (ZModem ZFIN/OO, ACK frames) from
	// the session so they don't appear as garbage when the BBS resumes.
	drainSessionInput(s, 500*time.Millisecond)

	return cmdErr
}

// drainSessionInput reads and discards any pending bytes from the session
// for the given duration.  Uses a simple non-blocking polling loop that does
// NOT use SetReadInterrupt — avoiding goroutine races that can leave stale
// deadlines on the connection and cause spurious disconnects.
func drainSessionInput(s ssh.Session, duration time.Duration) {
	buf := make([]byte, 1024)
	totalDrained := 0
	end := time.Now().Add(duration)

	for time.Now().Before(end) {
		// Use a short-lived read interrupt for each poll cycle so we
		// don't block the entire duration if no data is available.
		if ri, ok := s.(readInterrupter); ok {
			ch := make(chan struct{})
			time.AfterFunc(50*time.Millisecond, func() { close(ch) })
			ri.SetReadInterrupt(ch)
		}

		n, readErr := s.Read(buf)
		totalDrained += n

		// Clear the interrupt immediately after each read attempt
		// so no stale deadline lingers.
		if ri, ok := s.(readInterrupter); ok {
			ri.SetReadInterrupt(nil)
		}

		if readErr != nil || n == 0 {
			// No more data or error — stop draining.
			break
		}
	}

	if totalDrained > 0 {
		log.Printf("DEBUG: Drained %d leftover bytes from session after transfer", totalDrained)
	}
}

// RunCommandWithPTY executes an external command attached to the user's SSH
// session using a PTY. It handles setting raw mode, resizing, and copying I/O.
//
// ctx controls cancellation and timeout: when ctx.Done() fires, the process is
// killed and the function returns ctx.Err(). Pass context.Background() for
// no timeout.
func RunCommandWithPTY(ctx context.Context, s ssh.Session, cmd *exec.Cmd, stdinIdleTimeout time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ptyReq, winCh, isPty := s.Pty()
	if !isPty {
		log.Printf("WARN: No PTY available for session. Falling back to direct mode for %s.", cmd.Path)
		return RunCommandDirect(ctx, s, cmd, stdinIdleTimeout)
	}

	log.Printf("DEBUG: Starting command '%s' with PTY", cmd.Path)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("failed to start pty for command '%s': %w", cmd.Path, err)
	}
	// ptmx is closed explicitly during shutdown sequence below

	// Handle window resizing.
	go func() {
		if ptyReq.Window.Width > 0 || ptyReq.Window.Height > 0 {
			wErr := pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(ptyReq.Window.Height), Cols: uint16(ptyReq.Window.Width)})
			if wErr != nil {
				log.Printf("WARN: Failed to set initial pty size: %v", wErr)
			}
		}
		for win := range winCh {
			wErr := pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(win.Height), Cols: uint16(win.Width)})
			if wErr != nil {
				log.Printf("WARN: Failed to resize pty: %v", wErr)
			}
		}
	}()

	// Set PTY to raw mode so binary protocol data passes through unmodified.
	fd := int(ptmx.Fd())
	var restoreTerminal func() = func() {}
	originalState, err := term.MakeRaw(fd)
	if err != nil {
		log.Printf("WARN: Failed to put PTY (fd: %d) into raw mode for command '%s': %v.", fd, cmd.Path, err)
	} else {
		restoreTerminal = func() {
			if err := term.Restore(fd, originalState); err != nil {
				log.Printf("ERROR: Failed to restore terminal state (fd: %d) after command '%s': %v", fd, cmd.Path, err)
			}
		}
	}

	// --- SetReadInterrupt for clean shutdown ---
	readInterrupt := make(chan struct{})
	hasInterrupt := false
	if ri, ok := s.(interface{ SetReadInterrupt(<-chan struct{}) }); ok {
		ri.SetReadInterrupt(readInterrupt)
		defer ri.SetReadInterrupt(nil)
		hasInterrupt = true
	}

	// --- I/O Copying ---
	inputDone := make(chan struct{})
	outputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		log.Printf("DEBUG: (%s) Goroutine: Copying session stdin -> PTY starting...", cmd.Path)
		n, err := io.Copy(ptmx, s)
		log.Printf("DEBUG: (%s) Goroutine: Copying session stdin -> PTY finished. Bytes: %d, Error: %v", cmd.Path, n, err)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) && !errors.Is(err, syscall.EIO) && !errors.Is(err, syscall.EINTR) {
			log.Printf("WARN: (%s) Error copying session stdin to PTY: %v", cmd.Path, err)
		}
	}()
	go func() {
		defer close(outputDone)
		log.Printf("DEBUG: (%s) Goroutine: Copying PTY stdout -> session starting...", cmd.Path)
		n, err := io.Copy(s, ptmx)
		log.Printf("DEBUG: (%s) Goroutine: Copying PTY stdout -> session finished. Bytes: %d, Error: %v", cmd.Path, n, err)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) && !errors.Is(err, syscall.EIO) {
			log.Printf("WARN: (%s) Error copying PTY stdout to session stdout: %v", cmd.Path, err)
		}
	}()

	// Race: ctx cancellation vs normal completion
	log.Printf("DEBUG: (%s) Waiting for command completion...", cmd.Path)
	cmdDoneCh := make(chan error, 1)
	go func() { cmdDoneCh <- cmd.Wait() }()

	var cmdErr error
	select {
	case <-ctx.Done():
		log.Printf("DEBUG: (%s) transfer cancelled or timed out, killing process", cmd.Path)
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		cmdErr = ctx.Err()
		<-cmdDoneCh
	case cmdErr = <-cmdDoneCh:
	}
	log.Printf("DEBUG: (%s) Command finished. Error: %v", cmd.Path, cmdErr)

	// Interrupt the input goroutine's blocked Read() so it exits without
	// consuming the user's next keypress
	close(readInterrupt)
	if hasInterrupt {
		<-inputDone
	}

	// Restore terminal before closing PTY
	restoreTerminal()

	// Close PTY and wait for both goroutines
	_ = ptmx.Close()
	if !hasInterrupt {
		<-inputDone // Wait for input goroutine even without interrupt support
	}
	<-outputDone

	return cmdErr
}

// ExecuteZmodemSend initiates a Zmodem send (sz) of one or more files using a PTY.
// It requires the 'sz' command to be available on the system path.
// filePaths should be absolute paths to the files being sent.
// ctx controls cancellation and timeout; pass nil for no timeout.
func ExecuteZmodemSend(ctx context.Context, s ssh.Session, filePaths ...string) error {
	log.Printf("DEBUG: executeZmodemSend called with files: %v", filePaths)

	if len(filePaths) == 0 {
		return fmt.Errorf("no files provided for Zmodem send")
	}

	// Check if sz command exists
	szPath, err := exec.LookPath("sz")
	if err != nil {
		log.Printf("ERROR: 'sz' command not found in PATH: %v", err)
		return fmt.Errorf("'sz' command not found, Zmodem send unavailable")
	}
	log.Printf("DEBUG: Found 'sz' command at: %s", szPath)

	// Construct command: sz [-b] <files...>
	args := append([]string{"-b"}, filePaths...)
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, szPath, args...)

	log.Printf("INFO: Executing Zmodem send: %s %v", szPath, args)
	// Execute using the PTY helper
	err = RunCommandWithPTY(ctx, s, cmd, 0)
	if err != nil {
		log.Printf("ERROR: Zmodem send command ('%s') failed: %v", szPath, err)
		return fmt.Errorf("Zmodem send failed: %w", err)
	}

	log.Printf("INFO: Zmodem send completed successfully for files: %v", filePaths)
	return nil
}

// ExecuteZmodemReceive initiates a Zmodem receive (rz) into a specified directory using a PTY.
// It requires the 'rz' command to be available on the system path.
// targetDir should be the absolute path to the directory where received files will be stored.
// ctx controls cancellation and timeout; pass nil for no timeout.
func ExecuteZmodemReceive(ctx context.Context, s ssh.Session, targetDir string) error {
	log.Printf("DEBUG: executeZmodemReceive called for target directory: %s", targetDir)

	// 1. Validate and ensure target directory exists
	if targetDir == "" {
		return fmt.Errorf("target directory cannot be empty for Zmodem receive")
	}
	absTargetDir, err := filepath.Abs(targetDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for target directory '%s': %w", targetDir, err)
	}
	if err := os.MkdirAll(absTargetDir, 0755); err != nil {
		if fileInfo, statErr := os.Stat(absTargetDir); !(statErr == nil && fileInfo.IsDir()) {
			return fmt.Errorf("failed to create or access target directory '%s': %w", absTargetDir, err)
		}
	}

	// 2. Check if rz command exists
	rzPath, err := exec.LookPath("rz")
	if err != nil {
		log.Printf("ERROR: 'rz' command not found in PATH: %v", err)
		return fmt.Errorf("'rz' command not found, Zmodem receive unavailable")
	}
	log.Printf("DEBUG: Found 'rz' command at: %s", rzPath)

	// 3. Construct command: rz -b -r
	args := []string{"-b", "-r"} // Binary mode, Restricted mode (prevents path traversal)
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, rzPath, args...)
	cmd.Dir = absTargetDir // Run rz in the target directory

	log.Printf("INFO: Executing Zmodem receive in directory '%s': %s %v", absTargetDir, rzPath, args)
	// 4. Execute using the PTY helper
	err = RunCommandWithPTY(ctx, s, cmd, 0)
	if err != nil {
		log.Printf("ERROR: Zmodem receive command ('%s') failed: %v", rzPath, err)
		return fmt.Errorf("Zmodem receive failed: %w", err)
	}

	log.Printf("INFO: Zmodem receive completed successfully into directory: %s", absTargetDir)
	return nil
}
