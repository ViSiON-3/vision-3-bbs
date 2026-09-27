package tosser

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/filelock"
)

// mailLockTimeout is how long a toss, scan or pack waits for another to
// finish. A large toss can take a while; after this the caller reports an
// error and the work is picked up by its next run. Variable so tests can
// shorten it.
var mailLockTimeout = 2 * time.Minute

// lockMail takes the lock that runs FTN tosses, scans and packs one at a time
// across processes: the BBS's export cycle, v3mail toss/scan/ftn-pack (binkd
// runs toss after each session) and v3mail poll. Without it, two scans of the
// same JAM bases can both export a message before either marks it sent, and
// two tosses can import the same packet. Every network shares the lock, as
// they share the inbound, the staging outbound and the dupe database.
//
// It returns a no-op release when no inbound or outbound is configured, since
// there is then nothing on disk to contend over.
func (t *Tosser) lockMail() (release func(), err error) {
	dir := t.paths.InboundPath
	if dir == "" {
		dir = t.paths.OutboundPath
	}
	if dir == "" {
		return func() {}, nil
	}
	lock, err := filelock.Acquire(filepath.Join(filepath.Dir(filepath.Clean(dir)), "ftn_mail"), mailLockTimeout)
	if err != nil {
		return nil, fmt.Errorf("another FTN toss, scan or pack is still running: %w", err)
	}
	return lock.Release, nil
}
