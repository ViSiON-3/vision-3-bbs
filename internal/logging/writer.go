package logging

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

const (
	// cacheBufSize is the buffered-write cache size (matches the design's 8KB).
	cacheBufSize = 8 * 1024
	// flushInterval bounds how long a cached line can sit unwritten.
	flushInterval = 5 * time.Second
	// dateLayout is the date stamp used for daily-rolled filenames and pruning.
	dateLayout = "2006-01-02"
)

// errClosed is returned by Write/Flush after the writer has been closed.
var errClosed = errors.New("logging: writer closed")

// rollingWriter is a mutex-guarded io.WriteCloser implementing the three Mystic
// log types (none/size/daily) plus an optional 8KB write cache. A clock is
// injected so rolling behavior is deterministically testable.
type rollingWriter struct {
	mu       sync.Mutex
	dir      string
	stem     string // filename without extension, e.g. "vision3"
	ext      string // extension including the dot, e.g. ".log"
	logType  int
	maxFiles int
	maxSize  int64 // rotate threshold in bytes (size type); 0 disables rolling
	cache    bool
	now      func() time.Time

	f    *os.File
	bw   *bufio.Writer
	size int64  // bytes in the current file (size type)
	day  string // date stamp of the open file (daily type)

	ticker   *time.Ticker
	stopCh   chan struct{}
	stopOnce sync.Once
	closed   bool
}

// newRollingWriter creates the writer for cfg, logging to defaultFile within
// cfg.Dir. cfg is assumed already Normalize()d. now supplies the current time
// (pass time.Now in production).
func newRollingWriter(cfg config.LoggingConfig, defaultFile string, now func() time.Time) (*rollingWriter, error) {
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, err
	}
	ext := filepath.Ext(defaultFile)
	w := &rollingWriter{
		dir:      cfg.Dir,
		stem:     strings.TrimSuffix(defaultFile, ext),
		ext:      ext,
		logType:  cfg.Type,
		maxFiles: cfg.MaxFiles,
		maxSize:  int64(cfg.MaxSizeKB) * 1024,
		cache:    cfg.Cache,
		now:      now,
		stopCh:   make(chan struct{}),
	}
	if err := w.openCurrent(); err != nil {
		return nil, err
	}
	if w.cache {
		w.ticker = time.NewTicker(flushInterval)
		go w.flushLoop()
	}
	return w, nil
}

// basePath is the unrolled file path (none/size types).
func (w *rollingWriter) basePath() string {
	return filepath.Join(w.dir, w.stem+w.ext)
}

// backupPath is the Nth numbered backup for size rolling (e.g. vision3.log.1).
func (w *rollingWriter) backupPath(n int) string {
	return w.basePath() + "." + strconv.Itoa(n)
}

// datedPath is the date-stamped file for daily rolling (e.g. vision3.2026-06-28.log).
func (w *rollingWriter) datedPath(day string) string {
	return filepath.Join(w.dir, w.stem+"."+day+w.ext)
}

// currentPath returns the path the writer should currently be appending to.
func (w *rollingWriter) currentPath() string {
	if w.logType == config.LogTypeDaily {
		return w.datedPath(w.day)
	}
	return w.basePath()
}

// openCurrent opens (creating if needed) the current target file for append and
// initializes the cache and size/day tracking.
func (w *rollingWriter) openCurrent() error {
	if w.logType == config.LogTypeDaily {
		w.day = w.now().Format(dateLayout)
	}
	f, err := os.OpenFile(w.currentPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.f = f
	w.size = fi.Size()
	if w.cache {
		w.bw = bufio.NewWriterSize(f, cacheBufSize)
	} else {
		w.bw = nil
	}
	return nil
}

// closeCurrent flushes the cache and closes the open file.
func (w *rollingWriter) closeCurrent() error {
	var ferr error
	if w.bw != nil {
		ferr = w.bw.Flush()
		w.bw = nil
	}
	if w.f != nil {
		if cerr := w.f.Close(); ferr == nil {
			ferr = cerr
		}
		w.f = nil
	}
	return ferr
}

// dst returns the active write destination (cache or raw file).
func (w *rollingWriter) dst() interface{ Write([]byte) (int, error) } {
	if w.bw != nil {
		return w.bw
	}
	return w.f
}

// Write implements io.Writer, rolling the file first if the type and current
// state call for it.
func (w *rollingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, errClosed
	}

	switch w.logType {
	case config.LogTypeDaily:
		if today := w.now().Format(dateLayout); today != w.day {
			if err := w.rotateDaily(); err != nil {
				return 0, err
			}
		}
	case config.LogTypeSize:
		// Roll when the write would exceed the threshold, but never roll an
		// empty file (a single oversized line is written as-is).
		if w.maxSize > 0 && w.size > 0 && w.size+int64(len(p)) > w.maxSize {
			if err := w.rotateSize(); err != nil {
				return 0, err
			}
		}
	}

	n, err := w.dst().Write(p)
	w.size += int64(n)
	return n, err
}

// rotateDaily closes the current dated file, opens today's, and prunes dated
// files older than maxFiles days.
func (w *rollingWriter) rotateDaily() error {
	if err := w.closeCurrent(); err != nil {
		return err
	}
	if err := w.openCurrent(); err != nil {
		return err
	}
	w.pruneDaily()
	return nil
}

// pruneDaily removes dated log files whose date is more than maxFiles days
// before today. Errors are ignored: pruning is best-effort housekeeping.
func (w *rollingWriter) pruneDaily() {
	matches, err := filepath.Glob(filepath.Join(w.dir, w.stem+".*"+w.ext))
	if err != nil {
		return
	}
	cutoff := w.now().AddDate(0, 0, -w.maxFiles)
	for _, m := range matches {
		name := filepath.Base(m)
		datePart := strings.TrimSuffix(strings.TrimPrefix(name, w.stem+"."), w.ext)
		d, perr := time.Parse(dateLayout, datePart)
		if perr != nil {
			continue // not a dated file (e.g. a size-style backup)
		}
		if d.Before(cutoff) {
			os.Remove(m)
		}
	}
}

// rotateSize shifts the numbered backups (deleting the oldest) and opens a
// fresh base file.
func (w *rollingWriter) rotateSize() error {
	if err := w.closeCurrent(); err != nil {
		return err
	}
	os.Remove(w.backupPath(w.maxFiles)) // discard the oldest
	for i := w.maxFiles - 1; i >= 1; i-- {
		os.Rename(w.backupPath(i), w.backupPath(i+1))
	}
	os.Rename(w.basePath(), w.backupPath(1))
	return w.openCurrent()
}

// Flush flushes the write cache. It is a no-op when caching is disabled.
func (w *rollingWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.bw == nil {
		return nil
	}
	return w.bw.Flush()
}

// flushLoop periodically flushes the cache until the writer is closed.
func (w *rollingWriter) flushLoop() {
	for {
		select {
		case <-w.ticker.C:
			_ = w.Flush()
		case <-w.stopCh:
			return
		}
	}
}

// Close stops the flush loop, flushes the cache, and closes the file. It is
// safe to call more than once.
func (w *rollingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if w.ticker != nil {
		w.ticker.Stop()
	}
	w.stopOnce.Do(func() { close(w.stopCh) })
	return w.closeCurrent()
}
