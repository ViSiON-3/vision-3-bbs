package ziplab

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// sentEntry is one extracted member handed to the viewer's send callback:
// its path and its content as read while the file still existed.
type sentEntry struct {
	path, content string
}

// viewerHarness drives RunZipLabView with scripted line and key input and
// captures everything written to the terminal.
type viewerHarness struct {
	screen   bytes.Buffer
	terminal *term.Terminal
	sends    []sentEntry
	lines    []string // returned by readLine in order; then an error
	keys     []int    // returned by readKey in order; then an error
	keyReads int
}

func newViewerHarness(lines []string, keys []int) *viewerHarness {
	h := &viewerHarness{lines: lines, keys: keys}
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

func (h *viewerHarness) send(extract ExtractFunc) {
	path, err := extract()
	if err != nil {
		return
	}
	content, err := os.ReadFile(path)
	if err != nil {
		content = []byte("read error: " + err.Error())
	}
	h.sends = append(h.sends, sentEntry{path: path, content: string(content)})
}

func (h *viewerHarness) run(zipPath, name string) {
	RunZipLabView(h.terminal, zipPath, name, ansi.OutputModeUTF8, h.readLine, h.readKey, h.send)
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
	h.run(filepath.Join(t.TempDir(), "missing.zip"), "MISSING.ZIP")

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
	h.run(zipPath, "EMPTY.ZIP")

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
	h.run(zipPath, "VIEW.ZIP")

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
	if len(h.sends) != 0 {
		t.Errorf("sent %d entries, want none", len(h.sends))
	}
	// q ended the viewer before the last scripted line was read.
	if len(h.lines) != 1 {
		t.Errorf("%d scripted lines left unread, want 1", len(h.lines))
	}
}

func TestRunZipLabView_SendsSelectedEntry(t *testing.T) {
	zipPath := viewerTestZip(t)

	// The selected member is extracted and handed to send, then the viewer
	// returns to its prompt; input then ends, which closes the viewer.
	h := newViewerHarness([]string{"3", "1"}, nil)
	h.run(zipPath, "VIEW.ZIP")

	if len(h.sends) != 2 {
		t.Fatalf("sent %d entries, want 2", len(h.sends))
	}
	got := h.sends[0]
	if got.content != "the manual" {
		t.Errorf("sent content %q, want the entry's content", got.content)
	}
	// Only the base name is used, and the temp copy is gone afterwards.
	if filepath.Base(got.path) != "manual.txt" {
		t.Errorf("sent file %q, want base name manual.txt", got.path)
	}
	if _, err := os.Stat(filepath.Dir(got.path)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp dir %s not cleaned up after send (stat err = %v)", filepath.Dir(got.path), err)
	}
	if h.sends[1].content != "read me first" {
		t.Errorf("second send content %q, want readme.txt's", h.sends[1].content)
	}
	if got := strings.Count(h.screen.String(), "ZipLab ["); got != 3 {
		t.Errorf("selection prompt shown %d times, want 3", got)
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

func TestRunZipLabView_DeclinedSendExtractsNothing(t *testing.T) {
	zipPath := viewerTestZip(t)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)

	// A sender that refuses (no access, or a cancelled protocol menu) never
	// calls extract, so nothing is unpacked.
	h := newViewerHarness([]string{"1", "3"}, nil)
	calls := 0
	RunZipLabView(h.terminal, zipPath, "VIEW.ZIP", ansi.OutputModeUTF8, h.readLine, h.readKey, func(ExtractFunc) { calls++ })

	if calls != 2 {
		t.Errorf("send called %d times, want 2", calls)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("temp dir holds %d entries after declined sends, want none", len(entries))
	}
}

func TestExtractSingleEntry_SizeLimit(t *testing.T) {
	zipPath := viewerTestZip(t)
	old := maxExtractBytes
	maxExtractBytes = 4
	t.Cleanup(func() { maxExtractBytes = old })

	// readme.txt declares 13 bytes, over the lowered limit.
	if _, cleanup, err := extractSingleEntry(zipPath, 1); err == nil || !strings.Contains(err.Error(), "extraction limit") {
		cleanup()
		t.Fatalf("err = %v, want extraction-limit refusal", err)
	}
}
