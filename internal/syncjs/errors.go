package syncjs

import "errors"

// Sentinel errors for the Synchronet JS runtime.
var (
	ErrTerminated = errors.New("script terminated")
	ErrDisconnect = errors.New("user disconnected")
)
