package scripting

import "errors"

// Sentinel errors for V3 script engine lifecycle.
var (
	// ErrTerminated is returned when a script is interrupted by context cancellation.
	ErrTerminated = errors.New("script terminated")

	// ErrDisconnect is returned when the user disconnects during script execution.
	ErrDisconnect = errors.New("user disconnected")

	// ErrTimeout is returned when a script exceeds its maximum execution time.
	ErrTimeout = errors.New("script timeout")

	// ErrNotice is returned when ScriptConfig.Notice fired while the script
	// waited for a key, and the script was stopped there.
	ErrNotice = errors.New("script stopped for a notice")

	// ErrHostPanic is wrapped in the error Run returns when a Go function
	// the script called panicked.
	ErrHostPanic = errors.New("host function panicked")
)

// noticeStop is the interrupt value that stops a script for a notice.
type noticeStop struct{}

// exitCode is used as an interrupt value for clean exit() calls.
type exitCode struct{ code int }
