package menu

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// V3Net Central post alerts for replies uploaded in a QWK packet (#996). A
// packet can hold hundreds of replies, so it does not send an alert per
// reply: it sends one per V3Net area it posted to, after the upload is
// done, and at most maxUploadPostAlerts of them. Private mail is never
// announced, and the same consent applies as for a post in the editor:
// the uploader's stored record must share posts.

// maxUploadPostAlerts is the most post alerts one uploaded packet sends.
const maxUploadPostAlerts = 5

// uploadPostAlerts gathers the areas one upload posted to, in the order it
// first posted to each.
type uploadPostAlerts struct {
	seen  map[int]bool
	areas []*message.MessageArea
}

// posted records a saved reply; it is an ImportOptions.Posted.
func (a *uploadPostAlerts) posted(area *message.MessageArea, private bool) {
	if private || area == nil || a.seen[area.ID] {
		return
	}
	if a.seen == nil {
		a.seen = make(map[int]bool)
	}
	a.seen[area.ID] = true
	a.areas = append(a.areas, area)
}

// send announces the upload's V3Net areas for u, at most
// maxUploadPostAlerts of them. sess is the uploader's session, or nil for
// an upload through the QWK API.
func (e *MenuExecutor) sendUploadPostAlerts(um *user.UserMgr, sess *session.BbsSession, u *user.User, a *uploadPostAlerts) {
	if e.V3NetCentral == nil || u == nil {
		return
	}
	sent := 0
	for _, area := range a.areas {
		if sent >= maxUploadPostAlerts {
			return
		}
		network := e.V3NetCentral.NetworkForArea(area.ID)
		if network == "" || !protocol.AreaTagRegexp.MatchString(area.Tag) {
			continue
		}
		e.sendCentralAlert(um, sess, u, network, protocol.CentralAlertPost, "", area.Tag)
		sent++
	}
}

// QWKAPIPostAlerts is the QWK API's PostedFor: it gathers what an upload
// by u posted and, when the upload is done, sends its V3Net Central post
// alerts as an upload from the BBS does.
func (e *MenuExecutor) QWKAPIPostAlerts(um *user.UserMgr) func(u *user.User) (func(*message.MessageArea, bool), func()) {
	return func(u *user.User) (func(*message.MessageArea, bool), func()) {
		a := &uploadPostAlerts{}
		return a.posted, func() { e.sendUploadPostAlerts(um, nil, u, a) }
	}
}
