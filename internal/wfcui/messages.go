package wfcui

import "github.com/ViSiON-3/vision-3-bbs/internal/admin"

// snapshotMsg carries a freshly fetched snapshot.
// fromPoll is true only when produced by pollCmd (the self-sustaining tick
// loop); it is false for one-shot fetches (Init's initial paint and the R-key
// manual refresh). The Update handler only re-arms pollCmd when fromPoll is
// true, ensuring exactly one poll chain is ever active.
type snapshotMsg struct {
	snap     *admin.SystemSnapshot
	fromPoll bool
}
type eventMsg struct{ ev admin.Event }
type errMsg struct{ err error }
type subscribedMsg struct {
	ch  <-chan admin.Event
	err error
}
