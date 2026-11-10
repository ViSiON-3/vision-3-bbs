package leaf

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
)

func TestChatPrivateResolvesBareHandle(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	sender := subscribedLeaf(t, ts, "sender")
	recipient := subscribedLeaf(t, ts, "recipient")
	for _, l := range []*Leaf{sender, recipient} {
		if err := l.checkHubKey(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	alice := sender.NewChatSession("alice")
	defer alice.Close()
	bob := recipient.NewChatSession("bob")
	defer bob.Close()
	if _, _, err := bob.Join("lobby"); err != nil {
		t.Fatal(err)
	}
	watchEvents(t, recipient)
	if err := alice.Private("bob", "", "hello bob"); err != nil {
		t.Fatalf("handle-only private message: %v", err)
	}
	ev := awaitChat(t, bob, chat.TypePrivate)
	if ev.Message == nil || ev.Message.Text != "hello bob" || ev.Message.Node != sender.cfg.Keystore.NodeID() {
		t.Fatalf("recipient delivery: %+v", ev)
	}
}

func TestChatPrivateReportsAmbiguityAndPreservesExplicitTargets(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	sender := subscribedLeaf(t, ts, "sender")
	recipient := subscribedLeaf(t, ts, "recipient")
	other := subscribedLeaf(t, ts, "other")
	if err := sender.checkHubKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := recipient.checkHubKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	alice := sender.NewChatSession("alice")
	defer alice.Close()
	bob := recipient.NewChatSession("bob")
	defer bob.Close()
	duplicate := other.NewChatSession("bob")
	defer duplicate.Close()
	for _, s := range []*ChatSession{bob, duplicate} {
		if _, _, err := s.Join("lobby"); err != nil {
			t.Fatal(err)
		}
	}
	watchEvents(t, recipient)
	err := alice.Private("bob", "", "ambiguous")
	if err == nil || !strings.Contains(err.Error(), "409") || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous recipient error: %v", err)
	}
	for _, node := range []string{recipient.cfg.Keystore.NodeID(), other.cfg.Keystore.NodeID()} {
		if !strings.Contains(err.Error(), "bob@"+node) {
			t.Fatalf("missing explicit choice for %s: %v", node, err)
		}
	}
	err = alice.Private("missing", "", "unavailable")
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "not joined") {
		t.Fatalf("missing recipient error: %v", err)
	}
	// Neither rejection consumes the allowance; explicit disambiguation works.
	if err := alice.Private("bob", recipient.cfg.Keystore.NodeID(), "explicit"); err != nil {
		t.Fatal(err)
	}
	ev := awaitChat(t, bob, chat.TypePrivate)
	if ev.Message == nil || ev.Message.Text != "explicit" {
		t.Fatalf("explicit delivery: %+v", ev)
	}
}

func TestChatPrivateExplicitTargetWithoutRoomMembership(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	sender := subscribedLeaf(t, ts, "sender")
	recipient := subscribedLeaf(t, ts, "recipient")
	alice := sender.NewChatSession("alice")
	defer alice.Close()
	bob := recipient.NewChatSession("bob")
	defer bob.Close()
	watchEvents(t, recipient)
	if err := alice.Private("bob", recipient.cfg.Keystore.NodeID(), "outside room"); err != nil {
		t.Fatal(err)
	}
	ev := awaitChat(t, bob, chat.TypePrivate)
	if ev.Message == nil || ev.Message.Text != "outside room" {
		t.Fatalf("out-of-room delivery: %+v", ev)
	}
}

func TestChatPrivateErrorBodyFallback(t *testing.T) {
	for _, body := range []string{`not JSON`, `{"error":""}`} {
		t.Run(body, func(t *testing.T) {
			ts := signedHub(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusConflict); fmt.Fprint(w, body) }))
			defer ts.Close()
			l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
			offerChat(l)
			s := l.NewChatSession("alice")
			defer s.Close()
			err := s.Private("bob", "", "hello")
			if err == nil || !strings.Contains(err.Error(), "409") || strings.Contains(err.Error(), body) {
				t.Fatalf("unusable error body: %v", err)
			}
		})
	}
}
