package menu

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestComputeFilePagination(t *testing.T) {
	// Header of 3 lines, footer of 2 lines: headerLines=4, footerLines=3,
	// promptLines=2 (fixed), so fixedLines=9 and filesPerPage=termHeight-9.
	top := []byte("line1\nline2\nline3\n")
	bot := []byte("foot1\nfoot2\n")

	got := computeFilePagination(80, 24, top, bot)
	if got != 15 {
		t.Fatalf("computeFilePagination(80, 24, ...) = %d, want 15", got)
	}

	small := computeFilePagination(80, 10, top, bot)
	if small != 1 {
		t.Fatalf("computeFilePagination(80, 10, ...) = %d, want 1", small)
	}
	if small >= got {
		t.Fatalf("smaller terminal must page fewer files: %d >= %d", small, got)
	}
}

func TestComputeFilePagination_ClampsToAtLeastOne(t *testing.T) {
	top := []byte("line1\nline2\nline3\n")
	bot := []byte("foot1\nfoot2\n")

	// termHeight smaller than fixedLines (9) must still yield at least 1.
	got := computeFilePagination(80, 1, top, bot)
	if got != 1 {
		t.Fatalf("computeFilePagination(80, 1, ...) = %d, want 1 (clamped)", got)
	}
}

func TestFormatFileListLine_TokenSubstitution(t *testing.T) {
	u := &user.User{}
	rec := file.FileRecord{
		ID:          uuid.New(),
		Filename:    "TESTFILE.ZIP",
		Description: "A single line description",
		Size:        2048,
		UploadedAt:  time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
	}
	midTemplate := "^MARK^NUM ^NAME ^DATE ^SIZE ^DESC"

	line, cont := formatFileListLine(rec, u, false, midTemplate, 7)

	if len(cont) != 0 {
		t.Fatalf("expected no continuation lines for single-line description, got %d: %v", len(cont), cont)
	}
	if !strings.HasPrefix(line, " 7 ") {
		t.Errorf("expected unmarked line to start with ' 7 ' (mark+num), got %q", line)
	}
	if !strings.Contains(line, "TESTFILE.ZIP") {
		t.Errorf("expected filename in line, got %q", line)
	}
	if !strings.Contains(line, "03/15/24") {
		t.Errorf("expected formatted date in line, got %q", line)
	}
	if !strings.Contains(line, "2k") {
		t.Errorf("expected formatted size in line, got %q", line)
	}
	if !strings.Contains(line, "A single line description") {
		t.Errorf("expected description in line, got %q", line)
	}
}

func TestFormatFileListLine_MarkedFile(t *testing.T) {
	id := uuid.New()
	u := &user.User{TaggedFileIDs: []uuid.UUID{id}}
	rec := file.FileRecord{ID: id, Filename: "X.ZIP", Size: 1024, UploadedAt: time.Now()}

	line, _ := formatFileListLine(rec, u, false, "^MARK^NUM", 1)

	if !strings.HasPrefix(line, "*") {
		t.Errorf("expected tagged file line to start with '*', got %q", line)
	}
}

func TestFormatFileListLine_MultibyteFilenameTruncation(t *testing.T) {
	u := &user.User{}
	rec := file.FileRecord{
		Filename:   "abcdefgh日本語.zip",
		Size:       1024,
		UploadedAt: time.Now(),
	}

	line, _ := formatFileListLine(rec, u, false, "^NAME", 1)

	if !utf8.ValidString(line) {
		t.Fatalf("expected valid UTF-8 output from truncated multibyte filename, got invalid string %q", line)
	}

	want := string([]rune(rec.Filename)[:12])
	if got := strings.TrimRight(line, " "); got != want {
		t.Errorf("expected filename truncated to 12 runes %q, got %q (full line %q)", want, got, line)
	}
}

func TestFormatFileListLine_MultilineDIZContinuations(t *testing.T) {
	u := &user.User{}
	rec := file.FileRecord{
		Filename:    "MULTI.ZIP",
		Description: "Line one\nLine two\nLine three",
		Size:        1024,
		UploadedAt:  time.Now(),
	}
	midTemplate := "^MARK^NUM ^NAME ^DATE ^SIZE ^DESC"

	line, cont := formatFileListLine(rec, u, false, midTemplate, 1)

	if !strings.Contains(line, "Line one") {
		t.Fatalf("expected first DIZ line embedded via ^DESC, got %q", line)
	}
	if len(cont) != 2 {
		t.Fatalf("expected 2 continuation lines, got %d: %v", len(cont), cont)
	}
	if !strings.Contains(cont[0], "Line two") {
		t.Errorf("expected continuation[0] to contain %q, got %q", "Line two", cont[0])
	}
	if !strings.Contains(cont[1], "Line three") {
		t.Errorf("expected continuation[1] to contain %q, got %q", "Line three", cont[1])
	}
	if !strings.HasPrefix(cont[0], "|07") {
		t.Errorf("expected continuation to be prefixed with |07 color code, got %q", cont[0])
	}
}
