package scripting

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// fakeOneliners is an in-memory OnelinerStore for the script test.
type fakeOneliners struct{ items []OnelinerView }

func (f *fakeOneliners) Recent(limit int) []OnelinerView {
	out := []OnelinerView{}
	for i := len(f.items) - 1; i >= 0; i-- {
		if limit > 0 && len(out) == limit {
			break
		}
		out = append(out, f.items[i])
	}
	return out
}

func (f *fakeOneliners) Add(handle, username, text string, anonymous bool) error {
	f.items = append(f.items, OnelinerView{Text: text, Handle: handle, Anonymous: anonymous, PostedAt: time.Now()})
	return nil
}

// TestOnelinersAnimationRuns loads the real oneliners.js, lets its idle
// loop run (catching globals the wave/twinkle need that are defined after
// the blocking lightbar() call), drives the add flow, then quits.
func TestOnelinersAnimationRuns(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "scripts", "examples", "oneliners.js"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := newUserProviders(t)
	fake := &fakeOneliners{items: []OnelinerView{
		{Text: "first post", Handle: "Alice", PostedAt: time.Now().Add(-48 * time.Hour)},
		{Text: "hello world", Handle: "Bob", PostedAt: time.Now().Add(-time.Hour)},
	}}
	p.Oneliners = fake

	dir := t.TempDir()
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(filepath.Join(scriptsDir, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "oneliners.js"), src, 0o644); err != nil {
		t.Fatal(err)
	}

	reader, writer := io.Pipe()
	sess := &demandPipeSession{Reader: reader}
	now := time.Now()
	sc := &SessionContext{
		Session: sess, OutputMode: ansi.OutputModeCP437, ScreenWidth: 80, ScreenHeight: 24,
		UserID: 7, UserHandle: "Tester", NodeNumber: 1, SessionStartTime: now,
		BoardName: "Test BBS", SysOpName: "Sy", BBSVersion: "t",
	}
	eng := NewEngine(context.Background(), sc, ScriptConfig{Script: "oneliners.js", WorkingDir: scriptsDir}, p)
	defer func() { _ = reader.Close(); _ = writer.Close(); eng.Close() }()

	// Let a few frames run, open the add prompt, type a line, then quit.
	go func() {
		time.Sleep(1500 * time.Millisecond)
		_, _ = writer.Write([]byte("a"))
		time.Sleep(700 * time.Millisecond)
		_, _ = writer.Write([]byte("a fresh line\r"))
		time.Sleep(1500 * time.Millisecond)
		_, _ = writer.Write([]byte("q"))
	}()
	errc := make(chan error, 1)
	go func() { errc <- eng.Run("oneliners.js") }()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("oneliners animation errored: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("oneliners did not exit after quit key")
	}

	if len(fake.items) != 3 {
		t.Fatalf("add flow did not store the new line: have %d items", len(fake.items))
	}
	if fake.items[2].Text != "a fresh line" {
		t.Errorf("stored text = %q, want %q", fake.items[2].Text, "a fresh line")
	}
}
