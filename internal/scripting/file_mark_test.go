package scripting

import (
	"fmt"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/google/uuid"
)

// markHarness is fileReadHarness with a caller whose record the batch queue
// calls change, stored in a user manager on disk.
func markHarness(t *testing.T) (*scriptHarness, *user.User, *user.UserMgr) {
	t.Helper()
	p := fileAreasProviders(t, func(a *file.FileArea) bool { return a.ACSDownload == "" })
	um, err := user.NewUserManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewUserManager: %v", err)
	}
	u, err := um.AddUser("password", "Caller", "Caller", "Here")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	p.UserMgr, p.CurrentUser = um, u
	return newHarness(t, harnessOpts{providers: p}), u, um
}

func TestFileMark(t *testing.T) {
	h, u, um := markHarness(t)
	steps := []struct{ expr, want string }{
		{`v3.file.isMarked(1, "ACID.ZIP")`, "false"},
		{`v3.file.mark(1, "acid.zip")`, "true"},
		{`v3.file.isMarked(1, "ACID.ZIP")`, "true"},
		{`v3.file.mark(1, "ACID.ZIP")`, "true"}, // already there: still one entry
		{`v3.file.mark(1, "README.TXT")`, "true"},
		// the caller can't download from PRIV, and there's no such file
		{`v3.file.mark(2, "SECRET.TXT")`, "false"},
		{`v3.file.mark(1, "MISSING.ZIP")`, "false"},
		{`v3.file.mark(1)`, "false"},
		{`v3.file.unmark(1, "ACID.ZIP")`, "true"},
		{`v3.file.unmark(1, "ACID.ZIP")`, "false"},
		{`v3.file.isMarked(1, "ACID.ZIP")`, "false"},
	}
	for _, st := range steps {
		if got := h.eval(st.expr).String(); got != st.want {
			t.Fatalf("%s = %s, want %s", st.expr, got, st.want)
		}
	}
	if len(u.TaggedFileIDs) != 1 {
		t.Fatalf("live record has %d marked files, want 1 (README.TXT)", len(u.TaggedFileIDs))
	}
	saved, ok := um.GetUserByID(u.ID)
	if !ok || len(saved.TaggedFileIDs) != 1 || saved.TaggedFileIDs[0] != u.TaggedFileIDs[0] {
		t.Errorf("saved record's marks = %v, want %v", saved.TaggedFileIDs, u.TaggedFileIDs)
	}
}

// mark refuses a file once the queue is full, as DOWNLOADFILE does, and
// leaves the queue as it was.
func TestFileMarkQueueFull(t *testing.T) {
	h, u, _ := markHarness(t)
	for i := 0; i < file.MaxBatchFiles; i++ {
		u.TaggedFileIDs = append(u.TaggedFileIDs, uuid.New())
	}
	if got := h.eval(`v3.file.mark(1, "ACID.ZIP")`).String(); got != "false" {
		t.Errorf("mark with a full queue = %s, want false", got)
	}
	if len(u.TaggedFileIDs) != file.MaxBatchFiles {
		t.Errorf("queue has %d files, want %d", len(u.TaggedFileIDs), file.MaxBatchFiles)
	}
}

// Without the caller's record (or a download check, for mark) the calls
// change nothing.
func TestFileMarkNeedsCaller(t *testing.T) {
	h := fileReadHarness(t, func(a *file.FileArea) bool { return true })
	for _, call := range []string{"isMarked", "mark", "unmark"} {
		expr := fmt.Sprintf(`v3.file.%s(1, "ACID.ZIP")`, call)
		if got := h.eval(expr).String(); got != "false" {
			t.Errorf("%s with no caller = %s, want false", expr, got)
		}
	}
	h2, _, _ := markHarness(t)
	h2.eng.providers.CanDownload = nil
	if got := h2.eval(`v3.file.mark(1, "ACID.ZIP")`).String(); got != "false" {
		t.Errorf("mark with no CanDownload = %s, want false", got)
	}
}
