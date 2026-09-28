// Package session holds the state of each connected caller (BbsSession) and
// the SessionRegistry that indexes the live sessions by node number. The
// server registers a session when a caller connects and removes it on
// disconnect; who's online, node paging, chat, the admin views and V3 scripts
// read the registry to see who else is on.
package session

import (
	"net"
	"sync"
	"time"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/term"

	// "github.com/ViSiON-3/vision-3-bbs/internal/menu" // Removed menu import
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
	PendingPages []string             // Queued page messages for delivery at next prompt
	Invisible    bool                 // True if user logged in invisibly (SysOp/CoSysOp only)
}

// AddPage queues a page message for delivery at the user's next menu prompt.
func (s *BbsSession) AddPage(msg string) {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	s.PendingPages = append(s.PendingPages, msg)
}

// DrainPages returns all pending pages and clears the queue.
func (s *BbsSession) DrainPages() []string {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	if len(s.PendingPages) == 0 {
		return nil
	}
	pages := s.PendingPages
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
