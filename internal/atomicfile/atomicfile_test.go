package atomicfile

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

// TestWriteFileReplacesContents covers the ordinary case.
func TestWriteFileReplacesContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("new"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("contents = %q, want %q", got, "new")
	}
}

// TestWriteFileCreatesMissingFile covers first write.
func TestWriteFileCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.json")
	if err := WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("contents = %q", got)
	}
}

// TestWriteFileLeavesNoTempBehind checks the directory is not littered, on
// success or on failure.
func TestWriteFileLeavesNoTempBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	if err := WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "data.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v, want just data.json", names)
	}
}

// TestWriteFileIsNeverObservedPartiallyWritten is the property the package
// exists for, and the one that fails on Windows without the retry in
// replace_windows.go: a reader polling the file while it is rewritten must
// always see complete, valid contents, and the writer must never fail because
// a reader happened to have it open.
func TestWriteFileIsNeverObservedPartiallyWritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")

	payload := func(n int) []byte {
		b, err := json.Marshal(map[string]any{"n": n, "pad": bytes.Repeat([]byte("x"), 4096)})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if err := WriteFile(path, payload(0), 0o600); err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	bad := make(chan string, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			data, err := os.ReadFile(path)
			if err != nil {
				continue // only ever replaced, never absent for long
			}
			var out map[string]any
			if err := json.Unmarshal(data, &out); err != nil {
				select {
				case bad <- "reader saw a partially written file: " + err.Error():
				default:
				}
				return
			}
		}
	}()

	for i := 1; i <= 200; i++ {
		if err := WriteFile(path, payload(i), 0o600); err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("write %d failed while a reader had the file open: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()

	select {
	case msg := <-bad:
		t.Error(msg)
	default:
	}
}

// TestReplaceOverExistingFile covers Replace on its own.
func TestReplaceOverExistingFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Replace(src, dst); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("dst = %q, want %q", got, "new")
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("src still exists after Replace")
	}
}

// TestReplaceWhileDestinationIsOpen is the Windows case stated directly. On
// unix an open handle never blocks a rename, so this passes trivially there;
// on Windows it fails without the retry.
func TestReplaceWhileDestinationIsOpen(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Hold the destination open, then let go while Replace is retrying.
	f, err := os.Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- Replace(src, dst) }()

	// Close after long enough that Windows has refused at least once.
	closed := make(chan struct{})
	go func() { _ = f.Close(); close(closed) }()
	<-closed

	if err := <-done; err != nil {
		t.Errorf("Replace failed with the destination briefly open (GOOS=%s): %v",
			runtime.GOOS, err)
	}
}
