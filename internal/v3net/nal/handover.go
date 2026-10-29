package nal

import (
	"errors"
	"fmt"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// Limits on following coordinator handovers. A leaf that was offline
// through more handovers than MaxHandoverSteps re-pins by hand, as it would
// with no handover at all.
const (
	// MaxHandoverSteps is the longest chain of handovers a reader follows
	// from its pinned key.
	MaxHandoverSteps = 8
	// MaxHandovers is the most handovers a reader looks at in one NAL. The
	// hub serves at most this many, the most recent; a longer list is
	// refused whole rather than searched.
	MaxHandovers = 32
)

// ErrNoHandoverChain means the handovers served with a NAL do not lead from
// the reader's pinned coordinator key to the key that signed the NAL.
var ErrNoHandoverChain = errors.New("nal: no coordinator handover chain from the pinned key")

// FollowHandovers returns the handovers that lead from the key from to the
// key to for network, in order: the first is signed by from, each later one
// by the key the one before it handed over to, and the last hands over to
// to. Only handovers for network that verify are used, so a list with
// forged or foreign entries can still yield a chain through the good ones.
// No key is visited twice, so loops end, and a chain longer than
// MaxHandoverSteps is not followed. It returns an error wrapping
// ErrNoHandoverChain when there is no such chain or hs is longer than
// MaxHandovers. from and to must differ.
func FollowHandovers(network, from, to string, hs []protocol.CoordHandover) ([]protocol.CoordHandover, error) {
	if from == "" || to == "" || from == to {
		return nil, fmt.Errorf("%w: nothing to follow", ErrNoHandoverChain)
	}
	if len(hs) > MaxHandovers {
		return nil, fmt.Errorf("%w: %d handovers, more than the %d allowed", ErrNoHandoverChain, len(hs), MaxHandovers)
	}

	// Breadth first, so the shortest chain wins and the step limit is
	// simply the search depth. prev records how each key was reached.
	prev := map[string]handoverStep{from: {via: -1}}
	checked := make(map[int]bool, len(hs)) // index -> verified for network
	frontier := []string{from}
	for depth := 0; depth < MaxHandoverSteps && len(frontier) > 0; depth++ {
		var next []string
		for _, key := range frontier {
			for i := range hs {
				h := &hs[i]
				if h.OldKeyB64 != key {
					continue
				}
				if _, seen := prev[h.NewKeyB64]; seen {
					continue
				}
				ok, done := checked[i]
				if !done {
					ok = h.Network == network && h.Verify() == nil
					checked[i] = ok
				}
				if !ok {
					continue
				}
				prev[h.NewKeyB64] = handoverStep{from: key, via: i}
				if h.NewKeyB64 == to {
					return chainTo(hs, prev, to), nil
				}
				next = append(next, h.NewKeyB64)
			}
		}
		frontier = next
	}
	return nil, fmt.Errorf("%w: within %d steps", ErrNoHandoverChain, MaxHandoverSteps)
}

// handoverStep records how FollowHandovers reached a key.
type handoverStep struct {
	from string // the key it was reached from
	via  int    // index of the handover used, -1 for the starting key
}

// chainTo walks prev back from to and returns the handovers in order.
func chainTo(hs []protocol.CoordHandover, prev map[string]handoverStep, to string) []protocol.CoordHandover {
	var chain []protocol.CoordHandover
	for s := prev[to]; s.via >= 0; s = prev[s.from] {
		chain = append(chain, hs[s.via])
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}
