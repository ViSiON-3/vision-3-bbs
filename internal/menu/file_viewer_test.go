package menu

import (
	"archive/zip"
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
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
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

// addFileWithContent adds a record named name to areaID of env's file
// manager and writes content to its on-disk path, returning the record ID.
func addFileWithContent(t *testing.T, env *menuEnv, areaID int, name string, content []byte) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := env.e.FileMgr.AddFileRecord(file.FileRecord{
		ID: id, AreaID: areaID, Filename: name, Description: "desc of " + name,
		Size: int64(len(content)), UploadedAt: time.Date(2026, 5, 6, 7, 8, 0, 0, time.UTC), UploadedBy: "Sysop",
	}); err != nil {
		t.Fatalf("AddFileRecord: %v", err)
	}
	p, err := env.e.FileMgr.GetFilePath(id)
	if err != nil {
		t.Fatalf("GetFilePath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return id
}

// zipBytes builds an in-memory zip archive holding the named entries.
func zipBytes(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestViewFileShowsTextFile pins VIEW_FILE on a plain text file: the name is
// matched case-insensitively in the current area and the file is shown
// between the viewing header and end-of-file marker.
func TestViewFileShowsTextFile(t *testing.T) {
	env := newMenuEnv(t)
	addFileWithContent(t, env, 1, "README.TXT", []byte("first line\nsecond line\n"))
	env.caller.CurrentFileAreaID = 1

	r := env.runCmd("VIEW_FILE", env.caller, "", "readme.txt\r\r")
	if r.err != nil || r.user != env.caller {
		t.Fatalf("result = (%v, %v)", r.user, r.err)
	}
	if !r.has("Viewing: README.TXT", "first line", "second line", "End of File") {
		t.Errorf("text file not shown:\n%s", r.text())
	}
}

// TestViewFilePagesLongFile pins that a file longer than the screen stops
// at a More prompt: Q there ends the listing before the end-of-file marker,
// and Space/Enter page on through to it.
func TestViewFilePagesLongFile(t *testing.T) {
	env := newMenuEnv(t)
	var sb strings.Builder
	for i := 1; i <= 60; i++ {
		fmt.Fprintf(&sb, "row %02d\n", i)
	}
	addFileWithContent(t, env, 1, "LONG.TXT", []byte(sb.String()))
	env.caller.CurrentFileAreaID = 1

	r := env.runCmd("VIEW_FILE", env.caller, "", "LONG.TXT\rq")
	if !r.has("row 01", "MORE") || r.has("End of File") {
		t.Errorf("Q at the More prompt should stop paging:\n%s", r.text())
	}

	r = env.runCmd("VIEW_FILE", env.caller, "", "LONG.TXT\r"+strings.Repeat(" ", 20)+"\r")
	if n := strings.Count(r.text(), "MORE"); n < 2 || !r.has("End of File") {
		t.Errorf("paging through: %d More prompts, want several, then the end:\n%s", n, r.text())
	}
}

// TestViewFileListsArchive pins that VIEW_FILE on a zip shows the archive's
// entries through ZipLab rather than dumping the bytes.
func TestViewFileListsArchive(t *testing.T) {
	env := newMenuEnv(t)
	addFileWithContent(t, env, 1, "PACK.ZIP", zipBytes(t, map[string]string{"INSIDE.DOC": "hello"}))
	env.caller.CurrentFileAreaID = 1

	r := env.run(runViewFile, env.caller, "", "PACK.ZIP\rQ\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("INSIDE.DOC", "ZipLab") {
		t.Errorf("archive listing missing:\n%s", r.text())
	}
}

// TestViewFilePromptEdgeCases pins VIEW_FILE's early exits: no user, no
// area selected, a blank name, an unknown name, and a disconnect.
func TestViewFilePromptEdgeCases(t *testing.T) {
	env := newMenuEnv(t)
	addFileWithContent(t, env, 1, "README.TXT", []byte("x\n"))

	if r := env.runCmd("VIEW_FILE", nil, "", "README.TXT\r"); r.user != nil || r.raw != "" {
		t.Errorf("no user: user=%v output=%q, want nothing", r.user, r.raw)
	}

	r := env.runCmd("VIEW_FILE", env.caller, "", "README.TXT\r")
	if !strings.Contains(r.text(), stripPipes(env.e.Strings().FileNoAreaSelected)) {
		t.Errorf("no area: missing notice:\n%s", r.text())
	}

	env.caller.CurrentFileAreaID = 1
	if r := env.runCmd("VIEW_FILE", env.caller, "", "\r"); r.user != env.caller || r.has("Viewing") {
		t.Errorf("blank name should just return:\n%s", r.text())
	}
	r = env.runCmd("VIEW_FILE", env.caller, "", "NOPE.TXT\r")
	if !r.has("NOPE.TXT") || r.has("Viewing") {
		t.Errorf("unknown file: want not-found notice:\n%s", r.text())
	}
	if r := env.runCmd("VIEW_FILE", env.caller, "", ""); r.next != "LOGOFF" {
		t.Errorf("disconnect: next = %q, want LOGOFF", r.next)
	}
}

// TestTypeTextFileShowsFileAsText pins TYPE_TEXT_FILE: even an archive is
// typed out raw with paging rather than listed through ZipLab.
func TestTypeTextFileShowsFileAsText(t *testing.T) {
	env := newMenuEnv(t)
	addFileWithContent(t, env, 1, "NOTES.TXT", []byte("typed content\n"))
	addFileWithContent(t, env, 1, "PACK.ZIP", zipBytes(t, map[string]string{"INSIDE.DOC": "hello"}))
	env.caller.CurrentFileAreaID = 1

	r := env.runCmd("TYPE_TEXT_FILE", env.caller, "", "notes.txt\r\r")
	if !r.has("Viewing: NOTES.TXT", "typed content", "End of File") {
		t.Errorf("text not typed:\n%s", r.text())
	}
	r = env.runCmd("TYPE_TEXT_FILE", env.caller, "", "PACK.ZIP\rq")
	if r.has("ZipLab") || !r.has("Viewing: PACK.ZIP", "PK") {
		t.Errorf("archive should be typed raw, not listed:\n%s", r.text())
	}
	if r := env.runCmd("TYPE_TEXT_FILE", env.caller, "", "\r"); r.has("Viewing") {
		t.Errorf("blank name should just return:\n%s", r.text())
	}
}

// TestViewFileByRecordMissingPath pins that viewing a record whose area has
// vanished reports the locate error instead of opening anything.
func TestViewFileByRecordMissingPath(t *testing.T) {
	env := newMenuEnv(t)
	rec := &file.FileRecord{ID: uuid.New(), AreaID: 99, Filename: "GONE.TXT"}
	r := env.run(func(c *cmdCtx, _ string) (*user.User, string, error) {
		viewFileByRecord(c.e, c.s, c.terminal, rec, c.outputMode, 80, 0, nil)
		return nil, "", nil
	}, env.caller, "", "")
	if !strings.Contains(r.text(), stripPipes(env.e.Strings().FileLocateError)) {
		t.Errorf("missing locate error:\n%s", r.text())
	}
}

// TestDisplayTextWithPagingRefusesHugeFile pins the 4 MB guard: an oversized
// file is refused with the open error rather than read into memory.
func TestDisplayTextWithPagingRefusesHugeFile(t *testing.T) {
	env := newMenuEnv(t)
	p := filepath.Join(t.TempDir(), "big.txt")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxTextFilePagingBytes + 1); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	r := env.run(func(c *cmdCtx, _ string) (*user.User, string, error) {
		displayTextWithPaging(c.s, c.terminal, p, "big.txt", c.outputMode, 24, "Viewing %s", "EOF", "More", "Pause", "OPEN ERROR")
		return nil, "", nil
	}, env.caller, "", "")
	if !r.has("OPEN ERROR") || r.has("Viewing") {
		t.Errorf("huge file not refused:\n%s", r.text())
	}
}

// stripPipes renders a configured string's pipe codes and strips the
// resulting escapes, giving the plain text a session shows for it.
func stripPipes(s string) string {
	return strings.TrimSpace(testAnsiEscape.ReplaceAllString(string(ansi.ReplacePipeCodes([]byte(s))), ""))
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

// TestViewMisnamedArchive checks that command and file-list View routes
// list a ZIP with a nonstandard suffix through ZipLab.
func TestViewMisnamedArchive(t *testing.T) {
	for _, byRecord := range []bool{false, true} {
		t.Run(fmt.Sprint(byRecord), func(t *testing.T) {
			env := newMenuEnv(t)
			addFileWithContent(t, env, 1, "NODELIST.Z75", zipBytes(t, map[string]string{"NODELIST.275": "hello"}))
			env.caller.CurrentFileAreaID = 1
			fn := runViewFile
			input := "NODELIST.Z75\rQ\r"
			if byRecord {
				input = "Q\r"
				fn = func(c *cmdCtx, _ string) (*user.User, string, error) {
					rec, err := findFileInArea(c.e.FileMgr, 1, "NODELIST.Z75")
					if err != nil {
						return nil, "", err
					}
					viewFileByRecord(c.e, c.s, c.terminal, rec, c.outputMode, 80, 24, nil)
					return c.currentUser, "", nil
				}
			}
			r := env.run(fn, env.caller, "", input)
			if r.err != nil || !r.has("ZipLab", "NODELIST.275") || r.has("Viewing:") {
				t.Fatalf("misnamed ZIP: err=%v\n%s", r.err, r.text())
			}
		})
	}
}

// TestViewCorruptMisnamedArchive checks that a detected but corrupt ZIP
// reports an archive-reading error instead of displaying its bytes as text.
func TestViewCorruptMisnamedArchive(t *testing.T) {
	env := newMenuEnv(t)
	addFileWithContent(t, env, 1, "BROKEN.Z75", []byte("PK\x03\x04corrupt"))
	env.caller.CurrentFileAreaID = 1
	r := env.run(runViewFile, env.caller, "", "BROKEN.Z75\r\r")
	if r.err != nil || !r.has("Error reading archive") || r.has("Viewing:") {
		t.Fatalf("corrupt ZIP: err=%v\n%s", r.err, r.text())
	}
}
