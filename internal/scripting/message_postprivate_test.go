package scripting

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TestPostPrivateAddressesByHandle pins #462 for the script API: outside
// netmail, private mail is read only by the handle on it, so postPrivate
// stores the handle of the account `to` names (a real name resolves to its
// one account) and throws for a name that identifies no current account.
// Netmail keeps `to` as given.
func TestPostPrivateAddressesByHandle(t *testing.T) {
	mm, err := message.NewMessageManager(t.TempDir(), t.TempDir(), "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	localID, err := mm.AddArea(message.MessageArea{Tag: "PRIVMAIL", Name: "Private", AreaType: "local"})
	if err != nil {
		t.Fatal(err)
	}
	netID, err := mm.AddArea(message.MessageArea{Tag: "NETMAIL", Name: "Netmail", AreaType: "netmail", OriginAddr: "21:1/100"})
	if err != nil {
		t.Fatal(err)
	}
	um := user.NewUserMgrForTest(
		&user.User{ID: 1, Handle: "Sysop", RealName: "Sam Sysop", AccessLevel: 255},
		&user.User{ID: 2, Handle: "Bob", RealName: "Bob Builder", AccessLevel: 30},
		&user.User{ID: 3, Handle: "Gone", RealName: "Gone Away", AccessLevel: 30, DeletedUser: true},
		&user.User{ID: 4, Handle: "TwinA", RealName: "Pat Twin", AccessLevel: 30},
		&user.User{ID: 5, Handle: "TwinB", RealName: "Pat Twin", AccessLevel: 30},
	)

	post := func(areaID int, to string) (int, error) {
		t.Helper()
		sess := newInterruptibleSession("")
		eng := NewEngine(context.Background(), &SessionContext{
			Session: sess, OutputMode: ansi.OutputModeUTF8, ScreenWidth: 80, ScreenHeight: 24,
			UserHandle: "Sysop",
		}, ScriptConfig{}, &Providers{MessageMgr: mm, UserMgr: um})
		t.Cleanup(func() {
			sess.closeInterrupt()
			eng.Close()
		})
		v, err := eng.vm.RunString(`v3.message.postPrivate(` + strconv.Itoa(areaID) +
			`, {to: ` + strconv.Quote(to) + `, subject: "Hi", body: "B"})`)
		if err != nil {
			return 0, err
		}
		return int(v.ToInteger()), nil
	}
	storedTo := func(areaID, n int) string {
		t.Helper()
		m, err := mm.GetMessage(areaID, n)
		if err != nil {
			t.Fatal(err)
		}
		return m.To
	}

	for _, tc := range []struct{ to, want string }{
		{"Bob Builder", "Bob"},
		{"bob", "Bob"},
		{"Sysop", "Sysop"},
	} {
		n, err := post(localID, tc.to)
		if err != nil {
			t.Errorf("postPrivate to %q: %v", tc.to, err)
			continue
		}
		if got := storedTo(localID, n); got != tc.want {
			t.Errorf("postPrivate to %q stored To=%q, want %q", tc.to, got, tc.want)
		}
	}

	for _, to := range []string{"Nobody Known", "Gone", "Pat Twin"} {
		if _, err := post(localID, to); err == nil {
			t.Errorf("postPrivate to %q did not throw", to)
		} else if !strings.Contains(err.Error(), "not a user of this BBS") {
			t.Errorf("postPrivate to %q: unexpected error %v", to, err)
		}
	}
	if n, _ := mm.GetMessageCountForArea(localID); n != 3 {
		t.Errorf("area holds %d messages, want only the 3 delivered", n)
	}

	n, err := post(netID, "Joe Remote@21:1/200")
	if err != nil {
		t.Fatalf("netmail postPrivate: %v", err)
	}
	if got := storedTo(netID, n); got != "Joe Remote" {
		t.Errorf("netmail To=%q, want the name as given", got)
	}
}
