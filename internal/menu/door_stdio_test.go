package menu

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// failingWriter accepts n bytes and then fails.
type failingWriter struct {
	n   int
	got bytes.Buffer
}

var errDoorGone = errors.New("door gone")

func (w *failingWriter) Write(p []byte) (int, error) {
	if len(p) <= w.n {
		w.n -= len(p)
		return w.got.Write(p)
	}
	n := w.n
	w.got.Write(p[:n])
	w.n = 0
	return n, errDoorGone
}

// copyDoorInput hands back the keys a door never got, in order (#750).
func TestCopyDoorInput(t *testing.T) {
	t.Run("copies until end of input", func(t *testing.T) {
		var w bytes.Buffer
		unread, err := copyDoorInput(&w, strings.NewReader("abc"), nil)
		if unread != nil || err != nil || w.String() != "abc" {
			t.Errorf("copyDoorInput = %q, %v, wrote %q; want nil, nil, \"abc\"", unread, err, w.String())
		}
	})

	t.Run("keeps a chunk read after the door exited", func(t *testing.T) {
		exited := make(chan struct{})
		close(exited)
		var w bytes.Buffer
		unread, err := copyDoorInput(&w, strings.NewReader("abc"), exited)
		if string(unread) != "abc" || err != nil || w.Len() != 0 {
			t.Errorf("copyDoorInput = %q, %v, wrote %q; want \"abc\", nil, nothing", unread, err, w.String())
		}
	})

	t.Run("keeps what a failed write left", func(t *testing.T) {
		w := &failingWriter{n: 1}
		unread, err := copyDoorInput(w, strings.NewReader("abc"), nil)
		if string(unread) != "bc" || !errors.Is(err, errDoorGone) || w.got.String() != "a" {
			t.Errorf("copyDoorInput = %q, %v, wrote %q; want \"bc\", %v, \"a\"", unread, err, w.got.String(), errDoorGone)
		}
	})

	t.Run("passes on a read error", func(t *testing.T) {
		var w bytes.Buffer
		r := io.MultiReader(strings.NewReader("ab"), &errReader{errDoorGone})
		unread, err := copyDoorInput(&w, r, nil)
		if unread != nil || !errors.Is(err, errDoorGone) || w.String() != "ab" {
			t.Errorf("copyDoorInput = %q, %v, wrote %q; want nil, %v, \"ab\"", unread, err, w.String(), errDoorGone)
		}
	})
}

// errReader fails every read with err.
type errReader struct{ err error }

func (r *errReader) Read([]byte) (int, error) { return 0, r.err }
