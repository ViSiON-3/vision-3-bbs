package scheduler

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// TestMain lets a copy of the test binary stand in for an event's process and
// for the child it leaves behind; see orphanEnv.
func TestMain(m *testing.M) {
	switch os.Getenv(orphanEnv) {
	case "event":
		orphanEvent()
	case "child":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// orphanEnv tells a copy of the test binary which part it plays for
// TestExecuteEvent_OrphanedChildDoesNotBlockCompletion: "event" is the
// process executeEvent runs, "child" the one it leaves behind.
const orphanEnv = "VISION_SCHEDULER_TEST_ORPHAN"

// orphanEvent is the event's process. It starts a child that inherits its
// stdout and sleeps, prints the child's pid for the test to kill, and exits
// at once, leaving the child holding the event's output pipe.
func orphanEvent() {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	child := exec.Command(self)
	child.Env = append(os.Environ(), orphanEnv+"=child")
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("started %d\n", child.Process.Pid)
	os.Exit(0)
}

// testEventTimeout is the timeout for events that are expected to finish on
// their own. It only has to outlast a slow CI runner: a shell on the Windows
// runner has taken over 5 s to start and exit, and the timeout then reported
// the event as killed (exit code -1) instead of its real result.
const testEventTimeout = 60

// lookPath finds the absolute path to a command, skipping the test if not found.
func lookPath(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("command %q not found in PATH, skipping", name)
	}
	return path
}

func TestExecuteEvent_Success(t *testing.T) {
	s := &Scheduler{}

	event := config.EventConfig{
		ID:             "test_success",
		Name:           "Test Success Event",
		Command:        lookPath(t, "echo"),
		Args:           []string{"Hello, World!"},
		TimeoutSeconds: testEventTimeout,
	}

	result := s.executeEvent(context.Background(), event)

	if !result.Success {
		t.Errorf("Expected success, got failure: %v", result.Error)
	}

	if result.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", result.ExitCode)
	}

	if !strings.Contains(result.Output, "Hello, World!") {
		t.Errorf("Expected output to contain 'Hello, World!', got: %s", result.Output)
	}
}

func TestExecuteEvent_Failure(t *testing.T) {
	s := &Scheduler{}

	shPath := lookPath(t, "sh")

	event := config.EventConfig{
		ID:             "test_failure",
		Name:           "Test Failure Event",
		Command:        shPath,
		Args:           []string{"-c", "exit 1"},
		TimeoutSeconds: testEventTimeout,
	}

	result := s.executeEvent(context.Background(), event)

	if result.Success {
		t.Error("Expected failure, got success")
	}

	if result.ExitCode != 1 {
		t.Errorf("Expected exit code 1, got %d", result.ExitCode)
	}
}

func TestExecuteEvent_Timeout(t *testing.T) {
	s := &Scheduler{}

	event := config.EventConfig{
		ID:             "test_timeout",
		Name:           "Test Timeout Event",
		Command:        lookPath(t, "sleep"),
		Args:           []string{"10"},
		TimeoutSeconds: 1,
	}

	start := time.Now()
	result := s.executeEvent(context.Background(), event)
	duration := time.Since(start)

	if result.Success {
		t.Error("Expected failure due to timeout, got success")
	}

	if duration > 2*time.Second {
		t.Errorf("Expected timeout after ~1 second, took %v", duration)
	}
}

func TestExecuteEvent_WithEnvironmentVars(t *testing.T) {
	s := &Scheduler{}

	event := config.EventConfig{
		ID:      "test_env",
		Name:    "Test Environment Variables",
		Command: lookPath(t, "sh"),
		Args:    []string{"-c", "echo $TEST_VAR"},
		EnvironmentVars: map[string]string{
			"TEST_VAR": "test_value",
		},
		TimeoutSeconds: testEventTimeout,
	}

	result := s.executeEvent(context.Background(), event)

	if !result.Success {
		t.Errorf("Expected success, got failure: %v", result.Error)
	}

	if !strings.Contains(result.Output, "test_value") {
		t.Errorf("Expected output to contain 'test_value', got: %s", result.Output)
	}
}

func TestBuildSubstitutions(t *testing.T) {
	s := &Scheduler{}

	event := config.EventConfig{
		ID:   "test_placeholders",
		Name: "Test Placeholders Event",
	}

	subs := s.buildSubstitutions(event)

	expectedKeys := []string{"{TIMESTAMP}", "{EVENT_ID}", "{EVENT_NAME}", "{BBS_ROOT}", "{DATE}", "{TIME}", "{DATETIME}"}
	for _, key := range expectedKeys {
		if _, exists := subs[key]; !exists {
			t.Errorf("Expected placeholder %s to exist", key)
		}
	}

	if subs["{EVENT_ID}"] != "test_placeholders" {
		t.Errorf("Expected EVENT_ID to be 'test_placeholders', got %s", subs["{EVENT_ID}"])
	}

	if subs["{EVENT_NAME}"] != "Test Placeholders Event" {
		t.Errorf("Expected EVENT_NAME to be 'Test Placeholders Event', got %s", subs["{EVENT_NAME}"])
	}
}

func TestPlaceholderSubstitutionInWorkingDirectory(t *testing.T) {
	s := &Scheduler{}

	// Create a temp directory so the working directory actually exists
	tmpDir := t.TempDir()

	event := config.EventConfig{
		ID:               "test_workdir",
		Name:             "Test Working Directory Substitution",
		Command:          lookPath(t, "pwd"),
		WorkingDirectory: tmpDir,
		TimeoutSeconds:   testEventTimeout,
	}

	result := s.executeEvent(context.Background(), event)

	if !result.Success {
		t.Errorf("Expected success, got failure: %v", result.Error)
	}

	if strings.Contains(result.Output, "{BBS_ROOT}") {
		t.Error("Working directory placeholder was not substituted")
	}
}

func TestPlaceholderSubstitutionInCommand(t *testing.T) {
	s := &Scheduler{}

	echoPath := lookPath(t, "echo")

	event := config.EventConfig{
		ID:             "test_cmd_sub",
		Name:           "Test Command Path Substitution",
		Command:        "{BBS_ROOT}/nonexistent_should_be_replaced",
		Args:           []string{"hello"},
		TimeoutSeconds: testEventTimeout,
	}

	// The substituted command won't exist, so it should fail with a path that
	// does NOT contain the literal placeholder.
	result := s.executeEvent(context.Background(), event)

	if result.Error != nil && strings.Contains(result.Error.Error(), "{BBS_ROOT}") {
		t.Error("Command placeholder was not substituted")
	}

	// Now test with a valid command using the placeholder mechanism.
	// We'll set the command to the real echo path (no placeholder) to confirm
	// normal execution still works after the refactor.
	event2 := config.EventConfig{
		ID:             "test_cmd_real",
		Name:           "Test Real Command Execution",
		Command:        echoPath,
		Args:           []string{"placeholder_test_ok"},
		TimeoutSeconds: testEventTimeout,
	}

	result2 := s.executeEvent(context.Background(), event2)
	if !result2.Success {
		t.Errorf("Expected success, got failure: %v", result2.Error)
	}
	if !strings.Contains(result2.Output, "placeholder_test_ok") {
		t.Errorf("Expected output to contain 'placeholder_test_ok', got: %s", result2.Output)
	}
}

// An event that leaves a background child behind exits promptly itself, but
// the child inherits the output pipes. cmd.Run used to block until that child
// exited too, holding the event's concurrency slot and running-event mark for
// however long the orphan lived, and a timeout kill did not help because it
// reaches only the process we started.
//
// The event and its child are copies of this test binary (see TestMain), so
// the pid the event prints is the child's own on every OS. The event used to
// be a shell running "sleep 30 &" and printing $!, which under the MSYS
// shell on the Windows runner is not a Windows pid: the child did not hold
// the pipe there, so the test proved nothing, and killing that number twice
// ended the test binary itself with no output (#930).
func TestExecuteEvent_OrphanedChildDoesNotBlockCompletion(t *testing.T) {
	s := &Scheduler{}

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	event := config.EventConfig{
		ID:              "test_orphan",
		Name:            "Test Orphaned Child",
		Command:         self,
		EnvironmentVars: map[string]string{orphanEnv: "event"},
	}

	start := time.Now()
	result := s.executeEvent(context.Background(), event)
	duration := time.Since(start)

	if !result.Success {
		t.Errorf("the event's process exited 0, so the event should succeed: exit=%d err=%v", result.ExitCode, result.Error)
	}
	fields := strings.Fields(result.Output)
	if len(fields) != 2 || fields[0] != "started" {
		t.Fatalf("output written before exit must be kept, got %q", result.Output)
	}
	pid, err := strconv.Atoi(fields[1])
	if err != nil {
		t.Fatalf("the event did not print its child's pid: %q", result.Output)
	}
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill() // do not leak the orphan past the test
	}
	// The child held the pipe, so the run lasted the wait delay and no
	// longer: shorter means the scenario never arose and the test proved
	// nothing, longer means the orphan held the event open.
	if duration < outputWaitDelay {
		t.Errorf("event took %v; the orphaned child did not hold its output open", duration)
	}
	if duration > outputWaitDelay+5*time.Second {
		t.Errorf("event took %v; the orphaned child must not hold it past the wait delay", duration)
	}
}
