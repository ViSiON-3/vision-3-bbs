package menu

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// postToastFixture is the alert fixture with post toasts on at level 20 and
// a short post format.
func postToastFixture(t *testing.T) *alertFixture {
	t.Helper()
	f := newAlertFixture(t)
	f.e.SetServerConfig(config.ServerConfig{ToastPosts: true, ToastAnnounceLevel: 20, TwitListMax: 10})
	strs := alertStrings()
	strs.ToastPost = "post %s %s"
	f.e.SetStrings(strs)
	return f
}

func areaByTag(t *testing.T, f *alertFixture, tag string) *message.MessageArea {
	t.Helper()
	a, ok := f.e.MessageMgr.GetAreaByTag(tag)
	if !ok {
		t.Fatalf("no area %s", tag)
	}
	return a
}

// A post reaches the other callers here who are past login, at the level,
// and may read the area; never the poster's own sessions.
func TestAnnouncePostReachesAllowedCallers(t *testing.T) {
	f := postToastFixture(t)
	poster, pu := f.caller(t, 1, "Poster", 30, user.CentralAlertPrefs{}, true)
	reader, _ := f.caller(t, 2, "Reader", 30, user.CentralAlertPrefs{}, true)
	low, _ := f.caller(t, 3, "Low", 15, user.CentralAlertPrefs{}, true) // below toastAnnounceLevel
	arriving, _ := f.caller(t, 4, "Arriving", 30, user.CentralAlertPrefs{}, false)
	samePoster := &session.BbsSession{ID: 5, NodeID: 5, User: pu, Channel: newTestSession("")}
	samePoster.SetToastsReady(true)
	f.e.SessionRegistry.Register(samePoster)

	f.e.AnnouncePost(f.um, poster, pu, areaByTag(t, f, "fel.local"), "Poster", false, false)
	if got := reader.DrainPages(); len(got) != 1 || got[0] != "post Poster Not networked" {
		t.Errorf("reader got %q", got)
	}
	for name, s := range map[string]*session.BbsSession{"low level": low, "logging in": arriving, "poster's other session": samePoster, "poster": poster} {
		if got := s.DrainPages(); len(got) != 0 {
			t.Errorf("%s got %q", name, got)
		}
	}
}

// Access follows the area: its read ACS and its conference ACS on the
// viewer's own connection, and the viewer's Twit List.
func TestAnnouncePostFollowsAreaAccess(t *testing.T) {
	f := postToastFixture(t)
	poster, pu := f.caller(t, 1, "Poster", 60, user.CentralAlertPrefs{}, true)
	member, _ := f.caller(t, 2, "Member", 60, user.CentralAlertPrefs{}, true)
	outsider, _ := f.caller(t, 3, "Outsider", 25, user.CentralAlertPrefs{}, true) // passes the level, not s50
	twit, tu := f.caller(t, 4, "Twit", 60, user.CentralAlertPrefs{}, true)
	tu.TwitList = []user.TwitEntry{{UserID: pu.ID, Name: "Poster"}}
	if err := f.um.UpdateUser(tu); err != nil {
		t.Fatal(err)
	}
	// A name-only entry for the real name the post carries in a real-name
	// area: no account ID, so it matches that name and nothing else.
	realNamer, ru := f.caller(t, 5, "RealNamer", 60, user.CentralAlertPrefs{}, true)
	ru.TwitList = []user.TwitEntry{{Name: "Poster Real"}}
	if err := f.um.UpdateUser(ru); err != nil {
		t.Fatal(err)
	}

	f.e.AnnouncePost(f.um, poster, pu, areaByTag(t, f, "fel.elite"), "Poster", false, false)
	if got := member.DrainPages(); len(got) != 1 {
		t.Errorf("member got %q", got)
	}
	if got := realNamer.DrainPages(); len(got) != 1 {
		t.Errorf("a name-only entry for another name hid the toast: %q", got)
	}
	// The same post saved under the poster's real name, as a real-name area
	// requires: named so, and hidden by that entry.
	f.e.AnnouncePost(f.um, poster, pu, areaByTag(t, f, "fel.elite"), "Poster Real", false, false)
	if got := member.DrainPages(); len(got) != 1 || got[0] != "post Poster Real Elite" {
		t.Errorf("member got %q for the real-name post", got)
	}
	if got := realNamer.DrainPages(); len(got) != 0 {
		t.Errorf("a Twit entry for the post's real name did not hide it: %q", got)
	}
	if got := outsider.DrainPages(); len(got) != 0 {
		t.Errorf("caller outside the conference got %q", got)
	}
	if got := twit.DrainPages(); len(got) != 0 {
		t.Errorf("a Twit-listed poster was announced: %q", got)
	}
}

// Nothing is announced with the switch off, the string blank, or for
// private mail, netmail, an anonymous post or an invisible poster.
func TestAnnouncePostNeverAnnounced(t *testing.T) {
	f := postToastFixture(t)
	poster, pu := f.caller(t, 1, "Poster", 30, user.CentralAlertPrefs{}, true)
	reader, _ := f.caller(t, 2, "Reader", 30, user.CentralAlertPrefs{}, true)
	local := areaByTag(t, f, "fel.local")
	check := func(what string) {
		t.Helper()
		if got := reader.DrainPages(); len(got) != 0 {
			t.Errorf("%s announced: %q", what, got)
		}
	}

	f.e.AnnouncePost(f.um, poster, pu, local, "Poster", true, false)
	check("a private post")
	f.e.AnnouncePost(f.um, poster, pu, local, "Poster", false, true)
	check("an anonymous post")
	f.e.AnnouncePost(f.um, poster, pu, &message.MessageArea{ID: 9, Tag: "PRIVMAIL", Name: "Private Mail"}, "Poster", false, false)
	check("PRIVMAIL")
	f.e.AnnouncePost(f.um, poster, pu, &message.MessageArea{ID: 10, Tag: "NETMAIL", Name: "Netmail", AreaType: "netmail"}, "Poster", false, false)
	check("netmail")

	poster.Invisible = true
	f.e.AnnouncePost(f.um, poster, pu, local, "Poster", false, false)
	check("an invisible poster")
	poster.Invisible = false

	strs := *f.e.Strings()
	strs.ToastPost = ""
	f.e.SetStrings(strs)
	f.e.AnnouncePost(f.um, poster, pu, local, "Poster", false, false)
	check("a blank string")

	f = postToastFixture(t)
	f.e.SetServerConfig(config.ServerConfig{ToastAnnounceLevel: 20})
	poster, pu = f.caller(t, 1, "Poster", 30, user.CentralAlertPrefs{}, true)
	reader, _ = f.caller(t, 2, "Reader", 30, user.CentralAlertPrefs{}, true)
	f.e.AnnouncePost(f.um, poster, pu, local, "Poster", false, false)
	check("posts with the switch off")
}

// A post in a V3Net area made here gives each eligible caller here one
// toast, the local one, and sends one network alert for other boards. The
// same post arriving at another board as an alert is shown there once, as
// the alert, and never also as a local toast, since imports announce
// nothing locally.
func TestAnnouncePostNoDoubleToasts(t *testing.T) {
	f := postToastFixture(t)
	seesAll := user.CentralAlertPrefs{SeeLogons: true, SeeLogoffs: true, SeeChat: true, SeePosts: true}
	poster, pu := f.caller(t, 1, "Poster", 30, user.CentralAlertPrefs{SharePosts: true}, true)
	reader, _ := f.caller(t, 2, "Reader", 30, seesAll, true)
	area := areaByTag(t, f, "fel.general")

	// As the compose and reply paths do after a save.
	f.e.AnnouncePost(f.um, poster, pu, area, "Poster", false, false)
	f.e.sendPostAlert(f.um, 1, pu, area.ID, area.Tag, false, false)
	if got := reader.DrainPages(); len(got) != 1 || got[0] != "post Poster FelonyNet General" {
		t.Errorf("reader here got %q, want the one local toast", got)
	}
	if len(f.fake.alerts) != 1 || f.fake.alerts[0].kind != protocol.CentralAlertPost {
		t.Errorf("network alerts sent: %+v", f.fake.alerts)
	}

	// On a board receiving that post: only the alert, delivered once.
	f.e.DeliverCentralAlert(incoming(protocol.CentralAlertPost), f.um)
	if got := reader.DrainPages(); len(got) != 1 || got[0] != "post Alice@Beta FelonyNet General testnet" {
		t.Errorf("receiving board got %q, want the one network alert", got)
	}
}
