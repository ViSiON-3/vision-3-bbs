package qwkservice

import (
	"bytes"
	"slices"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestBuildPacketTwitListBeforeCapAndPrivacy(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	store.addArea(&message.MessageArea{ID: 2, Tag: "PRIVMAIL", Name: "Mail"})
	originMatch := dm(2, "Qualified", "All", "hidden-origin", "body")
	originMatch.OrigAddr = "21:1/100"
	originOther := dm(3, "Qualified", "All", "other-origin", "body")
	originOther.OrigAddr = "21:1/101"
	store.seed(1, dm(1, "Remote", "All", "hidden", "body"), originMatch, originOther, dm(4, "Anonymous", "All", "anonymous", "body"), privMsg(5, "Other", "SomeoneElse"), dm(6, "Visible", "All", "visible", "body"))
	store.seed(2, privMsg(1, "Remote", "tester"), privMsg(2, "Other", "tester"), privMsg(3, "Other", "SomeoneElse"))
	matcher := user.BuildTwitMatcher([]user.TwitEntry{{Name: "Remote"}, {Name: "Qualified", Addr: "21:1/100"}, {Name: "Anonymous"}}, nil, user.TwitPolicy{}, "")
	res, err := newTestService(t, store).BuildPacket(ExportOptions{Handle: "tester", TaggedTags: []string{"GENERAL", "PRIVMAIL"}, MaxPerArea: 2, TwitList: matcher})
	if err != nil {
		t.Fatal(err)
	}
	if res.MessageCount != 3 {
		t.Fatalf("count=%d want 3", res.MessageCount)
	}
	pkt, err := qwk.ReadPacket(bytes.NewReader(res.Packet), int64(len(res.Packet)))
	if err != nil {
		t.Fatal(err)
	}
	var numbers []int
	for _, m := range pkt.Messages {
		numbers = append(numbers, m.Number)
		if m.From == "Remote" {
			t.Fatal("twit exported")
		}
	}
	if !slices.Equal(numbers, []int{3, 4, 2}) {
		t.Fatalf("packet messages=%v", numbers)
	}
	if !slices.Equal(res.LastRead, []LastReadUpdate{{AreaID: 1, MsgNum: 4}, {AreaID: 2, MsgNum: 2}}) {
		t.Fatalf("lastread=%v", res.LastRead)
	}
	if len(store.setReads) != 0 {
		t.Fatal("pointers committed before transfer")
	}
}
func TestBuildPacketAllTwitsProducesNoPacket(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL"})
	store.seed(1, dm(1, "Remote", "All", "hidden", "body"))
	matcher := user.BuildTwitMatcher([]user.TwitEntry{{Name: "Remote"}}, nil, user.TwitPolicy{}, "")
	res, err := newTestService(t, store).BuildPacket(ExportOptions{Handle: "tester", TwitList: matcher})
	if err != nil || res.MessageCount != 0 || len(res.Packet) != 0 || len(res.LastRead) != 0 {
		t.Fatalf("result=%+v err=%v", res, err)
	}
}
