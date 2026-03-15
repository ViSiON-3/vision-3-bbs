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
)

// exitCode is used as an interrupt value for clean exit() calls.
type exitCode struct{ code int }
