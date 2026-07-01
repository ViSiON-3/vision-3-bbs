package qwkservice

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
)

// fakeStore is an in-memory MessageStore shared by the service tests.
type fakeStore struct {
	areas    []*message.MessageArea
	byTag    map[string]*message.MessageArea
	byID     map[int]*message.MessageArea
	msgs     map[int][]*message.DisplayMessage // areaID -> 1-based messages
	lastRead map[string]int                    // "areaID/user" -> num

	posted     []postedMessage
	privPosted []postedMessage
	setReads   []setRead
	addErr     map[int]error // areaID -> error to return from AddReply/AddPrivateReply
}

type postedMessage struct {
	areaID                       int
	from, to, subject, body, rep string
	replyToNum                   int
}

type setRead struct {
	areaID int
	user   string
	num    int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		byTag:    map[string]*message.MessageArea{},
		byID:     map[int]*message.MessageArea{},
		msgs:     map[int][]*message.DisplayMessage{},
		lastRead: map[string]int{},
		addErr:   map[int]error{},
	}
}

func (f *fakeStore) addArea(a *message.MessageArea) {
	f.areas = append(f.areas, a)
	f.byTag[a.Tag] = a
	f.byID[a.ID] = a
}

func (f *fakeStore) ListAreas() []*message.MessageArea { return f.areas }

func (f *fakeStore) GetAreaByTag(tag string) (*message.MessageArea, bool) {
	a, ok := f.byTag[tag]
	return a, ok
}

func (f *fakeStore) GetAreaByID(id int) (*message.MessageArea, bool) {
	a, ok := f.byID[id]
	return a, ok
}

func (f *fakeStore) GetLastRead(areaID int, username string) (int, error) {
	return f.lastRead[key(areaID, username)], nil
}

func (f *fakeStore) SetLastRead(areaID int, username string, msgNum int) error {
	f.setReads = append(f.setReads, setRead{areaID, username, msgNum})
	f.lastRead[key(areaID, username)] = msgNum
	return nil
}

func (f *fakeStore) GetMessageCountForArea(areaID int) (int, error) {
	return len(f.msgs[areaID]), nil
}

func (f *fakeStore) GetMessage(areaID, msgNum int) (*message.DisplayMessage, error) {
	list := f.msgs[areaID]
	if msgNum < 1 || msgNum > len(list) {
		return nil, fmt.Errorf("no message %d in area %d", msgNum, areaID)
	}
	return list[msgNum-1], nil
}

func (f *fakeStore) AddReply(areaID int, from, to, subject, body, replyToMsgID string, replyToNum int) (int, error) {
	if err := f.addErr[areaID]; err != nil {
		return 0, err
	}
	f.posted = append(f.posted, postedMessage{areaID, from, to, subject, body, replyToMsgID, replyToNum})
	return len(f.posted), nil
}

func (f *fakeStore) AddPrivateReply(areaID int, from, to, subject, body, replyToMsgID string, replyToNum int) (int, error) {
	if err := f.addErr[areaID]; err != nil {
		return 0, err
	}
	f.privPosted = append(f.privPosted, postedMessage{areaID, from, to, subject, body, replyToMsgID, replyToNum})
	return len(f.privPosted), nil
}

func key(areaID int, user string) string { return fmt.Sprintf("%d/%s", areaID, user) }

// newTestService builds a Service backed by the fake store, with the conference
// map persisted under a per-test temp dir.
func newTestService(t *testing.T, store *fakeStore) *Service {
	t.Helper()
	return New(store, "VISION3", "ViSiON/3 BBS", "SysOp", t.TempDir())
}

func (f *fakeStore) seed(areaID int, msgs ...*message.DisplayMessage) {
	f.msgs[areaID] = append(f.msgs[areaID], msgs...)
}

// dm builds a DisplayMessage for seeding the fake store.
func dm(num int, from, to, subj, body string) *message.DisplayMessage {
	return &message.DisplayMessage{
		MsgNum: num, From: from, To: to, Subject: subj, Body: body,
		DateTime: time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC),
	}
}

// makeREP builds the bytes of a REP packet for the given messages.
func makeREP(t *testing.T, bbsID string, msgs []qwk.PacketMessage) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := qwk.WriteREP(&buf, bbsID, msgs); err != nil {
		t.Fatalf("WriteREP: %v", err)
	}
	return buf.Bytes()
}
