package scripting

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

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
	inputBuf   []byte
	rawInputCh chan readResult
	pipeReader *io.PipeReader
	pipeWriter *io.PipeWriter
	readerOnce sync.Once
	copierDone chan struct{} // closed when the copier goroutine exits
}

// readResult carries data or an error from the reader goroutine.
type readResult struct {
	data []byte
	err  error
}

// NewEngine creates a new V3 scripting engine for the given session.
// Providers may be nil for console-only scripts.
// defaultMaxRunTime is the maximum execution time for a script if not configured.
const defaultMaxRunTime = 30 * time.Minute

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
	eng.vm.Set("v3", v3)

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

	log.Printf("INFO: V3Script: Running %s for node %d", scriptPath, eng.session.NodeNumber)

	_, err = eng.vm.RunScript(scriptPath, string(data))
	if err != nil {
		if isExitPanic(err) {
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
		eng.pipeWriter.Close()
	}
	// Wait for the copier goroutine to exit so it stops reading from
	// the session. This relies on the caller closing the read interrupt
	// (via SetReadInterrupt) to unblock the copier's session.Read().
	if eng.copierDone != nil {
		select {
		case <-eng.copierDone:
		case <-time.After(2 * time.Second):
			log.Printf("WARN: V3Script: copier goroutine did not exit within 2s; proceeding with cleanup")
		}
	}
	if eng.pipeReader != nil {
		eng.pipeReader.Close()
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
	eng.vm.Set("exit", func(call goja.FunctionCall) goja.Value {
		code := 0
		if len(call.Arguments) > 0 {
			code = int(call.Arguments[0].ToInteger())
		}
		panic(eng.vm.ToValue(exitCode{code: code}))
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
// Timeout of 0 means block indefinitely.
func (eng *Engine) readKey(timeout time.Duration) (string, error) {
	if len(eng.inputBuf) > 0 {
		ch := eng.inputBuf[0]
		eng.inputBuf = eng.inputBuf[1:]
		return string(ch), nil
	}

	eng.startReader()

	var timer <-chan time.Time
	if timeout > 0 {
		timer = time.After(timeout)
	}

	select {
	case result := <-eng.rawInputCh:
		if result.err != nil {
			eng.cancel()
			return "", ErrDisconnect
		}
		if len(result.data) == 0 {
			return "", nil
		}
		return eng.parseInput(result.data), nil
	case <-timer:
		return "", nil
	case <-eng.ctx.Done():
		return "", ErrTerminated
	}
}

// readLine reads a line of input with echo and basic editing.
func (eng *Engine) readLine(maxLen int, opts lineOpts) (string, error) {
	var buf []byte
	for {
		key, err := eng.readKey(0)
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
				buf = buf[:len(buf)-1]
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
			if len(buf) < maxLen {
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
	result := string(data[0])

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

// isExitPanic checks if an error is from a clean exit() call or context cancellation.
func isExitPanic(err error) bool {
	if ex, ok := err.(*goja.InterruptedError); ok {
		if _, isExit := ex.Value().(exitCode); isExit {
			return true
		}
		return true
	}
	return false
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
