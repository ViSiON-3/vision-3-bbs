package nal

import (
	"errors"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// handoverKeys returns n fresh keys.
func handoverKeys(t *testing.T, n int) []*keystore.Keystore {
	t.Helper()
	ks := make([]*keystore.Keystore, n)
	for i := range ks {
		ks[i] = testKeystore(t)
	}
	return ks
}

// handover returns from's signed handover of network to to.
func handover(t *testing.T, network string, from, to *keystore.Keystore) protocol.CoordHandover {
	t.Helper()
	h, err := protocol.NewCoordHandover(from, network, to.PubKeyBase64(), time.Now())
	if err != nil {
		t.Fatalf("NewCoordHandover: %v", err)
	}
	return *h
}

// chainOf returns the handovers k[0]->k[1]->...->k[len-1].
func chainOf(t *testing.T, k []*keystore.Keystore) []protocol.CoordHandover {
	t.Helper()
	var hs []protocol.CoordHandover
	for i := 0; i+1 < len(k); i++ {
		hs = append(hs, handover(t, "testnet", k[i], k[i+1]))
	}
	return hs
}

func TestFollowHandovers(t *testing.T) {
	k := handoverKeys(t, 4)
	pub := func(i int) string { return k[i].PubKeyBase64() }
	hs := chainOf(t, k) // 0->1->2->3

	// One step and two steps, from any point on the chain.
	if got, err := FollowHandovers("testnet", pub(0), pub(1), hs); err != nil || len(got) != 1 || got[0].NewKeyB64 != pub(1) {
		t.Errorf("one step: %d handovers, %v", len(got), err)
	}
	got, err := FollowHandovers("testnet", pub(1), pub(3), hs)
	if err != nil || len(got) != 2 || got[0].OldKeyB64 != pub(1) || got[1].NewKeyB64 != pub(3) {
		t.Errorf("two steps: %+v, %v", got, err)
	}
	// Order in the list does not matter.
	rev := []protocol.CoordHandover{hs[2], hs[1], hs[0]}
	if got, err := FollowHandovers("testnet", pub(0), pub(3), rev); err != nil || len(got) != 3 {
		t.Errorf("reversed list: %d handovers, %v", len(got), err)
	}

	// Nothing leads backwards, or from a key not on the chain.
	stranger := testKeystore(t).PubKeyBase64()
	for name, c := range map[string][2]string{
		"backwards":                     {pub(3), pub(0)},
		"not from the pin":              {stranger, pub(3)},
		"to an unknown key":             {pub(0), stranger},
		"same key":                      {pub(0), pub(0)},
		"pin spelled with a line break": {pub(0)[:20] + "\n" + pub(0)[20:], pub(1)},
	} {
		if _, err := FollowHandovers("testnet", c[0], c[1], hs); !errors.Is(err, ErrNoHandoverChain) {
			t.Errorf("%s: err = %v, want ErrNoHandoverChain", name, err)
		}
	}

	// Handovers for another network do not count.
	if _, err := FollowHandovers("othernet", pub(0), pub(1), hs); !errors.Is(err, ErrNoHandoverChain) {
		t.Errorf("other network: err = %v, want ErrNoHandoverChain", err)
	}
}

// A forged or tampered handover is skipped, so it neither breaks a good
// chain nor makes a chain of its own.
func TestFollowHandoversSkipsForgeries(t *testing.T) {
	k := handoverKeys(t, 3)
	good := handover(t, "testnet", k[0], k[1])
	// The attacker's key k[2] claims to take over from k[0], with a
	// signature of its own rather than k[0]'s.
	forged := handover(t, "testnet", k[2], k[1])
	forged.OldKeyB64 = k[0].PubKeyBase64()
	forged.NewKeyB64 = k[2].PubKeyBase64()

	if _, err := FollowHandovers("testnet", k[0].PubKeyBase64(), k[2].PubKeyBase64(), []protocol.CoordHandover{good, forged}); !errors.Is(err, ErrNoHandoverChain) {
		t.Errorf("forged step: err = %v, want ErrNoHandoverChain", err)
	}
	if got, err := FollowHandovers("testnet", k[0].PubKeyBase64(), k[1].PubKeyBase64(), []protocol.CoordHandover{forged, good}); err != nil || len(got) != 1 {
		t.Errorf("good chain beside a forgery: %d handovers, %v", len(got), err)
	}
}

// A loop ends the search instead of running forever, and a chain longer
// than MaxHandoverSteps, or a list longer than MaxHandovers, is not followed.
func TestFollowHandoversLimits(t *testing.T) {
	k := handoverKeys(t, 3)
	loop := []protocol.CoordHandover{
		handover(t, "testnet", k[0], k[1]),
		handover(t, "testnet", k[1], k[0]),
	}
	if _, err := FollowHandovers("testnet", k[0].PubKeyBase64(), k[2].PubKeyBase64(), loop); !errors.Is(err, ErrNoHandoverChain) {
		t.Errorf("loop: err = %v, want ErrNoHandoverChain", err)
	}

	long := handoverKeys(t, MaxHandoverSteps+2)
	hs := chainOf(t, long)
	first := long[0].PubKeyBase64()
	if got, err := FollowHandovers("testnet", first, long[MaxHandoverSteps].PubKeyBase64(), hs); err != nil || len(got) != MaxHandoverSteps {
		t.Errorf("%d steps: %d handovers, %v", MaxHandoverSteps, len(got), err)
	}
	if _, err := FollowHandovers("testnet", first, long[MaxHandoverSteps+1].PubKeyBase64(), hs); !errors.Is(err, ErrNoHandoverChain) {
		t.Errorf("%d steps: err = %v, want ErrNoHandoverChain", MaxHandoverSteps+1, err)
	}

	padded := append([]protocol.CoordHandover{}, hs[0])
	for len(padded) <= MaxHandovers {
		padded = append(padded, hs[1])
	}
	if _, err := FollowHandovers("testnet", first, long[1].PubKeyBase64(), padded); !errors.Is(err, ErrNoHandoverChain) {
		t.Errorf("%d handovers: err = %v, want ErrNoHandoverChain", len(padded), err)
	}
}

// Handovers are not part of the NAL's signature: a NAL verifies with or
// without them.
func TestHandoversOutsideNALSignature(t *testing.T) {
	k := handoverKeys(t, 2)
	n := testNAL(k[0])
	if err := Sign(n, k[0]); err != nil {
		t.Fatal(err)
	}
	n.Handovers = chainOf(t, k)
	if err := Verify(n); err != nil {
		t.Errorf("Verify with handovers attached: %v", err)
	}
}
