package menu

import (
	"fmt"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// An upload sends one post alert per V3Net area it posted to, in the order
// it first posted there: never for private mail, never for an area on no
// network, and only for an uploader who shares posts.
func TestUploadPostAlertsOnePerArea(t *testing.T) {
	f := newAlertFixture(t)
	sess, u := f.caller(t, 1, "Uploader", 30, user.CentralAlertPrefs{SharePosts: true}, true)
	general := uploadTestArea(t, f, "fel.general")
	local := uploadTestArea(t, f, "fel.local")
	elite := uploadTestArea(t, f, "fel.elite")

	a := &uploadPostAlerts{}
	for range 40 { // a busy packet: many replies in one area
		a.posted(general, false)
	}
	a.posted(local, false) // not on a network
	a.posted(elite, true)  // private: not announced, nor does it count
	a.posted(elite, false) // the area's first public reply
	a.posted(general, false)
	f.e.sendUploadPostAlerts(f.um, sess, u, a)

	want := []sentAlert{
		{"testnet", protocol.CentralAlertPost, "Uploader", "", "fel.general"},
		{"testnet", protocol.CentralAlertPost, "Uploader", "", "fel.elite"},
	}
	if fmt.Sprint(f.fake.alerts) != fmt.Sprint(want) {
		t.Errorf("sent %+v, want %+v", f.fake.alerts, want)
	}

	// The same upload by a caller who does not share posts sends nothing.
	f.fake.alerts = nil
	_, quiet := f.caller(t, 2, "Quiet", 30, user.CentralAlertPrefs{}, true)
	f.e.sendUploadPostAlerts(f.um, nil, quiet, a)
	if len(f.fake.alerts) != 0 {
		t.Errorf("an uploader who does not share posts sent %+v", f.fake.alerts)
	}
}

// However many V3Net areas one packet posts to, it sends at most
// maxUploadPostAlerts alerts.
func TestUploadPostAlertsCapped(t *testing.T) {
	f := newAlertFixture(t)
	_, u := f.caller(t, 1, "Uploader", 30, user.CentralAlertPrefs{SharePosts: true}, true)
	a := &uploadPostAlerts{}
	for i := range maxUploadPostAlerts + 4 {
		id := 100 + i
		f.fake.areaNets[id] = "testnet"
		a.posted(&message.MessageArea{ID: id, Tag: fmt.Sprintf("fel.area%d", i)}, false)
	}
	// An API upload: no session.
	f.e.sendUploadPostAlerts(f.um, nil, u, a)
	if len(f.fake.alerts) != maxUploadPostAlerts {
		t.Errorf("sent %d alerts, want %d", len(f.fake.alerts), maxUploadPostAlerts)
	}
}

// QWKAPIPostAlerts gathers an API upload's replies and sends their alerts
// only when the upload is done.
func TestQWKAPIPostAlerts(t *testing.T) {
	f := newAlertFixture(t)
	_, u := f.caller(t, 1, "Uploader", 30, user.CentralAlertPrefs{SharePosts: true}, true)
	posted, done := f.e.QWKAPIPostAlerts(f.um)(u)
	posted(uploadTestArea(t, f, "fel.general"), false)
	if len(f.fake.alerts) != 0 {
		t.Fatal("alert sent before the upload was done")
	}
	done()
	if len(f.fake.alerts) != 1 || f.fake.alerts[0].area != "fel.general" {
		t.Errorf("sent %+v", f.fake.alerts)
	}
}

// uploadTestArea returns the fixture's area with tag.
func uploadTestArea(t *testing.T, f *alertFixture, tag string) *message.MessageArea {
	t.Helper()
	a, ok := f.e.MessageMgr.GetAreaByTag(tag)
	if !ok {
		t.Fatalf("no area %s", tag)
	}
	return a
}
