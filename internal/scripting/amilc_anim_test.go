package scripting

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TestAmilcAnimationRuns loads the real amilc.js and lets its idle loop
// time out so the twinkle, wave and scroller actually run, then quits.
// It catches globals used by the animation but defined after the blocking
// lightbar() call, which the replay-driven test never reaches.
func TestAmilcAnimationRuns(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "scripts", "examples", "amilc.js"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := newUserProviders(t)
	now := time.Now()
	p.UserMgr.AddCallRecord(user.CallRecord{UserID: 7, Handle: "Tester", NodeID: 1,
		ConnectTime: now.Add(-time.Hour), DisconnectTime: now.Add(-time.Hour).Add(5 * time.Minute)})

	dir := t.TempDir()
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(filepath.Join(scriptsDir, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "amilc.js"), src, 0o644); err != nil {
		t.Fatal(err)
	}

	reader, writer := io.Pipe()
	sess := &demandPipeSession{Reader: reader}
	sc := &SessionContext{
		Session: sess, OutputMode: ansi.OutputModeCP437, ScreenWidth: 80, ScreenHeight: 24,
		UserID: 7, UserHandle: "Tester", NodeNumber: 1, SessionStartTime: now,
		BoardName: "Test BBS", SysOpName: "Sy", BBSVersion: "t",
	}
	eng := NewEngine(context.Background(), sc, ScriptConfig{Script: "amilc.js", WorkingDir: scriptsDir}, p)
	defer func() { _ = reader.Close(); _ = writer.Close(); eng.Close() }()

	// Let several animation frames run, then quit.
	go func() {
		time.Sleep(4000 * time.Millisecond)
		_, _ = writer.Write([]byte("q"))
	}()
	errc := make(chan error, 1)
	go func() { errc <- eng.Run("amilc.js") }()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("amilc animation errored: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("amilc did not exit after quit key")
	}
}
