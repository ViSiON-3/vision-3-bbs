// Package safegotest helps tests make a session goroutine panic (see
// safego.InjectPanic) and check what was logged for it.
package safegotest

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/safego"
)

// Log is the default logger's output while a test runs.
type Log struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *Log) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

// String returns everything logged so far.
func (l *Log) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// Capture sends the default logger to a Log until the test ends.
func Capture(t testing.TB) *Log {
	t.Helper()
	l := &Log{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(l, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return l
}

// Inject makes the next goroutine of kind panic at its test point, and takes
// the request back at the end of the test if it was not used.
func Inject(t testing.TB, kind string) {
	t.Helper()
	t.Cleanup(safego.InjectPanic(kind))
}

// WaitPanic waits for the injected panic in kind to be logged, and checks the
// log line names the goroutine and carries a stack through site, the file
// that started it. It returns the log.
func (l *Log) WaitPanic(t testing.TB, kind, site string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s := l.String()
		if strings.Contains(s, "injected panic in "+kind) {
			// slog quotes a value only when it has to.
			named := strings.Contains(s, `goroutine="`+kind+`"`) || strings.Contains(s, "goroutine="+kind+" ")
			if !named || !strings.Contains(s, "stack=") || !strings.Contains(s, site) {
				t.Errorf("panic in %s not logged with its goroutine and a stack through %s:\n%s", kind, site, s)
			}
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("no panic in %s was logged:\n%s", kind, s)
		}
		time.Sleep(2 * time.Millisecond)
	}
}
