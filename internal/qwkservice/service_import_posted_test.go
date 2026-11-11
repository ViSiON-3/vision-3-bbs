package qwkservice

import (
	"fmt"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
)

// Posted hears of every reply that was saved, with its area and whether it
// went in as private mail, and of none that was skipped or failed.
func TestImportREP_PostedReportsSavedReplies(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	store.addArea(&message.MessageArea{ID: 2, Tag: "BROKEN", Name: "Broken"})
	store.addArea(&message.MessageArea{ID: 3, Tag: "PRIVMAIL", Name: "Private Mail"})
	store.addErr[2] = fmt.Errorf("disk full")

	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 1, Number: 1, To: "All", Subject: "One", DateTime: time.Now(), Body: "a"},
		{Conference: 2, Number: 2, To: "All", Subject: "Fails", DateTime: time.Now(), Body: "b"},
		{Conference: 99, Number: 3, To: "All", Subject: "Unknown", DateTime: time.Now(), Body: "c"},
		{Conference: 0, Number: 4, To: "friend", Subject: "Private", DateTime: time.Now(), Body: "d"},
		{Conference: 1, Number: 5, To: "All", Subject: "Two", DateTime: time.Now(), Body: "e"},
	})

	type report struct {
		tag     string
		private bool
	}
	var got []report
	svc := newTestService(t, store)
	res, err := svc.ImportREP(rep, ImportOptions{
		Handle: "tester",
		Posted: func(a *message.MessageArea, private bool) { got = append(got, report{a.Tag, private}) },
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []report{{"GENERAL", false}, {"PRIVMAIL", true}, {"GENERAL", false}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Posted saw %v, want %v", got, want)
	}
	if res.Posted != len(want) {
		t.Errorf("result %+v does not match %d reports", res, len(want))
	}
}
