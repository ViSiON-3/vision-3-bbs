package menu

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/util"
	"github.com/google/uuid"
)

// setupTestFileManagerForViewer creates a FileManager with temp dirs, areas, and optional files on disk.
func setupTestFileManagerForViewer(t *testing.T, areas []file.FileArea) (*file.FileManager, string) {
	t.Helper()

	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	configDir := filepath.Join(tmpDir, "configs")
	os.MkdirAll(dataDir, 0755)
	os.MkdirAll(configDir, 0755)

	data, err := json.Marshal(areas)
	if err != nil {
		t.Fatalf("failed to marshal test areas: %v", err)
	}
	os.WriteFile(filepath.Join(configDir, "file_areas.json"), data, 0644)

	fm, err := file.NewFileManager(dataDir, configDir)
	if err != nil {
		t.Fatalf("failed to create FileManager: %v", err)
	}

	// Create area directories
	for _, area := range areas {
		os.MkdirAll(filepath.Join(dataDir, "files", area.Path), 0755)
	}

	return fm, filepath.Join(dataDir, "files")
}

func TestFindFileInArea_ExactMatch(t *testing.T) {
	areas := []file.FileArea{
		{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"},
	}
	fm, _ := setupTestFileManagerForViewer(t, areas)

	// Add a file record
	rec := file.FileRecord{
		ID:         uuid.New(),
		AreaID:     1,
		Filename:   "README.TXT",
		Size:       100,
		UploadedAt: time.Now(),
		UploadedBy: "TestUser",
	}
	err := fm.AddFileRecord(rec)
	if err != nil {
		t.Fatalf("failed to add file record: %v", err)
	}

	// Exact match
	found, err := findFileInArea(fm, 1, "README.TXT")
	if err != nil {
		t.Errorf("expected to find file, got error: %v", err)
	}
	if found == nil || found.Filename != "README.TXT" {
		t.Errorf("expected README.TXT, got %v", found)
	}
}

func TestFindFileInArea_CaseInsensitive(t *testing.T) {
	areas := []file.FileArea{
		{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"},
	}
	fm, _ := setupTestFileManagerForViewer(t, areas)

	rec := file.FileRecord{
		ID:         uuid.New(),
		AreaID:     1,
		Filename:   "README.TXT",
		Size:       100,
		UploadedAt: time.Now(),
		UploadedBy: "TestUser",
	}
	fm.AddFileRecord(rec)

	// Case-insensitive search
	found, err := findFileInArea(fm, 1, "readme.txt")
	if err != nil {
		t.Errorf("expected case-insensitive match, got error: %v", err)
	}
	if found == nil || found.Filename != "README.TXT" {
		t.Errorf("expected README.TXT, got %v", found)
	}
}

func TestFindFileInArea_NotFound(t *testing.T) {
	areas := []file.FileArea{
		{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"},
	}
	fm, _ := setupTestFileManagerForViewer(t, areas)

	_, err := findFileInArea(fm, 1, "NOFILE.ZIP")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestFindFileInArea_EmptyArea(t *testing.T) {
	areas := []file.FileArea{
		{ID: 1, Tag: "UTILS", Name: "Utilities", Path: "utils"},
	}
	fm, _ := setupTestFileManagerForViewer(t, areas)

	_, err := findFileInArea(fm, 1, "anything.txt")
	if err == nil {
		t.Error("expected error for empty area, got nil")
	}
}

func TestFormatFileSize(t *testing.T) {
	tests := []struct {
		size     int64
		expected string
	}{
		{0, "0"},
		{512, "512"},
		{1023, "1023"},
		{1024, "1.0K"},
		{1536, "1.5K"},
		{1048576, "1.0M"},
		{1572864, "1.5M"},
		{1073741824, "1.0G"},
	}

	for _, tt := range tests {
		result := util.FormatFileSize(tt.size)
		if result != tt.expected {
			t.Errorf("util.FormatFileSize(%d) = %q, want %q", tt.size, result, tt.expected)
		}
	}
}

func TestDisplayTextWithPaging_MissingFile(t *testing.T) {
	var buf bytes.Buffer
	displayTextWithPaging_toWriter(&buf, "/nonexistent/file.txt", "nope.txt", 24)

	output := buf.String()
	if !strings.Contains(output, "Error opening file") {
		t.Errorf("expected error message for missing file, got: %s", output)
	}
}

func TestDisplayTextWithPaging_SmallFile(t *testing.T) {
	// Create a small test file (fits in one screen)
	tmpDir := t.TempDir()
	textPath := filepath.Join(tmpDir, "small.txt")
	content := "Line 1\nLine 2\nLine 3\n"
	os.WriteFile(textPath, []byte(content), 0644)

	var buf bytes.Buffer
	displayTextWithPaging_toWriter(&buf, textPath, "small.txt", 24)

	output := buf.String()
	if !strings.Contains(output, "Line 1") {
		t.Errorf("expected output to contain 'Line 1', got: %s", output)
	}
	if !strings.Contains(output, "Line 3") {
		t.Errorf("expected output to contain 'Line 3', got: %s", output)
	}
	if !strings.Contains(output, "End of File") {
		t.Errorf("expected output to contain end-of-file marker, got: %s", output)
	}
}

func TestDisplayTextWithPaging_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	textPath := filepath.Join(tmpDir, "empty.txt")
	os.WriteFile(textPath, []byte(""), 0644)

	var buf bytes.Buffer
	displayTextWithPaging_toWriter(&buf, textPath, "empty.txt", 24)

	output := buf.String()
	if !strings.Contains(output, "Viewing: empty.txt") {
		t.Errorf("expected header with filename, got: %s", output)
	}
	if !strings.Contains(output, "End of File") {
		t.Errorf("expected end-of-file marker for empty file, got: %s", output)
	}
}

func TestViewFileByRecord_RegistrationExists(t *testing.T) {
	// Verify VIEW_FILE and TYPE_TEXT_FILE are registered commands
	registry := make(map[string]RunnableFunc)
	registerAppRunnables(registry)

	if _, ok := registry["VIEW_FILE"]; !ok {
		t.Error("VIEW_FILE not registered in command registry")
	}
	if _, ok := registry["TYPE_TEXT_FILE"]; !ok {
		t.Error("TYPE_TEXT_FILE not registered in command registry")
	}
}

// TestDisplayTextWithPaging_ShowsEveryLineAndPagesCorrectly drives the real
// session viewer (VIEW_FILE / TYPE_TEXT_FILE / file-list view). Appending CRLF
// to scanner.Bytes() in place used to overwrite the start of the next line in
// the scanner's buffer, so only the first line survived and the More-prompt
// count was off (#461).
func TestDisplayTextWithPaging_ShowsEveryLineAndPagesCorrectly(t *testing.T) {
	const numLines = 12
	var content strings.Builder
	for i := 1; i <= numLines; i++ {
		fmt.Fprintf(&content, "LINE-%02d text\n", i)
	}
	textPath := filepath.Join(t.TempDir(), "readme.txt")
	if err := os.WriteFile(textPath, []byte(content.String()), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// termHeight 9 gives 5 lines per page, so 12 lines pause after lines 5
	// and 10: exactly two More prompts. Space continues each, CR ends the
	// closing pause.
	ts := newTestSession("  \r")
	t.Cleanup(func() { resetSessionIH(ts) })
	terminal := newTestTerminal(ts)

	displayTextWithPaging(ts, terminal, textPath, "readme.txt", ansi.OutputModeCP437, 9,
		"Viewing: %s\r\n", "<EOF>", "<MORE>", "<PAUSE>", "<OPENERR>")

	out := ts.output()
	prev := -1
	for i := 1; i <= numLines; i++ {
		want := fmt.Sprintf("LINE-%02d text\r", i)
		idx := strings.Index(out, want)
		if idx < 0 {
			t.Fatalf("output missing %q:\n%q", want, out)
		}
		if idx <= prev {
			t.Errorf("%q out of order in output", want)
		}
		prev = idx
	}
	if got := strings.Count(out, "<MORE>"); got != 2 {
		t.Errorf("More prompt shown %d times, want 2:\n%q", got, out)
	}
	if !strings.Contains(out, "<EOF>") {
		t.Errorf("end-of-file marker missing:\n%q", out)
	}
}
