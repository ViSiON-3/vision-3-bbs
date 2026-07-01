package qwkapi

import (
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestTokenStore_IssueResolveExpire(t *testing.T) {
	ts := newTokenStore(50 * time.Millisecond)
	u := &user.User{Handle: "felonius"}

	tok, exp := ts.Issue(u)
	if tok == "" || !exp.After(time.Now()) {
		t.Fatalf("bad issue: tok=%q exp=%v", tok, exp)
	}
	got, ok := ts.Resolve(tok)
	if !ok || got.Handle != "felonius" {
		t.Fatalf("resolve = %v, %v; want felonius,true", got, ok)
	}
	if _, ok := ts.Resolve("nope"); ok {
		t.Error("unknown token resolved")
	}

	// After TTL, the token is rejected.
	ts.expireForTest(tok)
	if _, ok := ts.Resolve(tok); ok {
		t.Error("expired token still resolves")
	}
}
