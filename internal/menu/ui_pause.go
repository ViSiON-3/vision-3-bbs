package menu

import "time"

// uiPause holds a status or error message on screen for d before the handler
// moves on, so the caller has time to read it. Every fixed display pause in
// the package goes through it; it is a variable so tests can skip the waits
// (see harness_env_test.go). Timing-sensitive sleeps, such as waiting on a
// door process or a transfer, call time.Sleep directly.
var uiPause = time.Sleep
