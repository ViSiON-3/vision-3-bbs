package syncjs

import "errors"

// Sentinel errors for the Synchronet JS runtime.
var (
	ErrTerminated = errors.New("script terminated")
	ErrDisconnect = errors.New("user disconnected")

	// ErrHostPanic is wrapped in the error Run returns when a Go function
	// the script called panicked.
	ErrHostPanic = errors.New("host function panicked")
)
