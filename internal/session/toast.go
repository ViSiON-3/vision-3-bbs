package session

import (
	"log/slog"

	"github.com/ViSiON-3/vision-3-bbs/internal/toast"
)

// ToastWake returns the channel that fires when this session may have a toast
// to show. It is buffered, so a wake is kept until a key wait takes it.
func (s *BbsSession) ToastWake() <-chan struct{} {
	return s.toastWakeChan()
}

func (s *BbsSession) toastWakeChan() chan struct{} {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	if s.toastWake == nil {
		s.toastWake = make(chan struct{}, 1)
	}
	return s.toastWake
}

// WakeToasts tells the session's key wait to look for toasts.
func (s *BbsSession) WakeToasts() {
	select {
	case s.toastWakeChan() <- struct{}{}:
	default:
	}
}

// SetToastsReady marks whether the session can show toasts on screen. It is
// false until the login sequence has finished, so none lands on login art.
func (s *BbsSession) SetToastsReady(ready bool) { s.toastsReady.Store(ready) }

// ToastsReady reports what SetToastsReady last set.
func (s *BbsSession) ToastsReady() bool { return s.toastsReady.Load() }

// NoteToastAnnounced records that this session has drawn a toast's line but not
// yet all of it, its body or a line too long for the row, and reports whether it
// is the first time.
func (s *BbsSession) NoteToastAnnounced(id string) bool {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	if s.toastsAnnounced[id] {
		return false
	}
	if s.toastsAnnounced == nil {
		s.toastsAnnounced = map[string]bool{}
	}
	s.toastsAnnounced[id] = true
	return true
}

// ToastAnnounced reports whether NoteToastAnnounced has seen id.
func (s *BbsSession) ToastAnnounced(id string) bool {
	s.Mutex.RLock()
	defer s.Mutex.RUnlock()
	return s.toastsAnnounced[id]
}

// NoteToastShown records that this session has shown all of a toast but could
// not remove it from the queue, so it is not shown again while the removal is
// retried.
func (s *BbsSession) NoteToastShown(id string) {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	if s.toastsShown == nil {
		s.toastsShown = map[string]bool{}
	}
	s.toastsShown[id] = true
}

// ToastShown reports whether NoteToastShown has seen id.
func (s *BbsSession) ToastShown(id string) bool {
	s.Mutex.RLock()
	defer s.Mutex.RUnlock()
	return s.toastsShown[id]
}

// SendToast queues a toast for a user account and wakes any session they have.
// The disk comes first, so a toast cannot be lost with a session that ends
// before showing it. If the write fails, online sessions still get its line.
func (r *SessionRegistry) SendToast(store *toast.Store, userID int, t toast.Toast) error {
	err := store.Enqueue(userID, t)
	if err != nil {
		slog.Warn("failed to queue toast", "user", userID, "error", err)
	}
	for _, sess := range r.ListActive() {
		sess.Mutex.RLock()
		match := sess.User != nil && sess.User.ID == userID
		sess.Mutex.RUnlock()
		if !match {
			continue
		}
		if err != nil {
			sess.AddPage(t.Text)
		} else {
			sess.WakeToasts()
		}
	}
	return err
}
