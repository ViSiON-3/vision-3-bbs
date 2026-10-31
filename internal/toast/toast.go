// Package toast holds short notices for a user account. The store keeps the
// ones that must outlive a session on disk until they have been shown; the
// session and menu packages deliver them.
package toast

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
)

// Toast is one notice: a line of text, pipe codes included, and an optional
// body such as the text of a refused post.
type Toast struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Body      string    `json:"body,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Store is the on-disk queue of toasts, per user ID, in the order sent.
type Store struct {
	path string

	mu      sync.Mutex
	claimed map[string]bool // IDs a session is showing now; not persisted
}

// stores holds one Store per path, so every sender on the node shares its lock.
var stores sync.Map

// Open returns the store for the given data dir, falling back to the
// conventional "data" dir when none is configured.
func Open(dataDir string) *Store {
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "data"
	}
	path := filepath.Join(dataDir, "toasts.json")
	v, _ := stores.LoadOrStore(path, &Store{path: path, claimed: map[string]bool{}})
	return v.(*Store)
}

func (st *Store) load() (map[int][]Toast, error) {
	data, err := os.ReadFile(st.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[int][]Toast{}, nil
		}
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return map[int][]Toast{}, nil
	}
	var m map[int][]Toast
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[int][]Toast{}
	}
	return m, nil
}

func (st *Store) save(m map[int][]Toast) error {
	if err := os.MkdirAll(filepath.Dir(st.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(st.path, data, 0o644)
}

// Enqueue appends t to one user's queue, filling in its ID and CreatedAt when
// they are unset.
func (st *Store) Enqueue(userID int, t Toast) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	m, err := st.load()
	if err != nil {
		return err
	}
	if t.ID == "" {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return err
		}
		t.ID = hex.EncodeToString(b[:])
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	m[userID] = append(m[userID], t)
	return st.save(m)
}

// Peek returns one user's queued toasts without removing them, so a caller can
// show them and remove each only once it has been delivered.
func (st *Store) Peek(userID int) ([]Toast, error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	m, err := st.load()
	if err != nil {
		return nil, err
	}
	return m[userID], nil
}

// Remove drops one toast from a user's queue, keeping the rest.
func (st *Store) Remove(userID int, id string) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	m, err := st.load()
	if err != nil {
		return err
	}
	var kept []Toast
	for _, t := range m[userID] {
		if t.ID != id {
			kept = append(kept, t)
		}
	}
	if len(kept) == len(m[userID]) {
		return nil
	}
	if len(kept) == 0 {
		delete(m, userID)
	} else {
		m[userID] = kept
	}
	return st.save(m)
}

// Claim reports whether the caller may show this toast of userID's, so two
// sessions on one account do not both show it. A toast already removed cannot be
// claimed: the other session has shown it, and the caller's Peek was stale. Pair
// it with Release.
func (st *Store) Claim(userID int, id string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.claimed[id] {
		return false
	}
	m, err := st.load()
	if err != nil {
		return false
	}
	for _, t := range m[userID] {
		if t.ID == id {
			st.claimed[id] = true
			return true
		}
	}
	return false
}

// Release ends a Claim.
func (st *Store) Release(id string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.claimed, id)
}
