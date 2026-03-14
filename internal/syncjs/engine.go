package syncjs

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
)

// Engine is a Synchronet-compatible JavaScript runtime for a single BBS session.
// Each connected user running a JS door gets their own Engine instance.
type Engine struct {
	vm      *goja.Runtime
	session *SessionContext
	cfg     SyncJSDoorConfig
	ctx     context.Context
	cancel  context.CancelFunc

	// Attribute tracking
	currentAttr uint8

	// Module system
	execDirStack []string
	execDirMu    sync.Mutex

	// Exit handlers registered via js.on_exit()
	exitHandlers []goja.Callable
	exitCodes    []string // JS code strings to eval on exit

	// Input: interposed pipe reader feeds rawInputCh; inputBuf holds parsed leftovers
	inputBuf   []byte
	rawInputCh chan readResult // fed by reader goroutine on the pipe
	pipeReader *io.PipeReader  // engine reads from this end
	pipeWriter *io.PipeWriter  // copier writes session data to this end
	readerOnce sync.Once
	copierDone chan struct{} // closed when the copier goroutine exits

	// Lock files created by file_mutex() — cleaned up on Close()
	lockFiles []string

	// Pending cursor position response for DORKit's detect_ansi().
	// When writeRaw intercepts \x1b[6n, it sets this flag so the Queue
	// bridge can return a POSITION_row_col string on the next read.
	pendingDSR bool
}

// NewEngine creates a new Synchronet JS engine for the given session context.
func NewEngine(ctx context.Context, session *SessionContext, cfg SyncJSDoorConfig) *Engine {
	ctx, cancel := context.WithCancel(ctx)
	eng := &Engine{
		vm:           goja.New(),
		session:      session,
		cfg:          cfg,
		ctx:          ctx,
		cancel:       cancel,
		currentAttr:  7, // default: light gray on black
		execDirStack: []string{cfg.WorkingDir + "/"},
	}

	// Set up interrupt checking — allows context cancellation to halt JS execution
	go eng.watchContext()

	// Register all Synchronet API objects
	registerConsole(eng.vm, eng)
	registerBBS(eng.vm, eng)
	registerUser(eng.vm, eng)
	registerSystem(eng.vm, eng)
	registerFileClass(eng.vm, eng)
	registerModuleSystem(eng.vm, eng)
	registerQueueClass(eng.vm, eng)
	registerServerClient(eng.vm, eng)

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

	// Set initial exec_dir to the script's directory
	eng.pushExecDir(filepath.Dir(scriptPath) + "/")

	log.Printf("INFO: SyncJS: Running script %s for node %d", scriptPath, eng.session.NodeNumber)

	_, err = eng.vm.RunScript(scriptPath, string(data))
	if err != nil {
		if isExitPanic(err) {
			return nil
		}
		if eng.ctx.Err() != nil {
			return ErrDisconnect
		}
		return fmt.Errorf("script error: %w", err)
	}
	return nil
}

// Close runs exit handlers and cleans up the engine.
func (eng *Engine) Close() {
	// Run callable exit handlers in reverse order
	for i := len(eng.exitHandlers) - 1; i >= 0; i-- {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("WARN: SyncJS: exit handler panic: %v", r)
				}
			}()
			eng.exitHandlers[i](goja.Undefined())
		}()
	}
	// Eval string exit codes in reverse order (Synchronet behavior)
	for i := len(eng.exitCodes) - 1; i >= 0; i-- {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("WARN: SyncJS: exit code eval panic: %v", r)
				}
			}()
			eng.vm.RunString(eng.exitCodes[i])
		}()
	}
	// Stop the reader goroutines by closing the pipe. The caller should
	// close a SetReadInterrupt channel before calling Close() so the
	// copier goroutine's blocked session.Read() returns immediately.
	if eng.pipeWriter != nil {
		eng.pipeWriter.Close()
	}
	// Wait for the copier goroutine to exit so it stops reading from
	// the session. This relies on the caller closing the read interrupt
	// (via SetReadInterrupt) to unblock the copier's session.Read().
	if eng.copierDone != nil {
		<-eng.copierDone
	}
	if eng.pipeReader != nil {
		eng.pipeReader.Close()
	}
	// Drain any buffered results so goroutines don't block on channel send
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
	// Clean up lock files created by file_mutex()
	for _, lf := range eng.lockFiles {
		os.Remove(lf)
	}
	eng.cancel()
}

// watchContext monitors the context and interrupts the JS runtime on cancellation.
func (eng *Engine) watchContext() {
	<-eng.ctx.Done()
	eng.vm.Interrupt(ErrTerminated)
}

// --- I/O helpers used by console and other objects ---

// writeRaw writes raw bytes to the session, applying output mode conversion.
// Strings may contain runes 0-255 from Latin-1 file reads; convert each rune
// back to a single byte so terminalio can process them as CP437.
func (eng *Engine) writeRaw(s string) {
	if s == "" {
		return
	}
	raw := runesToBytes(s)
	filtered := filterOutputSequences(raw)
	if len(filtered.data) > 0 {
		// Write raw CP437 bytes directly to the session — no UTF-8 reinterpretation.
		if _, err := eng.session.Session.Write(filtered.data); err != nil {
			eng.cancel()
			return
		}
	}
	// If a cursor position query (\x1b[6n) was intercepted, set a flag so the
	// Queue bridge returns a synthetic POSITION response. DORKit's detect_ansi()
	// sends \x1b[6n and blocks waiting for POSITION_row_col from the input queue.
	if filtered.hasDSR {
		eng.pendingDSR = true
	}
}

// filterOutputResult holds the filtered output and flags for intercepted sequences.
type filterOutputResult struct {
	data    []byte
	hasDSR  bool // true if \x1b[6n was intercepted (needs synthetic response)
}

// filterOutputSequences removes or intercepts problematic CSI sequences:
//   - \x1b[! (Soft Terminal Reset) — most terminals display it literally as "[!"
//   - \x1b[6n (Device Status Report) — intercepted so we can inject a synthetic
//     cursor position response instead of letting the terminal respond with bytes
//     that get consumed as phantom input
func filterOutputSequences(data []byte) filterOutputResult {
	// Fast path: no ESC means nothing to filter
	hasESC := false
	for _, b := range data {
		if b == 0x1b {
			hasESC = true
			break
		}
	}
	if !hasESC {
		return filterOutputResult{data: data}
	}

	result := filterOutputResult{}
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		if data[i] == 0x1b && i+2 < len(data) && data[i+1] == '[' {
			// Check for \x1b[! (soft terminal reset)
			if data[i+2] == '!' {
				i += 2
				continue
			}
			// Check for \x1b[6n (device status report / cursor position query)
			if data[i+2] == '6' && i+3 < len(data) && data[i+3] == 'n' {
				i += 3
				result.hasDSR = true
				continue
			}
		}
		out = append(out, data[i])
	}
	result.data = out
	return result
}

// runesToBytes converts a string (possibly containing Latin-1 runes 128-255)
// back to raw bytes. Runes > 255 are passed through as UTF-8.
func runesToBytes(s string) []byte {
	// Fast path: if the string is all ASCII, just return the bytes directly
	allASCII := true
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			allASCII = false
			break
		}
	}
	if allASCII {
		return []byte(s)
	}

	buf := make([]byte, 0, len(s))
	for _, r := range s {
		if r < 256 {
			buf = append(buf, byte(r))
		} else {
			// Pass through higher Unicode as UTF-8 (e.g. ANSI escape sequences)
			var tmp [4]byte
			n := encodeRune(tmp[:], r)
			buf = append(buf, tmp[:n]...)
		}
	}
	return buf
}

func encodeRune(buf []byte, r rune) int {
	if r < 0x80 {
		buf[0] = byte(r)
		return 1
	}
	if r < 0x800 {
		buf[0] = byte(0xC0 | (r >> 6))
		buf[1] = byte(0x80 | (r & 0x3F))
		return 2
	}
	if r < 0x10000 {
		buf[0] = byte(0xE0 | (r >> 12))
		buf[1] = byte(0x80 | ((r >> 6) & 0x3F))
		buf[2] = byte(0x80 | (r & 0x3F))
		return 3
	}
	buf[0] = byte(0xF0 | (r >> 18))
	buf[1] = byte(0x80 | ((r >> 12) & 0x3F))
	buf[2] = byte(0x80 | ((r >> 6) & 0x3F))
	buf[3] = byte(0x80 | (r & 0x3F))
	return 4
}

// startReader interposes a pipe between the session and the engine's input
// channel. A copier goroutine moves bytes from the session into the pipe,
// and a reader goroutine reads from the pipe into rawInputCh. Closing the
// pipe in Close() cleanly stops both goroutines without affecting the session.
func (eng *Engine) startReader() {
	eng.readerOnce.Do(func() {
		eng.rawInputCh = make(chan readResult, 4)
		eng.pipeReader, eng.pipeWriter = io.Pipe()
		eng.copierDone = make(chan struct{})

		// Copier: session -> pipe (blocks on session.Read, killed by read interrupt)
		go func() {
			defer close(eng.copierDone)
			buf := make([]byte, 256)
			for {
				n, err := eng.session.Session.Read(buf)
				if n > 0 {
					if _, werr := eng.pipeWriter.Write(buf[:n]); werr != nil {
						return // pipe closed
					}
				}
				if err != nil {
					eng.pipeWriter.CloseWithError(err)
					return
				}
			}
		}()

		// Reader: pipe -> channel (killed by pipe close returning error)
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

type readResult struct {
	data []byte
	err  error
}

// parseInput translates raw input bytes (potentially ANSI escape sequences) to key strings.
// Consumes exactly one logical key from data and buffers the rest in inputBuf.
func (eng *Engine) parseInput(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	consumed := 1
	result := string(data[0])

	if data[0] == 0x1b && len(data) > 1 {
		if data[1] == '[' && len(data) > 2 {
			// CSI sequences: \x1b[ followed by parameter bytes and a final letter
			seqLen := 3 // minimum: \x1b [ X
			mapped := ""
			switch data[2] {
			case 'A':
				mapped = "\x01\x48" // KEY_UP
			case 'B':
				mapped = "\x01\x50" // KEY_DOWN
			case 'C':
				mapped = "\x01\x4d" // KEY_RIGHT
			case 'D':
				mapped = "\x01\x4b" // KEY_LEFT
			case 'H':
				mapped = "\x01\x47" // KEY_HOME
			case 'F':
				mapped = "\x01\x4f" // KEY_END
			case 'V':
				mapped = "\x01\x49" // KEY_PGUP
			case 'U':
				mapped = "\x01\x51" // KEY_PGDN
			}
			if mapped != "" {
				consumed = seqLen
				result = mapped
			} else if len(data) > 3 && data[3] == '~' {
				seqLen = 4
				switch data[2] {
				case '1':
					mapped = "\x01\x47" // KEY_HOME
				case '2':
					mapped = "\x01\x52" // KEY_INS
				case '3':
					mapped = "\x01\x53" // KEY_DEL
				case '4':
					mapped = "\x01\x4f" // KEY_END
				case '5':
					mapped = "\x01\x49" // KEY_PGUP
				case '6':
					mapped = "\x01\x51" // KEY_PGDN
				}
				if mapped != "" {
					consumed = seqLen
					result = mapped
				} else {
					// Unknown CSI sequence — skip to the end (final byte is 0x40-0x7E)
					consumed = skipCSI(data)
					result = ""
				}
			} else {
				// Unknown CSI — skip to the terminal byte
				consumed = skipCSI(data)
				result = ""
			}
		} else {
			// ESC + something else — consume both bytes, discard
			consumed = 2
			result = ""
		}
	} else if data[0] == '\r' {
		// Consume \r\n as a single Enter keypress
		if len(data) > 1 && data[1] == '\n' {
			consumed = 2
		}
		result = "\r"
	} else if data[0] == '\n' {
		// Treat bare \n as Enter too
		result = "\r"
	}

	if consumed < len(data) {
		eng.inputBuf = append(eng.inputBuf, data[consumed:]...)
	}
	return result
}

// skipCSI finds the end of a CSI escape sequence starting at data[0]=ESC.
// CSI sequences end with a byte in the range 0x40-0x7E.
func skipCSI(data []byte) int {
	for i := 2; i < len(data); i++ {
		if data[i] >= 0x40 && data[i] <= 0x7E {
			return i + 1
		}
	}
	return len(data) // consume all if unterminated
}

// readLine reads a line of input with echo and basic editing.
func (eng *Engine) readLine(maxLen int, mode int64) (string, error) {
	var buf []byte
	noEcho := mode&kNoEcho != 0
	upper := mode&kUpper != 0
	numberOnly := mode&kNumber != 0

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
			if mode&kNoCRLF == 0 {
				eng.writeRaw("\r\n")
			}
			result := string(buf)
			if upper {
				result = toUpperASCII(result)
			}
			return result, nil
		case '\x08', '\x7f': // Backspace, DEL
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				if !noEcho {
					eng.writeRaw("\x08 \x08")
				}
			}
		case '\x1b': // ESC — abort
			return "", nil
		default:
			if numberOnly && (ch < '0' || ch > '9') {
				continue
			}
			if len(buf) < maxLen {
				if upper && ch >= 'a' && ch <= 'z' {
					ch = ch - 32
				}
				buf = append(buf, ch)
				if !noEcho {
					eng.writeRaw(string(ch))
				}
			}
		}
	}
}

// --- Exec dir stack ---

func (eng *Engine) currentExecDir() string {
	eng.execDirMu.Lock()
	defer eng.execDirMu.Unlock()
	if len(eng.execDirStack) == 0 {
		return eng.cfg.WorkingDir + "/"
	}
	return eng.execDirStack[len(eng.execDirStack)-1]
}

func (eng *Engine) pushExecDir(dir string) {
	eng.execDirMu.Lock()
	defer eng.execDirMu.Unlock()
	eng.execDirStack = append(eng.execDirStack, dir)
}

func (eng *Engine) popExecDir() {
	eng.execDirMu.Lock()
	defer eng.execDirMu.Unlock()
	if len(eng.execDirStack) > 1 {
		eng.execDirStack = eng.execDirStack[:len(eng.execDirStack)-1]
	}
}

func (eng *Engine) addExitHandler(fn goja.Callable) {
	eng.exitHandlers = append(eng.exitHandlers, fn)
}

// addExitCode registers a JS code string to be eval'd on exit.
// Synchronet's js.on_exit() accepts both functions and code strings.
func (eng *Engine) addExitCode(code string) {
	eng.exitCodes = append(eng.exitCodes, code)
}

// createInputQueue returns a Queue-like JS object backed by real session I/O.
// This replaces Synchronet's background input thread — instead of a separate
// thread reading stdin and pushing to a Queue, we read directly from the session.
func (eng *Engine) createInputQueue() goja.Value {
	obj := eng.vm.NewObject()

	// poll(timeout_ms) — check if input is available within timeout
	obj.Set("poll", func(call goja.FunctionCall) goja.Value {
		timeout := int64(0)
		if len(call.Arguments) > 0 {
			timeout = call.Arguments[0].ToInteger()
		}
		if timeout <= 0 {
			timeout = 1 // minimum 1ms to avoid busy loop
		}
		key, err := eng.readKey(time.Duration(timeout) * time.Millisecond)
		if err != nil {
			return eng.vm.ToValue(false)
		}
		if key == "" {
			return eng.vm.ToValue(false)
		}
		// Got a key — buffer it for the next read()
		eng.inputBuf = append([]byte(key), eng.inputBuf...)
		return eng.vm.ToValue(true)
	})

	// read() — read a single key/character
	obj.Set("read", func(call goja.FunctionCall) goja.Value {
		if len(eng.inputBuf) > 0 {
			ch := eng.inputBuf[0]
			eng.inputBuf = eng.inputBuf[1:]
			return eng.vm.ToValue(string(ch))
		}
		key, err := eng.readKey(0)
		if err != nil || key == "" {
			return goja.Undefined()
		}
		return eng.vm.ToValue(key)
	})

	// write() — no-op for input queue (used for signaling exit)
	obj.Set("write", func(call goja.FunctionCall) goja.Value {
		return eng.vm.ToValue(true)
	})

	return obj
}

// newStubObject creates a recursive JS Proxy where any property access returns
// another stub, and any function call returns a stub. Used as a fallback when
// scoped require() fails — prevents chained "cannot read property of undefined".
func (eng *Engine) newStubObject() goja.Value {
	stub := eng.vm.NewObject()
	proxyCode := `(function makeStub(target) {
		var handler = {
			get: function(obj, prop) {
				if (prop === Symbol.toPrimitive) return function() { return ''; };
				if (prop === 'toString' || prop === 'valueOf') return function() { return ''; };
				if (prop in obj) return obj[prop];
				var child = new Proxy(function(){}, handler);
				return child;
			},
			apply: function() {
				return new Proxy({}, handler);
			}
		};
		return new Proxy(target, handler);
	})`
	proxyFn, err := eng.vm.RunString(proxyCode)
	if err != nil {
		return stub
	}
	fn, ok := goja.AssertFunction(proxyFn)
	if !ok {
		return stub
	}
	result, err := fn(goja.Undefined(), stub)
	if err != nil {
		return stub
	}
	return result
}

// isExitPanic checks if an error is from a clean exit() call or context cancellation.
func isExitPanic(err error) bool {
	if ex, ok := err.(*goja.InterruptedError); ok {
		if _, isExit := ex.Value().(exitCode); isExit {
			return true
		}
		return true // context cancellation also triggers interrupt
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
