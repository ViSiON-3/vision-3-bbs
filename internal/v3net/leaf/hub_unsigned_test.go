package leaf

import (
	"context"
	"errors"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
)

// A hub whose signatures are stripped is refused: every reply to a signed
// request, and the event stream. Syncing is not paused, since /info still
// serves the pinned key and there is nothing for the sysop to accept; once
// the signatures arrive again the leaf carries on.
func TestUnsignedReplyRefused(t *testing.T) {
	l, _, p, _ := proxiedLeaf(t, (*keystore.Keystore).PubKeyBase64)
	ctx := context.Background()
	p.stripSig.Store(true)

	if _, err := l.Poll(ctx); !errors.Is(err, ErrHubUnsigned) {
		t.Fatalf("Poll of an unsigned page = %v, want ErrHubUnsigned", err)
	}
	if err := l.SendLogon("tester"); !errors.Is(err, ErrHubUnsigned) {
		t.Errorf("SendLogon with an unsigned reply = %v, want ErrHubUnsigned", err)
	}
	if err := l.connectSSE(ctx, false); !errors.Is(err, ErrHubUnsigned) {
		t.Errorf("connectSSE to an unsigned stream = %v, want ErrHubUnsigned", err)
	}
	if l.HubPaused() {
		t.Error("paused by an unsigned reply")
	}

	p.stripSig.Store(false)
	if _, err := l.Poll(ctx); err != nil {
		t.Errorf("Poll once the hub's signatures arrive again: %v", err)
	}
}

// A leaf with no hub key pinned (its /info could not be read when it
// started) refuses the first signed reply, pins the key /info serves now
// and carries on; a hub whose /info serves no key keeps it paused.
func TestUnpinnedHubPinnedOnFirstReply(t *testing.T) {
	l, _, _, hubKS := proxiedLeaf(t, func(*keystore.Keystore) string { return "" })
	ctx := context.Background()

	if _, err := l.Poll(ctx); !errors.Is(err, ErrHubSignature) {
		t.Fatalf("Poll without a pin = %v, want ErrHubSignature", err)
	}
	if got := l.PinnedKey(KeyHub); got != hubKS.PubKeyBase64() {
		t.Fatalf("pinned %q after the refusal, want the hub's key", got)
	}
	if l.HubPaused() {
		t.Fatal("still paused once /info pinned the key")
	}
	if _, err := l.Poll(ctx); err != nil {
		t.Errorf("Poll with the key pinned: %v", err)
	}
}
