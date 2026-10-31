// Package session holds the state of each connected caller (BbsSession) and
// the SessionRegistry that indexes the live sessions by node number. The
// server registers a session when a caller connects and removes it on
// disconnect; who's online, node paging, chat, the admin views and V3 scripts
// read the registry to see who else is on.
package session

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/term"

	// "github.com/ViSiON-3/vision-3-bbs/internal/menu" // Removed menu import
	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
	"github.com/ViSiON-3/vision-3-bbs/internal/types" // Import the new types package
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	// Remove main import main "github.com/ViSiON-3/vision-3-bbs"
)

// BbsSession represents one caller's connection to the BBS: the SSH channel
// and terminal it talks through, the logged-in user (nil until login), and the
// node, menu and activity details shown to other callers. Fields that change
// while the session runs are guarded by Mutex.
type BbsSession struct {
	ID           int // Unique identifier for the session/node
	Conn         gossh.Conn
	Channel      gossh.Channel // Store the SSH channel for direct I/O
	Term         *term.Terminal
	User         *user.User // Logged-in user, nil if not logged in
	Width        int
	Height       int
	RemoteAddr   net.Addr
	CurrentMenu  string               // Tracks the current ViSiON/2 menu the user is in
	Activity     string               // Descriptive activity text for Who's Online (e.g., "Reading Messages")
	NodeID       int                  // Node ID for the session
	AssetsPath   string               // Store required path directly
	Mutex        sync.RWMutex         // For thread-safe access to session state if needed later
	Pty          *ssh.Pty             // Store PTY info
	AutoRunLog   types.AutoRunTracker // Tracks run-once commands executed (Use types.AutoRunTracker)
	LastMenu     string               // Tracks the previously visited menu
	StartTime    time.Time            // Tracks the session start time
	LastActivity time.Time            // Tracks last user input for idle calculation
	PendingPages []pendingPage        // Live-only toasts waiting to be shown; not kept across a restart
	Invisible    bool                 // True if user logged in invisibly (SysOp/CoSysOp only)
	Tap          *snoop.Tap           // WFC snoop point for this node; nil in tests
	ChatCredit   func() time.Duration // Sysop chat time credited to the caller; nil means none
	// Size returns the caller's live terminal size. The SSH session handler
	// sets it; sessions built without it (tests) report Width and Height.
	Size func() (width, height int)

	toastWake       chan struct{}   // see ToastWake; guarded by Mutex
	toastsReady     atomic.Bool     // see SetToastsReady
	toastsAnnounced map[string]bool // see NoteToastAnnounced; guarded by Mutex
	toastAnchorRow  int             // see SetToastAnchor; guarded by Mutex
	toastAnchorCol  int             // see SetToastAnchor; guarded by Mutex
	toastsShown     map[string]bool // see NoteToastShown; guarded by Mutex
	heldPages       []string        // see HoldPage; guarded by Mutex
}

// TermSize returns the caller's current terminal size.
func (s *BbsSession) TermSize() (width, height int) {
	s.Mutex.RLock()
	size, width, height := s.Size, s.Width, s.Height
	s.Mutex.RUnlock()
	if size != nil {
		return size()
	}
	return width, height
}

// pendingPage is one live-only toast. A zero expires never goes stale.
type pendingPage struct {
	text    string
	expires time.Time
}

// AddPage queues a live-only toast for this session and wakes its key wait.
func (s *BbsSession) AddPage(msg string) { s.addPage(pendingPage{text: msg}) }

// AddExpiringPage is AddPage for news that is worthless after ttl, such as a
// logon announcement to a caller who is in a door.
func (s *BbsSession) AddExpiringPage(msg string, ttl time.Duration) {
	s.addPage(pendingPage{text: msg, expires: time.Now().Add(ttl)})
}

func (s *BbsSession) addPage(p pendingPage) {
	s.Mutex.Lock()
	s.PendingPages = append(s.PendingPages, p)
	s.Mutex.Unlock()
	s.WakeToasts()
}

// dropStalePagesLocked removes expired pages. Caller holds Mutex for writing.
func (s *BbsSession) dropStalePagesLocked() {
	now := time.Now()
	kept := s.PendingPages[:0]
	for _, p := range s.PendingPages {
		if p.expires.IsZero() || now.Before(p.expires) {
			kept = append(kept, p)
		}
	}
	s.PendingPages = kept
}

// PeekPage returns the oldest pending page without removing it.
func (s *BbsSession) PeekPage() (string, bool) {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	s.dropStalePagesLocked()
	if len(s.PendingPages) == 0 {
		return "", false
	}
	return s.PendingPages[0].text, true
}

// PopPage removes the oldest pending page.
func (s *BbsSession) PopPage() {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	if len(s.PendingPages) > 0 {
		s.PendingPages = s.PendingPages[1:]
	}
}

// HoldPage takes the oldest pending page out of the overlay's way: it has been
// drawn cut short, so the whole of it waits for the next inline delivery.
func (s *BbsSession) HoldPage() {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	if len(s.PendingPages) > 0 {
		// An announcement is old news by the next menu; what was drawn will do.
		if p := s.PendingPages[0]; p.expires.IsZero() {
			s.heldPages = append(s.heldPages, p.text)
		}
		s.PendingPages = s.PendingPages[1:]
	}
}

// PeekHeldPage returns the oldest page HoldPage kept, without removing it.
func (s *BbsSession) PeekHeldPage() (string, bool) {
	s.Mutex.RLock()
	defer s.Mutex.RUnlock()
	if len(s.heldPages) == 0 {
		return "", false
	}
	return s.heldPages[0], true
}

// PopHeldPage removes the oldest page HoldPage kept.
func (s *BbsSession) PopHeldPage() {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	if len(s.heldPages) > 0 {
		s.heldPages = s.heldPages[1:]
	}
}

// DrainPages returns all pending pages and clears the queue.
func (s *BbsSession) DrainPages() []string {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	s.dropStalePagesLocked()
	if len(s.PendingPages) == 0 {
		return nil
	}
	pages := make([]string, len(s.PendingPages))
	for i, p := range s.PendingPages {
		pages[i] = p.text
	}
	s.PendingPages = nil
	return pages
}

// NewSession creates a new Session object.
// func NewSession(id int, conn ssh.Conn, term *term.Terminal, width, height int, remoteAddr net.Addr) *Session {
// 	return &Session{
// 		ID:          id,
// 		Conn:        conn,
// 		Term:        term,
// 		Width:       width,
// 		Height:      height,
// 		RemoteAddr:  remoteAddr,
// 		CurrentMenu: "", // Initialize CurrentMenu
// 	}
// }

// TODO: Implement methods for managing session state, e.g.:
// func (s *Session) SetUser(u *user.User) { ... }
// func (s *Session) GetUser() *user.User { ... }
// func (s *Session) SetCurrentMenu(menuName string) { ... }
// func (s *Session) GetCurrentMenu() string { ... }
