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

// TestIntroCubeRuns plays the whole cube intro (grow-in, spin, shrink-out)
// to completion and checks it never throws.
func TestIntroCubeRuns(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "scripts", "examples", "intro.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(filepath.Join(scriptsDir, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "intro.js"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	sess := &demandPipeSession{Reader: reader}
	sc := &SessionContext{
		Session: sess, OutputMode: ansi.OutputModeCP437, ScreenWidth: 80, ScreenHeight: 24,
		NodeNumber: 1, SessionStartTime: time.Now(), BoardName: "Test BBS", BBSVersion: "t",
	}
	eng := NewEngine(context.Background(), sc,
		ScriptConfig{Script: "intro.js", WorkingDir: scriptsDir, Args: []string{"4"}}, nil)
	defer func() { _ = reader.Close(); _ = writer.Close(); eng.Close() }()

	errc := make(chan error, 1)
	go func() { errc <- eng.Run("intro.js") }()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("intro errored: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("intro did not finish")
	}
}
