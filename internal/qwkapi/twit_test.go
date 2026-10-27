package qwkapi

import (
	"net/http/httptest"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/qwkservice"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

type twitPacketService struct {
	fakeSvc
	opts qwkservice.ExportOptions
}

// BuildPacket captures API export options without building a packet.
func (s *twitPacketService) BuildPacket(opts qwkservice.ExportOptions) (*qwkservice.ExportResult, error) {
	s.opts = opts
	return &qwkservice.ExportResult{}, nil
}

// TestPacketTwitMatcherResolvedOnEveryDownload checks that reused tokens receive current filter state.
func TestPacketTwitMatcherResolvedOnEveryDownload(t *testing.T) {
	svc := &twitPacketService{}
	live := &user.User{ID: 2, Handle: "Reader", TwitList: []user.TwitEntry{{Name: "First"}}}
	calls := 0
	server, err := NewServer(Deps{ConfigDir: t.TempDir(), Service: svc, DenyLogin: admitAll, TwitMatcherFor: func(snapshot *user.User) *user.TwitMatcher {
		calls++
		if snapshot.Handle != "Reader" {
			t.Fatalf("wrong account: %+v", snapshot)
		}
		return user.BuildTwitMatcher(live.TwitList, nil, user.TwitPolicy{}, "")
	}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := *live
	tok, _, err := server.tokens.Issue(&snapshot)
	if err != nil {
		t.Fatal(err)
	}
	h := server.Handler()
	for _, name := range []string{"First", "Second"} {
		live.TwitList = []user.TwitEntry{{Name: name}}
		r := clientReq("GET", "/api/qwk/packet", nil)
		r.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, r)
		if rr.Code != 204 || !svc.opts.TwitList.Hidden(name, "") || svc.opts.TwitList.Hidden("Anonymous", "") {
			t.Fatalf("status=%d matcher ignored current list", rr.Code)
		}
	}
	if calls != 2 {
		t.Fatalf("matcher called %d times", calls)
	}
}

// TestNewServerRequiresTwitMatcher checks that a server can't be built without the board's Twit List rules.
func TestNewServerRequiresTwitMatcher(t *testing.T) {
	if _, err := NewServer(Deps{ConfigDir: t.TempDir(), Service: &twitPacketService{}}); err == nil {
		t.Fatal("NewServer accepted a nil TwitMatcherFor")
	}
}
