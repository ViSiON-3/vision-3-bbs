package atomicfile

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
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

// TestReplaceWhileDestinationIsOpen is the Windows case stated directly: a
// rename over a file someone else holds open is refused there, and Replace has
// to wait them out.
//
// The hold has to outlast at least one retry interval, or the test passes
// whether or not Replace retries at all -- releasing the handle immediately
// lets even a single attempt succeed. Holding for several intervals means a
// non-retrying Replace fails here on Windows.
//
// On unix an open handle never blocks a rename, so this passes trivially.
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

	f, err := os.Open(dst)
	if err != nil {
		t.Fatal(err)
	}

	// Hold it well past a single retry interval, but comfortably inside the
	// total budget so Replace still has attempts left when it is released.
	hold := 4 * replacePause
	released := make(chan struct{})
	go func() {
		time.Sleep(hold)
		_ = f.Close()
		close(released)
	}()

	start := time.Now()
	err = Replace(src, dst)
	<-released

	if err != nil {
		t.Fatalf("Replace failed while the destination was briefly open (GOOS=%s): %v",
			runtime.GOOS, err)
	}
	if got, err := os.ReadFile(dst); err != nil || string(got) != "new" {
		t.Errorf("dst = %q (err %v), want %q", got, err, "new")
	}
	// On Windows the call cannot have succeeded before the handle went away,
	// which is the retry doing its job. Elsewhere it should not have waited.
	waited := time.Since(start)
	if runtime.GOOS == "windows" && waited < hold {
		t.Errorf("Replace returned after %v, before the handle was released at %v; "+
			"it cannot have been blocked, so this test is not exercising the retry",
			waited, hold)
	}
}
