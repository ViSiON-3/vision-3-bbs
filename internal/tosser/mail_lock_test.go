package tosser

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/filelock"
)

// While another process holds the FTN mail lock (a toss binkd started, or the
// BBS's export cycle), a toss, scan or pack waits and then reports an error
// rather than working on the same bases and packets at the same time.
func TestTossScanPackWaitForMailLock(t *testing.T) {
	orig := mailLockTimeout
	mailLockTimeout = 50 * time.Millisecond
	t.Cleanup(func() { mailLockTimeout = orig })

	env := setupTestEnv(t)
	tr, err := New("testnet", env.netCfg, env.globalCfg, env.dupeDB, env.msgMgr)
	if err != nil {
		t.Fatal(err)
	}
	held, err := filelock.Acquire(filepath.Join(filepath.Dir(filepath.Clean(tr.paths.InboundPath)), "ftn_mail"), 0)
	if err != nil {
		t.Fatal(err)
	}

	busy := func(what string, errs []string) {
		t.Helper()
		if len(errs) != 1 || !strings.Contains(errs[0], "still running") {
			t.Errorf("%s while locked: errors %v, want one saying another run is still going", what, errs)
		}
	}
	busy("toss", tr.ProcessInbound().Errors)
	busy("scan", tr.ScanAndExport().Errors)
	busy("pack", tr.PackOutbound().Errors)

	held.Release()
	if errs := tr.ScanAndExport().Errors; len(errs) != 0 {
		t.Errorf("scan after the lock was released: %v", errs)
	}
}
