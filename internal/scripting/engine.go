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
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/dop251/goja"
	"golang.org/x/text/encoding/charmap"
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

	// Input: interposed pipe reader feeds rawInputCh; inputBuf holds parsed leftovers.
	inputBuf []byte
	// utf8Pending holds the leading bytes of a UTF-8 character whose
	// remaining bytes have not arrived yet (UTF-8 sessions only).
	utf8Pending []byte
	rawInputCh  chan readResult
	pipeReader  *io.PipeReader
	pipeWriter  *io.PipeWriter
	readerOnce  sync.Once
	copierDone  chan struct{} // closed when the copier goroutine exits
}

// readResult carries data or an error from the reader goroutine.
type readResult struct {
	data []byte
	err  error
}

// defaultMaxRunTime is the maximum execution time for a script if not configured.
const defaultMaxRunTime = 30 * time.Minute

// NewEngine creates a new V3 scripting engine for the given session and
// registers the v3 API namespaces in a fresh goja runtime. The engine's
// context is ctx limited to cfg.MaxRunTime (30 minutes when unset); when it
// ends, running JavaScript is interrupted. providers may be nil for
// console-only scripts; namespaces whose backing manager is nil (user,
// message, file, nodes) are simply not registered. The caller must Close the
// engine when the script finishes.
func NewEngine(ctx context.Context, session *SessionContext, cfg ScriptConfig, providers *Providers) *Engine {
	maxRunTime := cfg.MaxRunTime
	if maxRunTime <= 0 {
		maxRunTime = defaultMaxRunTime
	}
	ctx, cancel := context.WithTimeout(ctx, maxRunTime)
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
	go eng.watchContext()

	// Register V3 API namespaces.
	v3 := eng.vm.NewObject()
	registerConsole(v3, eng)
	registerSession(v3, eng)
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

	_, err = eng.vm.RunScript(scriptPath, string(data))
	if err != nil {
		if code, ok := exitStatus(err); ok {
			if code != 0 {
				slog.Info("V3 script exited with non-zero code",
					"path", scriptPath, "node", eng.session.NodeNumber, "code", code)
			}
			return nil
		}
		if eng.ctx.Err() != nil {
			if eng.ctx.Err() == context.DeadlineExceeded {
				return ErrTimeout
			}
			return ErrDisconnect
		}
		return fmt.Errorf("script error: %w", err)
	}
	return nil
}

// Close cleans up the engine, stopping I/O goroutines.
func (eng *Engine) Close() {
	if eng.pipeWriter != nil {
		_ = eng.pipeWriter.Close() // best-effort shutdown of the input pipe
	}
	// Wait for the copier goroutine to exit so it stops reading from
	// the session. This relies on the caller closing the read interrupt
	// (via SetReadInterrupt) to unblock the copier's session.Read().
	if eng.copierDone != nil {
		select {
		case <-eng.copierDone:
		case <-time.After(2 * time.Second):
			slog.Warn("V3 script copier goroutine did not exit within 2s; proceeding with cleanup")
		}
	}
	if eng.pipeReader != nil {
		_ = eng.pipeReader.Close() // best-effort shutdown of the input pipe
	}
	// Drain buffered results so goroutines don't block.
	if eng.rawInputCh != nil {
		for {
			select {
			case <-eng.rawInputCh:
			default:
				goto drained
			}
		}
	drained:
	}
	eng.cancel()
}

// watchContext monitors the context and interrupts the JS runtime on cancellation.
func (eng *Engine) watchContext() {
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

// writeRaw writes a string to the session, encoding Unicode text to CP437.
// This ensures characters like ½ (U+00BD) map to the correct CP437 byte (0xAB)
// rather than being truncated to their Unicode codepoint value.
func (eng *Engine) writeRaw(s string) {
	if s == "" {
		return
	}
	encoded, err := charmap.CodePage437.NewEncoder().Bytes([]byte(s))
	if err != nil {
		// Fallback: send UTF-8 bytes as-is if encoding fails.
		encoded = []byte(s)
	}
	if _, err := eng.session.Session.Write(encoded); err != nil {
		eng.cancel()
	}
}

// writeBytes writes raw bytes directly to the session without any encoding conversion.
// Used for ANSI art where CP437 bytes must be sent as-is.
func (eng *Engine) writeBytes(b []byte) {
	if len(b) == 0 {
		return
	}
	if _, err := eng.session.Session.Write(b); err != nil {
		eng.cancel()
	}
}

// startReader interposes a pipe between the session and the engine's input channel.
func (eng *Engine) startReader() {
	eng.readerOnce.Do(func() {
		eng.rawInputCh = make(chan readResult, 4)
		eng.pipeReader, eng.pipeWriter = io.Pipe()
		eng.copierDone = make(chan struct{})

		// Copier: session -> pipe
		go func() {
			defer close(eng.copierDone)
			buf := make([]byte, 256)
			for {
				n, err := eng.session.Session.Read(buf)
				if n > 0 {
					if _, werr := eng.pipeWriter.Write(buf[:n]); werr != nil {
						return
					}
				}
				if err != nil {
					eng.cancel() // signal context so CPU-bound scripts stop on disconnect
					eng.pipeWriter.CloseWithError(err)
					return
				}
			}
		}()

		// Reader: pipe -> channel
		go func() {
			for {
				buf := make([]byte, 64)
				n, err := eng.pipeReader.Read(buf)
				if n > 0 {
					eng.rawInputCh <- readResult{data: buf[:n]}
				}
				if err != nil {
					eng.rawInputCh <- readResult{err: err}
					return
				}
			}
		}()
	})
}

// readKey reads a single key from the session with optional timeout.
// Timeout of 0 means block indefinitely. See readKeyEcho for how keys are
// decoded.
func (eng *Engine) readKey(timeout time.Duration) (string, error) {
	key, _, err := eng.readKeyEcho(timeout)
	return key, err
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

	for {
		var data []byte
		if len(eng.inputBuf) > 0 {
			data, eng.inputBuf = eng.inputBuf, nil
		} else {
			eng.startReader()
			select {
			case result := <-eng.rawInputCh:
				if result.err != nil {
					eng.cancel()
					return "", nil, ErrDisconnect
				}
				if len(result.data) == 0 {
					return "", nil, nil
				}
				data = result.data
			case <-timer:
				return "", nil, nil
			case <-eng.ctx.Done():
				return "", nil, ErrTerminated
			}
		}

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
				// echo it in the session's encoding.
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

	if data[0] == 0x1b && len(data) > 1 {
		if data[1] == '[' && len(data) > 2 {
			// CSI sequences — consume and discard (arrow keys etc.)
			consumed = skipCSI(data)
			result = ""
		} else {
			consumed = 2
			result = ""
		}
	} else if data[0] == '\r' {
		if len(data) > 1 && data[1] == '\n' {
			consumed = 2
		}
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
