package syncjs

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// mockSession implements io.ReadWriter for testing.
type mockSession struct {
	input  *bytes.Buffer
	output *bytes.Buffer
}

func (m *mockSession) Read(p []byte) (int, error)  { return m.input.Read(p) }
func (m *mockSession) Write(p []byte) (int, error) { return m.output.Write(p) }

func newTestEngine(t *testing.T, script string) (*Engine, *mockSession) {
	t.Helper()

	tmpDir := t.TempDir()
	if script != "" {
		if err := os.WriteFile(filepath.Join(tmpDir, "test.js"), []byte(script), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mock := &mockSession{
		input:  bytes.NewBuffer(nil),
		output: bytes.NewBuffer(nil),
	}

	session := &SessionContext{
		Session:          mock,
		OutputMode:       ansi.OutputModeCP437,
		UserID:           1,
		UserHandle:       "TestUser",
		UserRealName:     "Test User",
		AccessLevel:      100,
		TimeLimit:        60,
		TimesCalled:      42,
		Location:         "Testville",
		ScreenWidth:      80,
		ScreenHeight:     24,
		NodeNumber:       1,
		SessionStartTime: time.Now(),
		BoardName:        "Test BBS",
		SysOpName:        "SysOp",
	}

	cfg := SyncJSDoorConfig{
		Script:     "test.js",
		WorkingDir: tmpDir,
		ExecDir:    tmpDir,
		DataDir:    tmpDir,
		NodeDir:    tmpDir,
	}

	eng := NewEngine(context.Background(), session, cfg)
	return eng, mock
}

func TestEngineRunSimpleScript(t *testing.T) {
	eng, mock := newTestEngine(t, `console.write("Hello BBS");`)
	defer eng.Close()

	err := eng.Run("test.js")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	output := mock.output.String()
	if !strings.Contains(output, "Hello BBS") {
		t.Errorf("expected output to contain 'Hello BBS', got %q", output)
	}
}

func TestEngineUserObject(t *testing.T) {
	eng, mock := newTestEngine(t, `console.write(user.alias + ":" + user.number);`)
	defer eng.Close()

	err := eng.Run("test.js")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	output := mock.output.String()
	if !strings.Contains(output, "TestUser:1") {
		t.Errorf("expected 'TestUser:1', got %q", output)
	}
}

func TestEngineSystemObject(t *testing.T) {
	eng, mock := newTestEngine(t, `console.write(system.name);`)
	defer eng.Close()

	err := eng.Run("test.js")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	output := mock.output.String()
	if !strings.Contains(output, "Test BBS") {
		t.Errorf("expected 'Test BBS', got %q", output)
	}
}

func TestEngineBBSObject(t *testing.T) {
	eng, mock := newTestEngine(t, `
		console.write("node:" + bbs.node_num);
		console.write(" online:" + bbs.online);
		var tl = bbs.get_time_left();
		console.write(" timeleft:" + (tl > 0));
	`)
	defer eng.Close()

	err := eng.Run("test.js")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	output := mock.output.String()
	if !strings.Contains(output, "node:1") {
		t.Errorf("expected 'node:1' in output, got %q", output)
	}
	if !strings.Contains(output, "online:true") {
		t.Errorf("expected 'online:true' in output, got %q", output)
	}
	if !strings.Contains(output, "timeleft:true") {
		t.Errorf("expected 'timeleft:true' in output, got %q", output)
	}
}

func TestEngineExit(t *testing.T) {
	eng, _ := newTestEngine(t, `
		console.write("before");
		exit(0);
		console.write("after");
	`)
	defer eng.Close()

	err := eng.Run("test.js")
	if err != nil {
		t.Fatalf("Run should succeed on clean exit, got: %v", err)
	}
}

func TestEngineModuleLoad(t *testing.T) {
	eng, mock := newTestEngine(t, "")
	defer eng.Close()

	// Create a library file
	libDir := eng.cfg.WorkingDir
	os.WriteFile(filepath.Join(libDir, "mylib.js"), []byte(`var MYCONST = 42;`), 0o644)
	os.WriteFile(filepath.Join(libDir, "test.js"), []byte(`
		load("mylib.js");
		console.write("val:" + MYCONST);
	`), 0o644)

	err := eng.Run("test.js")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	output := mock.output.String()
	if !strings.Contains(output, "val:42") {
		t.Errorf("expected 'val:42', got %q", output)
	}
}

func TestEngineFileClass(t *testing.T) {
	eng, mock := newTestEngine(t, "")
	defer eng.Close()

	os.WriteFile(filepath.Join(eng.cfg.WorkingDir, "test.js"), []byte(`
		var f = new File("testdata.txt");
		f.open("w");
		f.write("Hello File");
		f.close();

		f.open("r");
		var content = f.read();
		f.close();
		console.write("read:" + content);
	`), 0o644)

	err := eng.Run("test.js")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	output := mock.output.String()
	if !strings.Contains(output, "read:Hello File") {
		t.Errorf("expected 'read:Hello File', got %q", output)
	}
}

func TestEngineGlobalFunctions(t *testing.T) {
	eng, mock := newTestEngine(t, `
		var r = random(100);
		console.write("rand:" + (r >= 0 && r < 100));
		var ts = time();
		console.write(" time:" + (ts > 0));
		var s = format("num=%d str=%s", 42, "hi");
		console.write(" fmt:" + s);
	`)
	defer eng.Close()

	err := eng.Run("test.js")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	output := mock.output.String()
	if !strings.Contains(output, "rand:true") {
		t.Errorf("expected 'rand:true', got %q", output)
	}
	if !strings.Contains(output, "time:true") {
		t.Errorf("expected 'time:true', got %q", output)
	}
	if !strings.Contains(output, "fmt:num=42 str=hi") {
		t.Errorf("expected 'fmt:num=42 str=hi', got %q", output)
	}
}

func TestEngineConsoleAttributes(t *testing.T) {
	eng, mock := newTestEngine(t, `
		console.attributes = 0x07;
		console.write("gray");
		console.attributes = 0x0C;
		console.write("bright red");
	`)
	defer eng.Close()

	err := eng.Run("test.js")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	output := mock.output.String()
	// Should contain ANSI SGR sequences before text
	if !strings.Contains(output, "gray") || !strings.Contains(output, "bright red") {
		t.Errorf("expected text output, got %q", output)
	}
	if !strings.Contains(output, "\x1b[") {
		t.Errorf("expected ANSI escape sequences, got %q", output)
	}
}

func TestEngineConsoleGotoxy(t *testing.T) {
	eng, mock := newTestEngine(t, `console.gotoxy(10, 5);`)
	defer eng.Close()

	eng.Run("test.js")

	output := mock.output.String()
	if !strings.Contains(output, "\x1b[5;10H") {
		t.Errorf("expected cursor position escape, got %q", output)
	}
}

func TestEngineFileExists(t *testing.T) {
	eng, mock := newTestEngine(t, "")
	defer eng.Close()

	// Create a test file
	os.WriteFile(filepath.Join(eng.cfg.WorkingDir, "exists.txt"), []byte("hi"), 0o644)
	os.WriteFile(filepath.Join(eng.cfg.WorkingDir, "test.js"), []byte(`
		console.write("e:" + file_exists("exists.txt"));
		console.write(" ne:" + file_exists("nope.txt"));
	`), 0o644)

	err := eng.Run("test.js")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	output := mock.output.String()
	if !strings.Contains(output, "e:true") {
		t.Errorf("expected 'e:true', got %q", output)
	}
	if !strings.Contains(output, "ne:false") {
		t.Errorf("expected 'ne:false', got %q", output)
	}
}

func TestEngineLiveLoadPathList(t *testing.T) {
	eng, mock := newTestEngine(t, "")
	defer eng.Close()

	// Create a subdirectory with a module
	subDir := filepath.Join(eng.cfg.WorkingDir, "sublib")
	os.MkdirAll(subDir, 0o755)
	os.WriteFile(filepath.Join(subDir, "helper.js"), []byte(`var HELPER_VAL = 99;`), 0o644)

	// Main script mutates js.load_path_list at runtime, then requires the module
	os.WriteFile(filepath.Join(eng.cfg.WorkingDir, "test.js"), []byte(`
		js.load_path_list.unshift("`+subDir+`/");
		load("helper.js");
		console.write("helper:" + HELPER_VAL);
		js.load_path_list.shift();
	`), 0o644)

	err := eng.Run("test.js")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	output := mock.output.String()
	if !strings.Contains(output, "helper:99") {
		t.Errorf("expected 'helper:99', got %q", output)
	}
}
