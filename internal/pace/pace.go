// Package pace keeps the picture frames the board sends a caller's terminal
// in step with what the terminal takes: no more than InFlight frames go
// before the terminal answers for them.
package pace

import (
	"sync"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// Query asks the terminal for its primary device attributes. Sent after a
// picture frame, its answer says the terminal has read the frame: a
// terminal reads its input in order and answers a query when it reads it.
// The session's reply trap (editor.ReplyTrap) takes device attributes
// replies out of the input all session, and no key has their shape.
const Query = ansi.DAPrimaryQuery

// InFlight is how many frames a terminal may have unanswered. Two lets the
// board draw the next frame while the terminal draws the last, so a
// terminal that takes about a frame's time to draw one keeps the frame
// rate it gets unpaced.
const InFlight = 2

// Gate lets picture frames go to a terminal only while fewer than InFlight
// are unanswered: each frame is followed by Query, and each answer, which
// a terminal sends in order, answers the oldest frame. One gate serves a
// session for its life, across the screens that draw pictures. A nil gate
// lets every frame go and sends no query.
type Gate struct {
	held       func() bool // see HoldWhile
	mu         sync.Mutex
	unanswered int // frames sent whose answers have not come
}

// NewGate returns a gate with no frame outstanding.
func NewGate() *Gate { return &Gate{} }

// HoldWhile has Begin refuse every frame while held reports true: while
// another reply to Query may still come, a frame's answer could not be
// told from it. It is set before the gate is shared.
func (g *Gate) HoldWhile(held func() bool) { g.held = held }

// Begin reports whether a frame may be sent now. When it returns true the
// caller sends the frame followed by g.Query(); while InFlight frames are
// unanswered, Begin returns false and the caller skips its frames, keeping
// what it last sent as the picture the terminal has.
func (g *Gate) Begin() bool {
	if g == nil {
		return true
	}
	if g.held != nil && g.held() {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.unanswered >= InFlight {
		return false
	}
	g.unanswered++
	return true
}

// Query returns the bytes to send after a frame: Query, or none for a nil
// gate.
func (g *Gate) Query() []byte {
	if g == nil {
		return nil
	}
	return []byte(Query)
}

// Waiting reports whether a frame's answer is still to come.
func (g *Gate) Waiting() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.unanswered > 0
}

// full reports whether InFlight frames are unanswered.
func (g *Gate) full() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.unanswered >= InFlight
}

// Ready reports whether Begin would let a frame go now: no answer is due
// and HoldWhile does not hold it.
func (g *Gate) Ready() bool {
	if g == nil {
		return true
	}
	if g.held != nil && g.held() {
		return false
	}
	return !g.full()
}

// Answer takes the terminal's answer to Query for the oldest unanswered
// frame. It reports whether an answer was awaited; one that was not is not
// g's.
func (g *Gate) Answer() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.unanswered == 0 {
		return false
	}
	g.unanswered--
	return true
}

// IsAnswer reports whether reply, a whole reply the reply trap took, has
// the shape of an answer to Query: CSI ? or =, parameters, c.
func IsAnswer(reply []byte) bool {
	return len(reply) >= 4 && reply[0] == 0x1b && reply[1] == '[' &&
		(reply[2] == '?' || reply[2] == '=') && reply[len(reply)-1] == 'c'
}
