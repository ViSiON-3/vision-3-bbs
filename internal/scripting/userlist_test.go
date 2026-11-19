package scripting

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestPublicUserDirectoryProjection(t *testing.T) {
	cur := &user.User{ID: 1, Handle: "Zulu", RealName: "Private name", PrivateNote: "Private note", PasswordHash: "Private hash"}
	p := &Providers{CurrentUser: cur, UserMgr: user.NewUserMgrForTest(cur,
		&user.User{ID: 2, Handle: "alpha", AccessLevel: 0},
		&user.User{ID: 3, Handle: "Gone", DeletedUser: true},
		&user.User{ID: 4, Handle: "Beta", Validated: true, GroupLocation: "Elsewhere", AccessLevel: 20})}
	h := newHarness(t, harnessOpts{providers: p})
	if got := h.eval(`v3.users.publicList().map(function(u) { return u.handle; }).join(",")`).String(); got != "alpha,Beta,Zulu" {
		t.Fatalf("public handles = %q", got)
	}
	// Every row, the current user's included, has only the public fields.
	got := h.eval(`v3.users.publicList().map(function(u) { return Object.keys(u).sort().join(","); }).join(";")`).String()
	if want := strings.Repeat("accessLevel,handle,location,validated;", 2) + "accessLevel,handle,location,validated"; got != want {
		t.Fatalf("public fields = %q, want %q", got, want)
	}
}
