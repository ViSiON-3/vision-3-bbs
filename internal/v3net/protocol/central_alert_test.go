package protocol_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// testAlert returns an unsigned alert of kind on testnet published at now,
// valid for two minutes.
func testAlert(now time.Time, kind string) *protocol.CentralAlert {
	a := &protocol.CentralAlert{
		V3NetCentral: protocol.CentralAlertVersion, Kind: kind, ID: "abcdefghijklmnopqrstuv", Network: "testnet",
		NodeID: "aaaa1111bbbb2222", BBS: "Alpha", Handle: "Alice",
		PublishedAt: now.Format(time.RFC3339), ExpiresAt: now.Add(2 * time.Minute).Format(time.RFC3339),
	}
	switch kind {
	case protocol.CentralAlertChatJoin:
		a.Room = "lobby"
	case protocol.CentralAlertPost:
		a.Area = "fel.general"
	}
	return a
}

// A signed alert verifies, and any change to what it covers breaks it: a
// relay cannot put another handle, board, room or area on it, or pass a
// logon off as a logoff.
func TestCentralAlertSignVerify(t *testing.T) {
	ks := activityKS(t)
	now := time.Now().UTC().Truncate(time.Second)
	for _, kind := range protocol.CentralAlertKinds {
		a := testAlert(now, kind)
		if err := a.Sign(ks); err != nil {
			t.Fatal(err)
		}
		if err := a.Validate("testnet"); err != nil {
			t.Fatalf("%s: validate: %v", kind, err)
		}
		if err := a.Verify(); err != nil {
			t.Fatalf("%s: verify: %v", kind, err)
		}
		for name, mutate := range map[string]func(x *protocol.CentralAlert){
			"handle":  func(x *protocol.CentralAlert) { x.Handle = "Mallory" },
			"bbs":     func(x *protocol.CentralAlert) { x.BBS = "Beta" },
			"kind":    func(x *protocol.CentralAlert) { x.Kind = protocol.CentralAlertLogoff },
			"id":      func(x *protocol.CentralAlert) { x.ID = "zzzzzzzzzzzzzzzzzzzzzz" },
			"room":    func(x *protocol.CentralAlert) { x.Room = "other" },
			"area":    func(x *protocol.CentralAlert) { x.Area = "fel.other" },
			"expires": func(x *protocol.CentralAlert) { x.ExpiresAt = now.Add(4 * time.Minute).Format(time.RFC3339) },
		} {
			x := *a
			mutate(&x)
			if x == *a {
				continue // no change for this kind
			}
			if err := x.Verify(); err == nil {
				t.Errorf("%s: a changed %s still verifies", kind, name)
			}
		}
	}
	var unsigned protocol.CentralAlert
	if err := unsigned.Verify(); err != protocol.ErrCentralAlertUnsigned {
		t.Errorf("unsigned alert: %v", err)
	}
}

// Validate refuses what an alert must not carry: a room or area on the
// wrong kind, a missing one on the right kind, screen codes, a life over
// the limit, and an unknown kind.
func TestCentralAlertValidate(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	cases := map[string]func(a *protocol.CentralAlert){
		"unknown kind":   func(a *protocol.CentralAlert) { a.Kind = "message_body" },
		"short id":       func(a *protocol.CentralAlert) { a.ID = "abc" },
		"other network":  func(a *protocol.CentralAlert) { a.Network = "othernet" },
		"no handle":      func(a *protocol.CentralAlert) { a.Handle = " " },
		"pipe in handle": func(a *protocol.CentralAlert) { a.Handle = "|12Alice" },
		"long handle":    func(a *protocol.CentralAlert) { a.Handle = strings.Repeat("x", protocol.MaxCentralHandleLen+1) },
		"control in bbs": func(a *protocol.CentralAlert) { a.BBS = "Alpha\x1b[2J" },
		"room on logon":  func(a *protocol.CentralAlert) { a.Room = "lobby" },
		"area on logon":  func(a *protocol.CentralAlert) { a.Area = "fel.general" },
		"long life": func(a *protocol.CentralAlert) {
			a.ExpiresAt = now.Add(protocol.MaxCentralAlertTTL + time.Second).Format(time.RFC3339)
		},
		"expires first":    func(a *protocol.CentralAlert) { a.ExpiresAt = now.Add(-time.Second).Format(time.RFC3339) },
		"bad version":      func(a *protocol.CentralAlert) { a.V3NetCentral = "2.0" },
		"unparsed publish": func(a *protocol.CentralAlert) { a.PublishedAt = "yesterday" },
	}
	for name, mutate := range cases {
		a := testAlert(now, protocol.CentralAlertLogon)
		mutate(a)
		if err := a.Validate("testnet"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	join := testAlert(now, protocol.CentralAlertChatJoin)
	join.Room = ""
	if join.Validate("testnet") == nil {
		t.Error("chat join without a room accepted")
	}
	join.Room = "Bad Room!"
	if join.Validate("testnet") == nil {
		t.Error("chat join with a malformed room accepted")
	}
	join = testAlert(now, protocol.CentralAlertChatJoin)
	join.Area = "fel.general"
	if join.Validate("testnet") == nil {
		t.Error("chat join with an area accepted")
	}

	post := testAlert(now, protocol.CentralAlertPost)
	post.Area = ""
	if post.Validate("testnet") == nil {
		t.Error("post without an area accepted")
	}
	post = testAlert(now, protocol.CentralAlertPost)
	post.Room = "lobby"
	if post.Validate("testnet") == nil {
		t.Error("post with a room accepted")
	}
}

// Expired is true at and after expires_at, and for one that does not parse.
func TestCentralAlertExpired(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	a := testAlert(now, protocol.CentralAlertLogon)
	if a.Expired(now) {
		t.Error("fresh alert expired")
	}
	if !a.Expired(now.Add(2 * time.Minute)) {
		t.Error("alert at its expiry not expired")
	}
	a.ExpiresAt = "soon"
	if !a.Expired(now) {
		t.Error("unparsed expiry not expired")
	}
}

// Alert IDs are random and of the form Validate wants.
func TestNewCentralAlertID(t *testing.T) {
	a, err := protocol.NewCentralAlertID()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := protocol.NewCentralAlertID()
	if a == b {
		t.Error("two IDs are equal")
	}
	x := testAlert(time.Now().UTC(), protocol.CentralAlertLogon)
	x.ID = a
	if err := x.Validate("testnet"); err != nil {
		t.Errorf("generated ID refused: %v", err)
	}
}
