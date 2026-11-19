// Package scripting runs Vision/3 (V3) scripts: JavaScript programs, executed
// in the embedded goja interpreter, that act as doors inside a caller's
// session. NewEngine builds a per-session Engine and exposes the BBS to the
// script through a global v3 object (console I/O, ANSI display, session and
// user details, message and file areas, node list, sandboxed file access and
// persistent data); Engine.Run executes a script file. The menu package's
// V3 script door handler is the caller.
package scripting

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/ViSiON-3/vision-3-bbs/internal/safego"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/dop251/goja"
)

// Engine is the Vision/3 scripting runtime for a single BBS session.
// Each user running a V3 script gets their own Engine instance with an
// isolated goja.Runtime, session I/O, and sandboxed file access.
type Engine struct {
	vm        *goja.Runtime
	session   *SessionContext
	cfg       ScriptConfig
	providers *Providers
	ctx       context.Context
	cancel    context.CancelFunc

	// Borrowed input is requested on demand; normal doors read ahead.
	// inputBuf holds parsed leftovers.
	inputBuf []byte
	// utf8Pending holds the leading bytes of a UTF-8 character whose
	// remaining bytes have not arrived yet (UTF-8 sessions only).
	utf8Pending  []byte
	rawInputCh   chan readResult
	readRequests chan struct{}
	readPending  bool
	readerOnce   sync.Once
	copierDone   chan struct{} // closed when the copier goroutine exits
	// stopCopier, closed by Close, tells the read-ahead copier to stop
	// offering input. A chunk it has read but can no longer deliver is left
	// in heldInput, which only Close reads, after copierDone.
	stopCopier chan struct{}
	heldInput  []byte
	closeOnce  sync.Once
	// crTrailer is set when the last key read was a CR that ended its
	// chunk: Enter sent as CR LF or CR NUL can be split across reads, and
	// the LF or NUL at the start of the next chunk is part of that Enter,
	// not a key of its own.
	crTrailer bool
	// frameSeq counts the paced frames sent; frameSent holds, per canvas,
	// the number of the last it sent, frameSkipped the count when it last
	// skipped one, and frameYielded one more than the count when it last
	// yielded (see beginFrame).
	frameSeq     int
	frameSent    map[int]int
	frameSkipped map[int]int
	frameYielded map[int]int

	// answer and answered record v3.prompt.answer (see prompt.go).
	answer   any
	answered bool
	// keyName is the name of the last special key parseInput consumed
	// ("UP", "PGDN", ...; see keys.go), or "" when it consumed none.
	// readNamedKey clears it before each read.
	keyName string
	// holdEscape, set while readNamedKey reads, makes parseInput return
	// an unfinished escape sequence's ESC on its own and keep the rest,
	// so readNamedKey can wait for the remaining bytes.
	holdEscape bool
	// inLine is set while readLine reads, which ScriptConfig.Notice does
	// not stop.
	inLine bool
	// noticeDue is set when ScriptConfig.Notice fired but input already
	// read in went to the script first; the next key wait stops it.
	noticeDue bool
	// afterKey is set while the last key wait ended on input rather than
	// on its timeout; a notice holds off a timed wait then (readKeyEcho).
	afterKey bool
	// noticeHeld is when a notice was first held off for a wait after a
	// key; it is held no longer than noticeKeyHold from then.
	noticeHeld time.Time
}

// noticeKeyHold is the longest a notice is held off for timed key waits that
// follow keys. It is longer than the half second the board's menu scripts
// wait for the second key of a command, and short enough that a toast is
// not noticeably late.
const noticeKeyHold = time.Second

// readResult carries data or an error from the reader goroutine.
type readResult struct {
	data []byte
	err  error
}

// defaultMaxRunTime is the wall-clock limit for a script whose
// ScriptConfig.MaxRunTime is zero.
const defaultMaxRunTime = 30 * time.Minute

// NewEngine creates a new V3 scripting engine for the given session and
// registers the v3 API namespaces in a fresh goja runtime. The engine's
// context is ctx limited to cfg.MaxRunTime (30 minutes when zero, no limit
// when negative); when it ends, running JavaScript is interrupted. providers may be nil for
// console-only scripts; namespaces whose backing manager is nil (user,
// message, file, nodes) are simply not registered. The caller must Close the
// engine when the script finishes.
func NewEngine(ctx context.Context, session *SessionContext, cfg ScriptConfig, providers *Providers) *Engine {
	var cancel context.CancelFunc
	switch {
	case cfg.MaxRunTime < 0:
		ctx, cancel = context.WithCancel(ctx)
	case cfg.MaxRunTime == 0:
		ctx, cancel = context.WithTimeout(ctx, defaultMaxRunTime)
	default:
		ctx, cancel = context.WithTimeout(ctx, cfg.MaxRunTime)
	}
	if providers == nil {
		providers = &Providers{}
	}
	eng := &Engine{
		vm:        goja.New(),
		session:   session,
		cfg:       cfg,
		providers: providers,
		ctx:       ctx,
		cancel:    cancel,
	}

	// Halt JS execution on context cancellation.
	// Without its watcher nothing would stop a busy script at its run time
	// limit or a hang-up, so a watcher that panicked stops the script now.
	safego.Go(safego.KindScriptWatch, session.NodeNumber, eng.watchContext, func() {
		eng.cancel()
		eng.vm.Interrupt(ErrTerminated)
	})

	// Register V3 API namespaces.
	v3 := eng.vm.NewObject()
	registerConsole(v3, eng)
	registerSession(v3, eng)
	registerPrompt(v3, eng)
	if providers.Menu != nil {
		registerMenu(v3, eng)
	}
	registerGfx(v3, eng)
	if providers.UserMgr != nil && providers.CurrentUser != nil {
		registerUser(v3, eng)
		registerUsers(v3, eng)
	}
	if providers.MessageMgr != nil {
		registerMessage(v3, eng)
	}
	if providers.FileMgr != nil {
		registerFile(v3, eng)
	}
	if providers.Oneliners != nil {
		registerOneliners(v3, eng)
	}
	registerData(v3, eng)
	registerAnsi(v3, eng)
	registerUtil(v3, eng)
	registerFS(v3, eng)
	if providers.SessionRegistry != nil {
		registerNodes(v3, eng)
	}
	jsutil.Set(eng.vm, "v3", v3)

	// Register top-level helpers.
	eng.registerGlobals()

	return eng
}

// Run loads and executes the main script file.
func (eng *Engine) Run(scriptPath string) error {
	if !filepath.IsAbs(scriptPath) {
		scriptPath = filepath.Join(eng.cfg.WorkingDir, scriptPath)
	}

	data, err := os.ReadFile(scriptPath)
	if err != nil {
		return fmt.Errorf("reading script %s: %w", scriptPath, err)
	}

	slog.Info("running V3 script", "path", scriptPath, "node", eng.session.NodeNumber)

	err = eng.runScript(scriptPath, string(data))
	if errors.Is(err, ErrHostPanic) {
		eng.restoreTerminal()
		return fmt.Errorf("script error: %w", err)
	}
	if err != nil {
		if code, ok := exitStatus(err); ok {
			if code != 0 {
				slog.Info("V3 script exited with non-zero code",
					"path", scriptPath, "node", eng.session.NodeNumber, "code", code)
			}
			return nil
		}
		// A script stopped for a notice as its run time ran out, or the
		// caller hung up, ends for that instead.
		if eng.ctx.Err() != nil {
			if eng.ctx.Err() == context.DeadlineExceeded {
				return ErrTimeout
			}
			return ErrDisconnect
		}
		if stoppedForNotice(err) {
			return ErrNotice
		}
		return fmt.Errorf("script error: %w", err)
	}
	return nil
}

// runScript runs src, turning a Go panic in a host function the script
// called into an ErrHostPanic error. goja passes such a panic straight
// through RunScript, and on a caller's session goroutine it would end the
// session, or with no recovery above it the board. The panic is logged with
// its stack.
func (eng *Engine) runScript(path, src string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("V3 script host function panicked",
				"path", path, "node", eng.session.NodeNumber, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			err = fmt.Errorf("%w: %v", ErrHostPanic, r)
		}
	}()
	_, err = eng.vm.RunScript(path, src)
	return err
}

// restoreTerminal resets colours and shows the cursor, which a script whose
// host function panicked may have left changed for whatever comes next.
func (eng *Engine) restoreTerminal() {
	eng.writeRaw("\x1b[0m\x1b[?25h")
}

// Close stops the engine's I/O goroutines and returns the input the script
// read from the session but never consumed, oldest first: keys the caller
// typed ahead for whatever follows the script. The caller should hand them to
// the session's next reader so they are not lost.
//
// When the session takes input on demand and can take it back (ReturnInput,
// as a borrowed InputHandler does), Close returns the bytes to it itself and
// returns nil.
//
// The copier goroutine stops only once its blocked session Read returns, so
// the caller must close the session's read interrupt (SetReadInterrupt)
// before calling Close. A session without one cannot be stopped: Close gives
// up waiting after two seconds, and a key the copier reads after that is
// lost. Close may be called more than once; later calls return nil.
func (eng *Engine) Close() []byte {
	var unread []byte
	eng.closeOnce.Do(func() { unread = eng.close() })
	return unread
}

// close does the work of Close, once.
func (eng *Engine) close() []byte {
	defer eng.cancel()
	if eng.readRequests != nil {
		eng.cancel() // stops the demand reader between requests
	}
	if eng.stopCopier != nil {
		close(eng.stopCopier)
	}
	// Wait for the copier goroutine to exit so it stops reading from the
	// session, and so everything it read is in rawInputCh or heldInput.
	stopped := false
	if eng.copierDone != nil {
		select {
		case <-eng.copierDone:
			stopped = true
		case <-time.After(2 * time.Second):
			slog.Warn("V3 script copier goroutine did not exit within 2s; proceeding with cleanup")
		}
	}

	// Everything the script did not consume, oldest first: the start of a
	// partial UTF-8 character, the rest of the last chunk it parsed, the
	// chunks still queued, then the one the copier held when it stopped.
	unread := append(append([]byte(nil), eng.utf8Pending...), eng.inputBuf...)
	eng.inputBuf, eng.utf8Pending = nil, nil
	if eng.rawInputCh != nil {
	drain:
		for {
			select {
			case result := <-eng.rawInputCh:
				unread = append(unread, result.data...)
			default:
				break drain
			}
		}
	}
	if stopped {
		unread = append(unread, eng.heldInput...)
		eng.heldInput = nil
	}
	// The rest of an Enter the script has already read is not the next
	// prompt's key.
	if eng.crTrailer && len(unread) > 0 && (unread[0] == '\n' || unread[0] == 0x00) {
		unread = unread[1:]
		eng.crTrailer = false
	}
	if len(unread) > 0 {
		eng.crTrailer = false
	}
	if len(unread) == 0 {
		// An Enter whose LF or NUL has not arrived yet is the next
		// reader's to finish. A borrowed handler is told here; otherwise
		// the caller asks EnterTrailerPending.
		if eng.crTrailer && eng.readRequests != nil && stopped {
			if owner, ok := eng.session.Session.(interface{ ExpectEnterTrailer() }); ok {
				owner.ExpectEnterTrailer()
				eng.crTrailer = false
			}
		}
		return nil
	}

	// A getkey timeout can leave a requested read in flight. Once it has
	// stopped, a borrowed handler takes its input back before its owner
	// resumes; while it may still be reading, the handler is not ours to use.
	if eng.readRequests != nil {
		if owner, ok := eng.session.Session.(interface{ ReturnInput([]byte) }); ok {
			if stopped {
				owner.ReturnInput(unread)
			}
			return nil
		}
	}
	return unread
}

// ExpectEnterTrailer tells the engine that the session's previous reader
// took the CR of an Enter whose LF or NUL had not arrived, so a leading LF
// or NUL in the first input the engine reads belongs to that Enter and is
// dropped. Call it before Run.
func (eng *Engine) ExpectEnterTrailer() { eng.crTrailer = true }

// EnterTrailerPending reports, after Close, whether the script took the CR
// of an Enter whose LF or NUL had not arrived when it ended, and no borrowed
// handler was told. The session's next reader should then drop a leading LF
// or NUL.
func (eng *Engine) EnterTrailerPending() bool { return eng.crTrailer }

// watchContext monitors the context and interrupts the JS runtime on cancellation.
func (eng *Engine) watchContext() {
	safego.TestPoint(safego.KindScriptWatch)
	<-eng.ctx.Done()
	eng.vm.Interrupt(ErrTerminated)
}

// registerGlobals adds top-level convenience functions (exit, sleep, etc.).
func (eng *Engine) registerGlobals() {
	jsutil.Set(eng.vm, "exit", func(call goja.FunctionCall) goja.Value {
		code := 0
		if len(call.Arguments) > 0 {
			code = int(call.Arguments[0].ToInteger())
		}
		// Interrupt rather than throw: goja delivers an interrupt as an
		// uncatchable *goja.InterruptedError at the next instruction
		// boundary, so try/catch cannot swallow exit() and Run can tell a
		// clean exit apart from a script error. Returning undefined lets the
		// runtime reach that boundary.
		eng.vm.Interrupt(exitCode{code: code})
		return goja.Undefined()
	})
}

// --- I/O helpers ---

// writeRaw writes script text to the session in the session's encoding
// (terminalio.WriteProcessedBytes). A UTF-8 session gets the UTF-8 bytes. A
// CP437 session gets each character's CP437 byte (½ U+00BD -> 0xAB), and a
// character CP437 lacks becomes '?' on its own without affecting the rest.
// ANSI escape sequences pass through untouched in both.
func (eng *Engine) writeRaw(s string) {
	if s == "" {
		return
	}
	if err := terminalio.WriteProcessedBytes(eng.session.Session, []byte(s), eng.session.OutputMode); err != nil {
		eng.cancel()
	}
}

// writeBytes writes bytes to the session without any encoding conversion.
// The caller has already put them in the session's encoding.
func (eng *Engine) writeBytes(b []byte) {
	if len(b) == 0 {
		return
	}
	if _, err := eng.session.Session.Write(b); err != nil {
		eng.cancel()
	}
}

// startReader starts the goroutine that feeds the engine's input channel from
// the session. It reads ahead of the script, up to the channel's capacity;
// Close returns whatever the script has not consumed by then.
func (eng *Engine) startReader() {
	if source, ok := eng.session.Session.(interface{ InputOnDemand() bool }); ok && source.InputOnDemand() {
		eng.startDemandReader()
		return
	}

	eng.readerOnce.Do(func() {
		eng.rawInputCh = make(chan readResult, 4)
		eng.copierDone = make(chan struct{})
		eng.stopCopier = make(chan struct{})

		// Copier: session -> channel. Each chunk it reads is either
		// delivered to rawInputCh or, once Close has asked it to stop, left
		// in heldInput, so no key it took from the session is dropped.
		// offer reports whether the copier may go on.
		offer := func(r readResult) bool {
			select {
			case eng.rawInputCh <- r:
				return true
			case <-eng.stopCopier:
				eng.heldInput = r.data
				return false
			}
		}
		safego.Go(safego.KindScriptInput, eng.session.NodeNumber, func() {
			defer close(eng.copierDone)
			safego.TestPoint(safego.KindScriptInput)
			for {
				data, err := eng.readSession()
				// A Read can return bytes with its error. The bytes go first,
				// on their own, so readKeyEcho, which stops at an error,
				// still sees them, and Close can return them if it does not.
				if len(data) > 0 && !offer(readResult{data: data}) {
					return
				}
				if err != nil {
					if !offer(readResult{err: err}) {
						return
					}
					eng.cancel() // signal context so CPU-bound scripts stop on disconnect
					return
				}
			}
		}, eng.cancel)
	})
}

// readSession reads the session once, without the terminal's replies
// (SessionContext.StripReplies). What a read holds may be all replies, so
// the bytes returned can be none without an error.
func (eng *Engine) readSession() ([]byte, error) {
	buf := make([]byte, 256)
	n, err := eng.session.Session.Read(buf)
	data := buf[:n]
	if strip := eng.session.StripReplies; strip != nil && n > 0 {
		data = strip(data)
	}
	return data, err
}

// maxTakenInput bounds the keys takeInput holds for a script that does not
// read them. Keys past it are dropped: the reader goes on reading, so the
// terminal's answers are still taken.
const maxTakenInput = 4096

// takeInput moves what the session reader has read, without waiting, into
// inputBuf, where the script's next key reads find it. A script that draws
// paced frames (SessionContext.Frames) without reading keys calls it so the
// reader goes on reading the session, and so taking the terminal's answers.
func (eng *Engine) takeInput() {
	eng.startReader()
	if eng.readRequests != nil && !eng.readPending {
		eng.readRequests <- struct{}{}
		eng.readPending = true
	}
	for {
		select {
		case r := <-eng.rawInputCh:
			eng.readPending = false
			data := r.data
			// The LF or NUL of an Enter split across two reads, as
			// readKeyEcho drops it.
			if eng.crTrailer && len(eng.inputBuf) == 0 && len(data) > 0 {
				eng.crTrailer = false
				if data[0] == '\n' || data[0] == 0x00 {
					data = data[1:]
				}
			}
			eng.inputBuf = append(eng.inputBuf, data[:min(len(data), max(0, maxTakenInput-len(eng.inputBuf)))]...)
			if r.err != nil {
				eng.cancel()
				return
			}
			if eng.readRequests != nil && !eng.readPending {
				eng.readRequests <- struct{}{}
				eng.readPending = true
			}
		default:
			eng.takeSplitReplies()
			return
		}
	}
}

// answerDue reports whether a paced frame's answer is due and replies
// split across reads can be taken (SessionContext.TakeReply).
func (eng *Engine) answerDue() bool {
	return eng.session != nil && eng.session.TakeReply != nil && eng.session.Frames.Waiting()
}

// takeReply passes seq, a whole CSI sequence, to SessionContext.TakeReply
// if it has a terminal reply's shape: one split after its ESC passed the
// trap in pieces.
func (eng *Engine) takeReply(seq []byte) bool {
	return eng.session != nil && eng.session.TakeReply != nil && len(seq) > 2 &&
		editor.IsReplyCSI(seq[2:]) && eng.session.TakeReply(seq)
}

// takeSplitReplies takes the whole terminal replies out of inputBuf (see
// takeReply), keeping the other bytes in order.
func (eng *Engine) takeSplitReplies() {
	buf := eng.inputBuf
	out := buf[:0:0]
	for i := 0; i < len(buf); {
		if buf[i] == 0x1b && i+2 < len(buf) && buf[i+1] == '[' {
			end := i + skipCSI(buf[i:])
			if c := buf[end-1]; c >= 0x40 && c <= 0x7e && eng.takeReply(buf[i:end]) {
				i = end
				continue
			}
		}
		out = append(out, buf[i])
		i++
	}
	eng.inputBuf = out
}

// startDemandReader starts a demand-driven reader. It never takes another key until
// readKeyEcho asks for one, so borrowed session input stays with its owner.
func (eng *Engine) startDemandReader() {
	eng.readerOnce.Do(func() {
		eng.rawInputCh = make(chan readResult, 1)
		eng.readRequests = make(chan struct{}, 1)
		eng.copierDone = make(chan struct{})
		safego.Go(safego.KindScriptInput, eng.session.NodeNumber, func() {
			defer close(eng.copierDone)
			safego.TestPoint(safego.KindScriptInput)
			for {
				select {
				case <-eng.ctx.Done():
					return
				case <-eng.readRequests:
				}
				var data []byte
				var err error
				for len(data) == 0 && err == nil && eng.ctx.Err() == nil {
					data, err = eng.readSession()
				}
				select {
				case eng.rawInputCh <- readResult{data: data, err: err}:
				case <-eng.ctx.Done():
					// Keep the result available for Close to return to its owner.
					eng.rawInputCh <- readResult{data: data, err: err}
					return
				}
				if err != nil {
					return
				}
			}
		}, eng.cancel)
	})
}

// readKey reads a single key from the session with optional timeout.
// Timeout of 0 means block indefinitely. See readKeyEcho for how keys are
// decoded.
//
// While a paced frame's answer is due, an escape sequence a read ends
// partway through may be that answer, split across reads (parseInput
// returns its ESC alone): the rest is gathered as readkey gathers a
// sequence (completeEscape). A whole reply goes to the trap (takeReply)
// and is not a key; any other whole sequence is read as if it had come in
// one read; an unfinished one is the Esc key, and what was gathered is
// read next.
func (eng *Engine) readKey(timeout time.Duration) (string, error) {
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	for {
		key, _, err := eng.readKeyEcho(timeout)
		if err != nil || key != "\x1b" || !eng.answerDue() {
			return key, err
		}
		seq, rest, err := eng.completeEscape(deadline)
		if err != nil {
			return "", err
		}
		if escapePending(seq) {
			eng.inputBuf = append(append(append([]byte(nil), seq[1:]...), rest...), eng.inputBuf...)
			return key, nil
		}
		if eng.takeReply(seq) {
			eng.inputBuf = append(rest, eng.inputBuf...)
		} else {
			eng.inputBuf = append(append(seq, rest...), eng.inputBuf...)
		}
		if !deadline.IsZero() {
			if timeout = time.Until(deadline); timeout <= 0 {
				return "", nil
			}
		}
	}
}

// readKeyEcho reads a single key and also returns the bytes that echo it in
// the session's encoding. Timeout of 0 means block indefinitely.
//
// V3 scripts work with Unicode strings, so a typed character >= 0x80 is
// decoded per the session's output mode (ansi.DecodeExtendedKey): on a CP437
// session its byte maps through ansi.Cp437ToUnicode and the echo is that same
// byte; on a UTF-8 session the bytes of one character are assembled, across
// several reads if need be, and the echo is the UTF-8 sequence. Malformed
// UTF-8 and unmapped bytes are dropped. Every other key is returned as
// parseInput yields it ("" for a discarded escape sequence) and echoes as
// itself. Buffered leftovers go through parseInput too, so an escape sequence
// or CR LF arriving in the same read as earlier keys is still one key.
func (eng *Engine) readKeyEcho(timeout time.Duration) (string, []byte, error) {
	var timer <-chan time.Time
	if timeout > 0 {
		timer = time.After(timeout)
	}
	var holdTimer *time.Timer
	defer func() {
		if holdTimer != nil {
			holdTimer.Stop()
		}
	}()

	for {
		var data []byte
		if len(eng.inputBuf) > 0 {
			data, eng.inputBuf = eng.inputBuf, nil
		} else {
			eng.startReader()
			if eng.readRequests != nil && !eng.readPending {
				eng.readRequests <- struct{}{}
				eng.readPending = true
			}
			// stopForNotice stops the script for a notice that has fired,
			// unless input already read in is waiting: that goes to the
			// script first. The interrupt stops the script at its next
			// instruction, which try/catch cannot catch; a key read after
			// this is returned by Close.
			var result readResult
			stopForNotice := func() bool {
				select {
				case result = <-eng.rawInputCh:
					// The notice stays due for the next wait.
					eng.noticeDue = true
					return false
				default:
					eng.vm.Interrupt(noticeStop{})
					return true
				}
			}
			// A timed wait that follows a key may be a script waiting for
			// the rest of a command (the W of /W). A notice does not stop
			// it there, which would lose the key the script holds, but
			// waits for the wait to end, for no longer than noticeKeyHold.
			notice := eng.notice()
			var hold <-chan time.Time
			holdNotice := timeout > 0 && eng.afterKey
		wait:
			for {
				select {
				case result = <-eng.rawInputCh:
					break wait
				case <-timer:
					eng.afterKey = false
					// A notice that fired as the time ran out still stops it.
					select {
					case <-eng.notice():
						if stopForNotice() {
							return "", nil, ErrNotice
						}
						break wait
					default:
						return "", nil, nil
					}
				case <-notice:
					if holdNotice {
						// A notice sent once, not closed, stays due for
						// the waits that follow.
						eng.noticeDue = true
						if eng.noticeHeld.IsZero() {
							eng.noticeHeld = time.Now()
						}
						if left := noticeKeyHold - time.Since(eng.noticeHeld); left > 0 {
							if holdTimer != nil {
								holdTimer.Stop() // one from an earlier read of this key
							}
							holdTimer = time.NewTimer(left)
							notice, hold = nil, holdTimer.C
							continue
						}
					}
					if stopForNotice() {
						return "", nil, ErrNotice
					}
					break wait
				case <-hold:
					if stopForNotice() {
						return "", nil, ErrNotice
					}
					break wait
				case <-eng.ctx.Done():
					return "", nil, ErrTerminated
				}
			}
			eng.readPending = false
			if result.err != nil {
				eng.cancel()
				return "", nil, ErrDisconnect
			}
			if len(result.data) == 0 {
				return "", nil, nil
			}
			data = result.data
			// The LF or NUL of an Enter split across two reads.
			if eng.crTrailer {
				eng.crTrailer = false
				if data[0] == '\n' || data[0] == 0x00 {
					if data = data[1:]; len(data) == 0 {
						continue
					}
				}
			}
		}

		eng.afterKey = true
		key := eng.parseInput(data)
		if len(key) != 1 || key[0] < 0x80 {
			// Any other key abandons a partial UTF-8 character, so its
			// stray lead bytes cannot absorb the next character's bytes.
			eng.utf8Pending = nil
			return key, []byte(key), nil
		}
		var char, echo []byte
		char, echo, eng.utf8Pending = ansi.DecodeExtendedKey(nil, eng.session.OutputMode, key[0], eng.utf8Pending)
		if len(char) > 0 {
			return string(char), echo, nil
		}
		// An incomplete UTF-8 sequence or a dropped byte: keep reading.
	}
}

// readLine reads a line of input with echo and basic editing. maxLen counts
// characters, not bytes, and backspace removes one whole character.
func (eng *Engine) readLine(maxLen int, opts lineOpts) (string, error) {
	eng.inLine = true
	defer func() { eng.inLine = false }()
	var buf []byte
	for {
		key, echo, err := eng.readKeyEcho(0)
		if err != nil {
			return string(buf), err
		}
		if len(key) == 0 {
			continue
		}

		ch := key[0]
		switch ch {
		case '\r', '\n':
			eng.writeRaw("\r\n")
			result := string(buf)
			if opts.upper {
				result = toUpperASCII(result)
			}
			return result, nil
		case '\x08', '\x7f': // Backspace, DEL
			if len(buf) > 0 {
				buf = ansi.BackspaceRune(buf)
				if !opts.noEcho {
					eng.writeRaw("\x08 \x08")
				}
			}
		case '\x1b': // ESC — abort
			return "", nil
		default:
			if opts.numberOnly && (ch < '0' || ch > '9') {
				continue
			}
			if utf8.RuneCount(buf) >= maxLen {
				continue
			}
			if ch >= 0x80 {
				// A decoded non-ASCII character: store it as UTF-8 and
				// echo the bytes the caller typed. They are already in the
				// session's encoding, so there is nothing to gain from
				// re-encoding key through writeRaw.
				buf = append(buf, key...)
				if !opts.noEcho {
					eng.writeBytes(echo)
				}
				continue
			}
			if opts.upper && ch >= 'a' && ch <= 'z' {
				ch = ch - 32
			}
			buf = append(buf, ch)
			if !opts.noEcho {
				eng.writeRaw(string(ch))
			}
		}
	}
}

// lineOpts controls readLine behavior.
type lineOpts struct {
	noEcho     bool
	upper      bool
	numberOnly bool
}

// parseInput translates raw input bytes (potentially ANSI escape sequences) to key strings.
func (eng *Engine) parseInput(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	consumed := 1
	// The raw byte, not string(data[0]), which would convert the byte to a
	// rune (0xA9 -> "©", C2 A9). readKeyEcho decodes bytes >= 0x80.
	result := string(data[:1])

	if data[0] == 0x1b && (eng.holdEscape || eng.answerDue()) && escapePending(data) {
		// readkey, or getkey while a frame's answer is due (see readKey):
		// the ESC alone, the rest gathered by the caller
		consumed = 1
	} else if data[0] == 0x1b && len(data) > 1 {
		switch {
		case data[1] == '[' && len(data) > 2:
			// CSI sequences (arrow keys etc.): getkey discards them,
			// readkey reports their name
			consumed = skipCSI(data)
			eng.keyName = csiKeyName(data[:consumed])
			result = ""
			// A terminal reply split after ESC or ESC [ passed the trap
			// in pieces; whole again here, it goes to the trap.
			eng.takeReply(data[:consumed])
		case data[1] == 'O' && len(data) > 2:
			// SS3: arrows in application cursor mode, and F1-F4
			consumed = 3
			eng.keyName = ss3KeyName(data[2])
			result = ""
		case eng.holdEscape:
			// readkey: ESC and another key at once is Esc, then that
			// key, as the editor reads it (the key is put back)
			eng.keyName = "ESC"
			result = ""
		default:
			// getkey: ESC and another key at once is Alt plus that key,
			// and both go
			consumed = 2
			result = ""
		}
	} else if data[0] == '\r' {
		if len(data) > 1 && (data[1] == '\n' || data[1] == 0x00) {
			consumed = 2 // Enter as CR LF or CR NUL
		}
		// A CR that ends the chunk may have its LF or NUL in the next one.
		eng.crTrailer = len(data) == 1
		result = "\r"
	} else if data[0] == '\n' {
		result = "\r"
	}

	if consumed < len(data) {
		eng.inputBuf = append(eng.inputBuf, data[consumed:]...)
	}
	return result
}

// skipCSI finds the end of a CSI escape sequence starting at data[0]=ESC.
func skipCSI(data []byte) int {
	for i := 2; i < len(data); i++ {
		if data[i] >= 0x40 && data[i] <= 0x7E {
			return i + 1
		}
	}
	return len(data)
}

// notice is the channel a key wait selects on for ScriptConfig.Notice, or
// nil while a line is read.
func (eng *Engine) notice() <-chan struct{} {
	if eng.inLine {
		return nil
	}
	if eng.noticeDue {
		return noticeFired
	}
	return eng.cfg.Notice
}

// noticeFired is a channel that is always ready, for a notice that fired
// earlier (Engine.noticeDue).
var noticeFired = func() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}()

// stoppedForNotice reports whether err is the interrupt readKeyEcho raises
// when ScriptConfig.Notice fires.
func stoppedForNotice(err error) bool {
	var ie *goja.InterruptedError
	if !errors.As(err, &ie) {
		return false
	}
	_, ok := ie.Value().(noticeStop)
	return ok
}

// exitStatus reports whether err is the interrupt raised by a clean exit()
// call, and if so the exit code the script passed. Other interrupts (context
// timeout, disconnect) are not clean exits.
func exitStatus(err error) (int, bool) {
	var ie *goja.InterruptedError
	if !errors.As(err, &ie) {
		return 0, false
	}
	ec, ok := ie.Value().(exitCode)
	if !ok {
		return 0, false
	}
	return ec.code, true
}

func toUpperASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}
