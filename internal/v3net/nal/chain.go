package nal

import (
	"errors"
	"fmt"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// The rules a hub applies to a posted chain of coordinator handovers. They
// live here so the hub and v3net-handover, which checks a chain before posting
// it, use the same code. They look only at the keys of each handover, so a
// caller that holds just the keys can pass handovers with no signature; the
// hub verifies signatures itself.

// Reasons a hub refuses a posted chain.
var (
	// ErrChainNotJoined means a link does not start at the key the one before
	// handed over to.
	ErrChainNotJoined = errors.New("each handover must start at the key the one before hands over to")
	// ErrChainRepeatsKey means a chain visits a key twice.
	ErrChainRepeatsKey = errors.New("a handover chain may not visit a key twice")
	// ErrChainUsedKey means a chain hands over to a key the network used.
	ErrChainUsedKey = errors.New("handover returns to a key the network already used")
	// ErrChainNotInForce means a link is not signed by the key in force.
	ErrChainNotInForce = errors.New("handover is not signed by the coordinator key in force")
	// ErrChainNotToHub means a hub running a new key was offered a handover to
	// another.
	ErrChainNotToHub = errors.New("the hub already runs a new key; only a handover to the hub's key is accepted")
)

// ChainCheck is what CheckPostedChain found in a chain it accepts.
type ChainCheck struct {
	// Fresh are the links the hub does not hold yet. None means a repeat.
	Fresh []protocol.CoordHandover
	// FromPrevSigners says Fresh bridges the previous signers to the hub's
	// key, rather than following the key in force.
	FromPrevSigners bool
}

// CheckChainShape checks a chain on its own: 1 to MaxHandoverSteps links, each
// starting at the key the one before handed over to, no key twice.
func CheckChainShape(chain []protocol.CoordHandover) error {
	if len(chain) == 0 || len(chain) > MaxHandoverSteps {
		return fmt.Errorf("a handover chain has 1 to %d handovers", MaxHandoverSteps)
	}
	seen := map[string]bool{chain[0].OldKeyB64: true}
	for i := range chain {
		if i > 0 && chain[i].OldKeyB64 != chain[i-1].NewKeyB64 {
			return ErrChainNotJoined
		}
		if seen[chain[i].NewKeyB64] {
			return ErrChainRepeatsKey
		}
		seen[chain[i].NewKeyB64] = true
	}
	return nil
}

// CheckPostedChain applies a hub's rules to chain, whose shape has been
// checked, for a hub running hubKey that stores a NAL signed by nalKey and
// holds the handovers held and the previous signers prev, oldest first. Links
// the hub holds are repeats and are skipped. The rest must be a late chain from
// the previous signers, or else each link must follow the key in force. It
// returns one of the ErrChain errors if the hub refuses the chain.
func CheckPostedChain(nalKey string, held, chain []protocol.CoordHandover, prev []string, hubKey string) (ChainCheck, error) {
	var fresh []protocol.CoordHandover
	for i := range chain {
		if !holdsHandover(held, &chain[i]) {
			fresh = append(fresh, chain[i])
		}
	}
	if len(fresh) == 0 {
		return ChainCheck{}, nil
	}
	if fromPrevSigners(nalKey, held, fresh, prev, hubKey) {
		return ChainCheck{Fresh: fresh, FromPrevSigners: true}, nil
	}
	// A previous signer is a key the network used, though no handover names
	// it, so the key-in-force rules below would not see it. Only a chain from
	// the previous signers passes through them.
	if returnsToSigner(fresh, prev) {
		return ChainCheck{}, ErrChainUsedKey
	}
	if err := checkChainInForce(nalKey, held, fresh, hubKey); err != nil {
		return ChainCheck{}, err
	}
	return ChainCheck{Fresh: fresh}, nil
}

// CoordKeyPath returns the keys the coordinator role passes through: nalKey,
// the key of the stored NAL, then each key hs hands it on to, ending with the
// key in force. It is just nalKey when no handover starts there, so a NAL
// re-signed by a key off the chain is not overruled by older handovers.
func CoordKeyPath(nalKey string, hs []protocol.CoordHandover) []string {
	key := nalKey
	path := []string{key}
	seen := map[string]bool{key: true}
	for moved := true; moved; {
		moved = false
		for _, h := range hs {
			if h.OldKeyB64 == key && !seen[h.NewKeyB64] {
				key = h.NewKeyB64
				seen[key] = true
				path = append(path, key)
				moved = true
				break
			}
		}
	}
	return path
}

// returnsToSigner reports whether chain hands over to one of the previous
// signers prev.
func returnsToSigner(chain []protocol.CoordHandover, prev []string) bool {
	for _, ho := range chain {
		for _, key := range prev {
			if ho.NewKeyB64 == key {
				return true
			}
		}
	}
	return false
}

// holdsHandover reports whether hs has a handover from ho's old key to its
// new key, signed at any time.
func holdsHandover(hs []protocol.CoordHandover, ho *protocol.CoordHandover) bool {
	for _, old := range hs {
		if old.OldKeyB64 == ho.OldKeyB64 && old.NewKeyB64 == ho.NewKeyB64 {
			return true
		}
	}
	return false
}

// checkChainInForce checks chain link by link against the stored NAL's key and
// the handovers hs, as if each link were posted once the ones before it
// were held: each must be signed by the coordinator key in force, hand
// over to a key the network has not used, and, while the hub runs a key
// that neither signs the NAL nor appears on the handovers, hand over to the
// hub's key.
func checkChainInForce(nalKey string, hs, chain []protocol.CoordHandover, hubKey string) error {
	staged := append(hs[:len(hs):len(hs)], chain...)
	for i := range chain {
		ho, heldNow := &chain[i], staged[:len(hs)+i]
		path := CoordKeyPath(nalKey, heldNow)
		if ho.OldKeyB64 != path[len(path)-1] {
			return ErrChainNotInForce
		}
		if ho.NewKeyB64 == nalKey || handoverUsesKey(heldNow, ho.NewKeyB64) {
			return ErrChainUsedKey
		}
		if hubSwitched(nalKey, heldNow, hubKey) && ho.NewKeyB64 != hubKey {
			return ErrChainNotToHub
		}
	}
	return nil
}

// fromPrevSigners reports whether chain is a late chain of handovers from
// the network's previous signers prev (see Hub.SignNAL) to the hub's key. The
// keys that signed the NAL, in order, are the recorded previous signers and
// then the stored NAL's own key, when that is not yet the hub's. The
// chain qualifies when no handover is held, it ends at the hub's key,
// which is none of those, and every old key on
// it is one of them, in that order. When the NAL is not yet the hub's, the
// last link must start at its key, so that adopting the chain re-signs it. The
// chain has been verified, so each link was signed by the key before it.
//
// When the NAL is already the hub's, it is re-signed only to move the old
// keys' areas to the hub, in the update that stores the chain (see
// handlePostHandover). When it is not (the hub ran
// A, was switched to B and re-signed, then to C without re-signing), the
// chain is stored and adopted as any handover to the hub's key is.
//
// This is as strong as the key-in-force rule: each previous signer was the
// key in force until the hub re-signed without a handover, and only its
// holder can sign its link. Taking the keys in the order they signed keeps
// the chain to the switches the hub made, never through a third key, and a
// chain can only end at the hub's own key. Once any handover is held the
// record no longer applies, so a chain must be posted whole (see
// handlePostHandover). If a previous signer was a lost key, nobody can sign
// a link from it and the chain starts after it; its leaves re-pin by hand,
// as before. A stolen one gains its thief nothing: it can only point
// leaves at the key the hub already runs. Only a limited number of keys are
// recorded; leaves on an older one re-pin by hand.
//
// A hub whose own key is a previous signer went back to a key the network
// used; no chain qualifies then, and the caller refuses any chain to a
// previous signer (see returnsToSigner).
func fromPrevSigners(nalKey string, hs, chain []protocol.CoordHandover, prev []string, hubKey string) bool {
	if len(hs) > 0 || hubKey == "" || chain[len(chain)-1].NewKeyB64 != hubKey {
		return false
	}
	if nalKey != hubKey && chain[len(chain)-1].OldKeyB64 != nalKey {
		return false
	}
	signers := make([]string, 0, len(prev)+1)
	for _, key := range prev {
		if key == hubKey {
			return false
		}
		if key != nalKey {
			signers = append(signers, key)
		}
	}
	if nalKey != hubKey {
		signers = append(signers, nalKey)
	}
	pos := make(map[string]int, len(signers))
	for i, key := range signers {
		pos[key] = i
	}
	last := -1
	for _, ho := range chain {
		i, ok := pos[ho.OldKeyB64]
		if !ok || i <= last {
			return false
		}
		last = i
	}
	return true
}

// hubSwitched reports whether the hub runs a key that neither signs the
// stored NAL nor appears on its handovers hs: it was restarted with a new
// key before the network was handed to it.
func hubSwitched(nalKey string, hs []protocol.CoordHandover, hubKey string) bool {
	return hubKey != "" && hubKey != nalKey && !handoverUsesKey(hs, hubKey)
}

// handoverUsesKey reports whether key is either end of any of hs.
func handoverUsesKey(hs []protocol.CoordHandover, key string) bool {
	for _, h := range hs {
		if h.OldKeyB64 == key || h.NewKeyB64 == key {
			return true
		}
	}
	return false
}
