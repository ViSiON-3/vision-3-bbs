package menu

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

func TestReplyAddressee(t *testing.T) {
	netmail := &message.MessageArea{AreaType: "netmail", OriginAddr: "21:4/158"}

	tests := []struct {
		desc     string
		area     *message.MessageArea
		msg      *message.DisplayMessage
		wantName string
		wantTo   string
	}{
		{
			desc:     "local area replies to the bare name",
			area:     &message.MessageArea{AreaType: "local"},
			msg:      &message.DisplayMessage{From: "Bob", To: "Alice"},
			wantName: "Bob",
			wantTo:   "Bob",
		},
		{
			desc:     "echomail keeps the name even though it has an origin",
			area:     &message.MessageArea{AreaType: "echomail", OriginAddr: "21:4/158"},
			msg:      &message.DisplayMessage{From: "Bob", To: "All", OrigAddr: "21:1/100"},
			wantName: "Bob",
			wantTo:   "Bob",
		},
		{
			desc:     "inbound netmail is addressed back to its sender",
			area:     netmail,
			msg:      &message.DisplayMessage{From: "Bob", To: "Alice", OrigAddr: "21:1/100", DestAddr: "21:4/158"},
			wantName: "Bob",
			wantTo:   "Bob@21:1/100",
		},
		{
			desc:     "a point sender keeps its point number",
			area:     netmail,
			msg:      &message.DisplayMessage{From: "Bob", To: "Alice", OrigAddr: "21:1/100.5", DestAddr: "21:4/158"},
			wantName: "Bob",
			wantTo:   "Bob@21:1/100.5",
		},
		{
			desc:     "a reply to netmail we sent follows the parent's addressee",
			area:     netmail,
			msg:      &message.DisplayMessage{From: "Alice", To: "Bob", OrigAddr: "21:4/158", DestAddr: "21:1/100"},
			wantName: "Bob",
			wantTo:   "Bob@21:1/100",
		},
		{
			desc:     "netmail with no origin address falls back to the name",
			area:     netmail,
			msg:      &message.DisplayMessage{From: "Bob", To: "Alice"},
			wantName: "Bob",
			wantTo:   "Bob",
		},
		{
			desc:     "a missing area is treated as non-netmail",
			area:     nil,
			msg:      &message.DisplayMessage{From: "Bob", To: "Alice", OrigAddr: "21:1/100"},
			wantName: "Bob",
			wantTo:   "Bob",
		},
	}

	for _, tt := range tests {
		name, to := replyAddressee(tt.area, tt.msg)
		if name != tt.wantName || to != tt.wantTo {
			t.Errorf("%s: replyAddressee = (%q, %q), want (%q, %q)",
				tt.desc, name, to, tt.wantName, tt.wantTo)
		}
	}
}
