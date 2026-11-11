package protocol_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// rosterOf returns a roster on testnet naming the given keystores' boards.
func rosterOf(t *testing.T, seq int64, members ...interface {
	NodeID() string
	PubKeyBase64() string
}) *protocol.CentralRoster {
	t.Helper()
	r := &protocol.CentralRoster{V3NetCentral: protocol.CentralRosterVersion, Network: "testnet", Seq: seq,
		PublishedAt: time.Now().UTC().Format(time.RFC3339)}
	for _, m := range members {
		r.Members = append(r.Members, protocol.CentralRosterMember{NodeID: m.NodeID(), PubKeyB64: m.PubKeyBase64()})
	}
	return r
}

// A signed roster verifies and admits exactly its members, by node ID and
// key together; any change to what it covers breaks it.
func TestCentralRosterSignVerifyAdmits(t *testing.T) {
	coord, a, b := activityKS(t), activityKS(t), activityKS(t)
	r := rosterOf(t, 1, b, a)
	if err := r.Sign(coord); err != nil {
		t.Fatal(err)
	}
	if err := r.Validate("testnet"); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if err := r.Verify(); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !r.Admits(a.NodeID(), a.PubKeyBase64()) || !r.Admits(b.NodeID(), b.PubKeyBase64()) {
		t.Error("a member is not admitted")
	}
	c := activityKS(t)
	if r.Admits(c.NodeID(), c.PubKeyBase64()) {
		t.Error("a non-member is admitted")
	}
	// Same node ID claimed with another key: refused.
	if r.Admits(a.NodeID(), c.PubKeyBase64()) {
		t.Error("a member's node ID with another key is admitted")
	}
	if r.Members[0].NodeID > r.Members[1].NodeID {
		t.Error("members not sorted by Sign")
	}

	forged := *r
	forged.Members = append(append([]protocol.CentralRosterMember(nil), r.Members...), protocol.CentralRosterMember{NodeID: c.NodeID(), PubKeyB64: c.PubKeyBase64()})
	if forged.Verify() == nil {
		t.Error("a hub-added member still verifies")
	}
	forged = *r
	forged.Seq++
	if forged.Verify() == nil {
		t.Error("a changed seq still verifies")
	}
	var unsigned protocol.CentralRoster
	if unsigned.Verify() != protocol.ErrCentralRosterUnsigned {
		t.Error("unsigned roster not reported")
	}
}

// Validate refuses a member whose node ID does not derive from its key, a
// member named twice, a bad seq, version or network.
func TestCentralRosterValidate(t *testing.T) {
	coord, a, b := activityKS(t), activityKS(t), activityKS(t)
	for name, mutate := range map[string]func(r *protocol.CentralRoster){
		"mismatched id": func(r *protocol.CentralRoster) { r.Members[0].NodeID = b.NodeID() },
		"bad key":       func(r *protocol.CentralRoster) { r.Members[0].PubKeyB64 = "not-a-key" },
		"twice":         func(r *protocol.CentralRoster) { r.Members = append(r.Members, r.Members[0]) },
		"zero seq":      func(r *protocol.CentralRoster) { r.Seq = 0 },
		"version":       func(r *protocol.CentralRoster) { r.V3NetCentral = "2.0" },
		"network":       func(r *protocol.CentralRoster) { r.Network = "othernet" },
		"published":     func(r *protocol.CentralRoster) { r.PublishedAt = "today" },
	} {
		r := rosterOf(t, 1, a)
		mutate(r)
		_ = r.Sign(coord)
		if err := r.Validate("testnet"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	m, err := protocol.CentralRosterMemberFor(a.PubKeyBase64())
	if err != nil || m.NodeID != a.NodeID() {
		t.Errorf("member for key: %+v %v", m, err)
	}
	if _, err := protocol.CentralRosterMemberFor(strings.Repeat("A", 10)); err == nil {
		t.Error("a bad key made a member")
	}
}

// An open roster admits every board, and its flag is signed: a hub cannot
// open or close a roster the coordinator signed.
func TestCentralRosterOpen(t *testing.T) {
	coord, a, c := activityKS(t), activityKS(t), activityKS(t)
	r := rosterOf(t, 2, a)
	r.PrevSeq, r.Open = 1, true
	if err := r.Sign(coord); err != nil {
		t.Fatal(err)
	}
	if err := r.Verify(); err != nil {
		t.Fatal(err)
	}
	if !r.Admits(c.NodeID(), c.PubKeyBase64()) {
		t.Error("an open roster refused a board")
	}
	flipped := *r
	flipped.Open = false
	if flipped.Verify() == nil {
		t.Error("closing a signed open roster still verifies")
	}
	closed := rosterOf(t, 3, a)
	closed.PrevSeq = 2
	_ = closed.Sign(coord)
	opened := *closed
	opened.Open = true
	if opened.Verify() == nil {
		t.Error("opening a signed closed roster still verifies")
	}
}

// A closed roster signs exactly as before the open flag existed, so boards
// that predate it still verify it.
func TestCentralRosterClosedSignsAsBefore(t *testing.T) {
	coord, a := activityKS(t), activityKS(t)
	r := rosterOf(t, 1, a)
	if err := r.Sign(coord); err != nil {
		t.Fatal(err)
	}
	payload, _ := r.SignedPayload()
	if strings.Contains(string(payload), "open") {
		t.Errorf("closed roster's payload names open: %s", payload)
	}
}

// A signed chat action verifies, is refused once changed, and must name
// what its kind needs.
func TestChatSignedSignVerifyValidate(t *testing.T) {
	ks := activityKS(t)
	c, err := protocol.NewChatSigned(ks, protocol.ChatSignedLine, "testnet", "Alice", func(c *protocol.ChatSigned) { c.Room, c.Text = "lobby", "hi" })
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Validate("testnet"); err != nil {
		t.Fatal(err)
	}
	if err := c.Verify(); err != nil {
		t.Fatal(err)
	}
	changed := *c
	changed.Text = "bye"
	if changed.Verify() == nil {
		t.Error("changed text still verifies")
	}
	for name, mutate := range map[string]func(c *protocol.ChatSigned){
		"line with recipient": func(c *protocol.ChatSigned) { c.ToHandle = "Bob" },
		"no room":             func(c *protocol.ChatSigned) { c.Room = "" },
		"no handle":           func(c *protocol.ChatSigned) { c.Handle = " " },
		"other network":       func(c *protocol.ChatSigned) { c.Network = "othernet" },
		"kind":                func(c *protocol.ChatSigned) { c.Kind = "topic" },
	} {
		x := *c
		mutate(&x)
		if x.Validate("testnet") == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	p, _ := protocol.NewChatSigned(ks, protocol.ChatSignedPrivate, "testnet", "Alice", func(c *protocol.ChatSigned) { c.ToHandle, c.Text = "Bob", "psst" })
	if p.Validate("testnet") != nil || p.Verify() != nil {
		t.Error("a private message does not validate")
	}
}
