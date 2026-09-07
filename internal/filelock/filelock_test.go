package filelock

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireAndRelease(t *testing.T) {
	data := filepath.Join(t.TempDir(), "users.json")

	lock, err := Acquire(data, time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	lock.Release()

	// Releasing frees it for the next taker.
	again, err := Acquire(data, time.Second)
	if err != nil {
		t.Fatalf("Acquire after Release: %v", err)
	}
	again.Release()
}

// flock and LockFileEx are both per open file description, so two independent
// acquires contend even inside one process. That is what makes this a real
// exclusion test rather than a check that the code runs.
func TestSecondAcquireWaitsForTheFirst(t *testing.T) {
	data := filepath.Join(t.TempDir(), "users.json")

	held, err := Acquire(data, time.Second)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}

	if _, err := Acquire(data, 50*time.Millisecond); err == nil {
		t.Fatal("second Acquire succeeded while the lock was held")
	}

	held.Release()
	second, err := Acquire(data, time.Second)
	if err != nil {
		t.Fatalf("Acquire after the holder released: %v", err)
	}
	second.Release()
}

// A blocked acquire has to give up rather than hang, so a wedged editor cannot
// stall every save on the BBS indefinitely.
func TestAcquireHonoursItsTimeout(t *testing.T) {
	data := filepath.Join(t.TempDir(), "users.json")

	held, err := Acquire(data, time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer held.Release()

	start := time.Now()
	if _, err := Acquire(data, 150*time.Millisecond); err == nil {
		t.Fatal("expected a timeout error")
	}
	waited := time.Since(start)
	if waited < 150*time.Millisecond {
		t.Errorf("gave up after %s, before the %s timeout", waited, 150*time.Millisecond)
	}
	if waited > 2*time.Second {
		t.Errorf("waited %s, far longer than the timeout", waited)
	}
}

// Release must tolerate a nil receiver so callers can defer it beside an
// acquire whose failure they handle by continuing unlocked -- which is what
// the BBS save path does.
func TestReleaseOnNilLockIsSafe(t *testing.T) {
	var l *Lock
	l.Release()
}

func TestSidecarPathIsNotTheDataFile(t *testing.T) {
	if got := SidecarPath("/data/users.json"); got != "/data/users.json.lock" {
		t.Errorf("SidecarPath = %q", got)
	}
}

// The lock has to hold across processes; that is its entire purpose, and an
// in-process test cannot prove it. This re-runs the test binary as a child
// that takes the lock and holds it, then checks the parent is excluded.
func TestLockExcludesASeparateProcess(t *testing.T) {
	if os.Getenv("FILELOCK_CHILD_HOLD") != "" {
		holdLockForChild()
		return
	}

	dir := t.TempDir()
	data := filepath.Join(dir, "users.json")
	ready := filepath.Join(dir, "ready")

	cmd := exec.Command(os.Args[0], "-test.run", "TestLockExcludesASeparateProcess")
	cmd.Env = append(os.Environ(),
		"FILELOCK_CHILD_HOLD="+data,
		"FILELOCK_CHILD_READY="+ready,
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	defer func() { _ = cmd.Wait() }()

	// Wait for the child to signal it holds the lock.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child never signalled that it had the lock")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := Acquire(data, 200*time.Millisecond); err == nil {
		t.Error("acquired the lock while another process held it")
	}
}

// holdLockForChild runs in the child process: take the lock, say so, hold it
// briefly, exit.
func holdLockForChild() {
	lock, err := Acquire(os.Getenv("FILELOCK_CHILD_HOLD"), 5*time.Second)
	if err != nil {
		os.Exit(1)
	}
	defer lock.Release()
	_ = os.WriteFile(os.Getenv("FILELOCK_CHILD_READY"), []byte("held"), 0600)
	time.Sleep(1500 * time.Millisecond)
}
