package ziplab

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// viewerSession is the minimum ssh.Session a ZMODEM send needs: it has no
// PTY, offers no input, and records what the transfer program sends.
type viewerSession struct {
	ssh.Session
	mu  sync.Mutex
	out bytes.Buffer
}

func (s *viewerSession) Read([]byte) (int, error) { return 0, io.EOF }

func (s *viewerSession) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out.Write(p)
}

func (s *viewerSession) Pty() (ssh.Pty, <-chan ssh.Window, bool) { return ssh.Pty{}, nil, false }

func (s *viewerSession) sent() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out.String()
}

// viewerHarness drives RunZipLabView with scripted line and key input and
// captures everything written to the terminal.
type viewerHarness struct {
	screen   bytes.Buffer
	terminal *term.Terminal
	session  *viewerSession
	lines    []string // returned by readLine in order; then an error
	keys     []int    // returned by readKey in order; then an error
	keyReads int
}

func newViewerHarness(lines []string, keys []int) *viewerHarness {
	h := &viewerHarness{session: &viewerSession{}, lines: lines, keys: keys}
	h.terminal = term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{strings.NewReader(""), &h.screen}, "")
	return h
}

func (h *viewerHarness) readLine() (string, error) {
	if len(h.lines) == 0 {
		return "", io.EOF
	}
	line := h.lines[0]
	h.lines = h.lines[1:]
	return line, nil
}

func (h *viewerHarness) readKey() (int, error) {
	h.keyReads++
	if len(h.keys) == 0 {
		return 0, io.EOF
	}
	key := h.keys[0]
	h.keys = h.keys[1:]
	return key, nil
}

func (h *viewerHarness) run(ctx context.Context, zipPath, name string) {
	RunZipLabView(ctx, h.session, h.terminal, zipPath, name, ansi.OutputModeUTF8, h.readLine, h.readKey)
}

// fakeSZ puts a stand-in for lrzsz's sz alone on PATH. It sends the file it
// is asked to transfer, followed by the path it was given.
func fakeSZ(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell script as the sz stand-in")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n/bin/cat \"$2\"\nprintf '|path=%s' \"$2\"\n"
	if err := os.WriteFile(filepath.Join(dir, "sz"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func viewerTestZip(t *testing.T) string {
	t.Helper()
	zipPath := filepath.Join(t.TempDir(), "view.zip")
	createTestZipWithTimes(t, zipPath, []struct{ Name, Content string }{
		{"readme.txt", "read me first"},
		{"docs/", ""},
		{"docs/manual.txt", "the manual"},
	}, time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC))
	return zipPath
}

func TestRunZipLabView_UnreadableArchive(t *testing.T) {
	// Any key but ENTER is ignored at the pause prompt.
	h := newViewerHarness(nil, []int{'x', ' ', '\r', 'y'})
	h.run(context.Background(), filepath.Join(t.TempDir(), "missing.zip"), "MISSING.ZIP")

	screen := h.screen.String()
	if !strings.Contains(screen, "Error reading archive.") {
		t.Errorf("screen missing error message: %q", screen)
	}
	if !strings.Contains(screen, "to continue...") {
		t.Errorf("screen missing pause prompt: %q", screen)
	}
	if h.keyReads != 3 {
		t.Errorf("read %d keys, want 3 (up to and including ENTER)", h.keyReads)
	}
	if strings.Contains(screen, "ZipLab [") {
		t.Errorf("selection prompt shown for an unreadable archive: %q", screen)
	}
}

func TestRunZipLabView_EmptyArchive(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "empty.zip")
	createTestZipWithTimes(t, zipPath, nil, time.Now())

	// The caller hanging up at the pause prompt ends the viewer.
	h := newViewerHarness([]string{"1"}, nil)
	h.run(context.Background(), zipPath, "EMPTY.ZIP")

	screen := h.screen.String()
	if !strings.Contains(screen, "Archive is empty.") {
		t.Errorf("screen missing empty-archive message: %q", screen)
	}
	if h.keyReads != 1 || len(h.lines) != 1 {
		t.Errorf("keyReads=%d, unread lines=%d; want one key read and no line read", h.keyReads, len(h.lines))
	}
}

func TestRunZipLabView_MenuInput(t *testing.T) {
	zipPath := viewerTestZip(t)

	// Blank redisplays the listing, junk and out-of-range numbers are
	// rejected, a directory entry cannot be extracted, and q leaves.
	h := newViewerHarness([]string{"", "abc", "0", "4", " 2 ", "q", "1"}, nil)
	h.run(context.Background(), zipPath, "VIEW.ZIP")

	screen := h.screen.String()
	if got := strings.Count(screen, "Archive Contents: VIEW.ZIP"); got != 2 {
		t.Errorf("listing shown %d times, want 2 (initial and redisplay)", got)
	}
	if got := strings.Count(screen, "3 file(s)"); got != 2 {
		t.Errorf("summary shown %d times, want 2", got)
	}
	if got := strings.Count(screen, "Invalid selection. Enter 1-3 or Q."); got != 3 {
		t.Errorf("invalid-selection message shown %d times, want 3", got)
	}
	if got := strings.Count(screen, "Extraction failed."); got != 1 {
		t.Errorf("extraction failure shown %d times, want 1", got)
	}
	if strings.Contains(screen, "via ZMODEM") {
		t.Errorf("a transfer was started: %q", screen)
	}
	// q ended the viewer before the last scripted line was read.
	if len(h.lines) != 1 {
		t.Errorf("%d scripted lines left unread, want 1", len(h.lines))
	}
	if got := h.session.sent(); got != "" {
		t.Errorf("session received %q, want nothing", got)
	}
}

func TestRunZipLabView_SendsSelectedEntry(t *testing.T) {
	fakeSZ(t)
	zipPath := viewerTestZip(t)

	// One send with no context (the viewer supplies its own timeout) and
	// one with a caller deadline; input then ends, which closes the viewer.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{{"no context", nil}, {"caller deadline", ctx}} {
		t.Run(tc.name, func(t *testing.T) {
			h := newViewerHarness([]string{"3"}, nil)
			h.run(tc.ctx, zipPath, "VIEW.ZIP")

			screen := h.screen.String()
			if !strings.Contains(screen, "manual.txt") || !strings.Contains(screen, "via ZMODEM...") {
				t.Errorf("screen missing send notice: %q", screen)
			}
			if !strings.Contains(screen, "Transfer complete.") || strings.Contains(screen, "Transfer failed.") {
				t.Errorf("screen does not report a completed transfer: %q", screen)
			}

			content, sentPath, ok := strings.Cut(h.session.sent(), "|path=")
			if !ok || content != "the manual" {
				t.Fatalf("session received %q, want the entry's content and its path", h.session.sent())
			}
			// Only the base name is used, and the temp copy is gone afterwards.
			if filepath.Base(sentPath) != "manual.txt" {
				t.Errorf("sent file %q, want base name manual.txt", sentPath)
			}
			if _, err := os.Stat(filepath.Dir(sentPath)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("temp dir %s not cleaned up after send (stat err = %v)", filepath.Dir(sentPath), err)
			}
		})
	}
}

func TestRunZipLabView_TransferFailure(t *testing.T) {
	// With no sz on PATH the send cannot start; the viewer reports it and
	// returns to its prompt.
	t.Setenv("PATH", t.TempDir())
	zipPath := viewerTestZip(t)

	h := newViewerHarness([]string{"1", "Q"}, nil)
	h.run(context.Background(), zipPath, "VIEW.ZIP")

	screen := h.screen.String()
	if !strings.Contains(screen, "Transfer failed.") || strings.Contains(screen, "Transfer complete.") {
		t.Errorf("screen does not report a failed transfer: %q", screen)
	}
	if got := strings.Count(screen, "ZipLab ["); got != 2 {
		t.Errorf("selection prompt shown %d times, want 2", got)
	}
	if len(h.lines) != 0 {
		t.Errorf("%d scripted lines left unread, want 0", len(h.lines))
	}
}

func TestSanitizeEntryName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain.txt", "plain.txt"},
		{"evil\x1b[2J.txt", "evil[2J.txt"},
		{"tab\there\r\n", "tabhere"},
		{"del\x7fete", "delete"},
		{"|04red|07.txt", ".04red.07.txt"},
	}
	for _, tt := range tests {
		if got := sanitizeEntryName(tt.in); got != tt.want {
			t.Errorf("sanitizeEntryName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestExtractSingleEntry_Errors(t *testing.T) {
	zipPath := viewerTestZip(t)

	tests := []struct {
		name    string
		path    string
		entry   int
		wantErr string
	}{
		{"zero index", zipPath, 0, "must be >= 1"},
		{"missing archive", filepath.Join(t.TempDir(), "missing.zip"), 1, "failed to open archive"},
		{"directory entry", zipPath, 2, "is a directory"},
		{"past the end", zipPath, 4, "out of range (archive has 3 entries)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, cleanup, err := extractSingleEntry(tt.path, tt.entry)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
			if path != "" {
				t.Errorf("path = %q, want empty on error", path)
			}
			cleanup() // must be safe to call on the error path
		})
	}
}

func TestExtractSingleEntry_FlattensNestedPath(t *testing.T) {
	path, cleanup, err := extractSingleEntry(viewerTestZip(t), 3)
	if err != nil {
		t.Fatalf("extractSingleEntry: %v", err)
	}
	defer cleanup()

	// docs/manual.txt lands directly in the temp dir, not in a docs subdir.
	if filepath.Base(path) != "manual.txt" || filepath.Base(filepath.Dir(path)) == "docs" {
		t.Errorf("extracted to %q, want manual.txt directly in the temp dir", path)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "the manual" {
		t.Errorf("extracted content = %q, err %v", data, err)
	}
}
