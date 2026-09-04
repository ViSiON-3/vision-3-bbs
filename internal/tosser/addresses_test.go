package tosser

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// pointTosser builds the minimal Tosser resolveOrigAddr needs: its own address,
// used only as the last-resort zone.
func pointTosser(t *testing.T) *Tosser {
	t.Helper()
	addr, err := jam.ParseAddress("21:4/158.1")
	if err != nil {
		t.Fatalf("ParseAddress: %v", err)
	}
	return &Tosser{ownAddr: addr}
}

func TestResolveOrigAddrRecoversPoint(t *testing.T) {
	// A hub relaying a point's echomail: the packet is the hub's, the packed
	// message header carries the boss node's net/node, and only the MSGID and
	// origin line know the author is a point.
	hubPkt := &ftn.PacketHeader{OrigZone: 21, OrigNet: 4, OrigNode: 100}
	authored := &ftn.PackedMessage{OrigNet: 4, OrigNode: 158}

	tests := []struct {
		name   string
		pktHdr *ftn.PacketHeader
		msg    *ftn.PackedMessage
		parsed *ftn.ParsedBody
		msgID  string
		want   string
	}{
		{
			name:   "msgid names the point",
			pktHdr: hubPkt,
			msg:    authored,
			parsed: &ftn.ParsedBody{},
			msgID:  "21:4/158.1 1a2b3c4d",
			want:   "21:4/158.1",
		},
		{
			name:   "origin line names the point",
			pktHdr: hubPkt,
			msg:    authored,
			parsed: &ftn.ParsedBody{Text: "Hello\r * Origin: My BBS (21:4/158.1)\r"},
			want:   "21:4/158.1",
		},
		{
			name:   "fmpt kludge names the point",
			pktHdr: hubPkt,
			msg:    authored,
			parsed: &ftn.ParsedBody{Kludges: []string{"FMPT 7"}},
			want:   "21:4/158.7",
		},
		{
			name:   "packet header names the point when the point sent the packet",
			pktHdr: &ftn.PacketHeader{OrigZone: 21, OrigNet: 4, OrigNode: 158, OrigPoint: 1},
			msg:    authored,
			parsed: &ftn.ParsedBody{},
			want:   "21:4/158.1",
		},
		{
			name:   "node author stays 3D",
			pktHdr: hubPkt,
			msg:    authored,
			parsed: &ftn.ParsedBody{Text: "Hello\r * Origin: My BBS (21:4/158)\r"},
			msgID:  "21:4/158 1a2b3c4d",
			want:   "21:4/158",
		},
		{
			name:   "three dimensional msgid wins over an origin point",
			pktHdr: hubPkt,
			msg:    authored,
			parsed: &ftn.ParsedBody{Text: "Hello\r * Origin: My BBS (21:4/158.9)\r"},
			msgID:  "21:4/158 1a2b3c4d",
			want:   "21:4/158",
		},
		{
			name:   "msgid zone wins over the packet zone",
			pktHdr: hubPkt,
			msg:    authored,
			parsed: &ftn.ParsedBody{},
			msgID:  "1:4/158.1 1a2b3c4d",
			want:   "1:4/158.1",
		},
		{
			name:   "netmail fmpt fills in a point-less msgid",
			pktHdr: hubPkt,
			msg:    authored,
			parsed: &ftn.ParsedBody{Kludges: []string{"FMPT 1"}},
			msgID:  "21:4/158 1a2b3c4d",
			want:   "21:4/158.1",
		},
		{
			name:   "netmail fmpt does not override a point the msgid names",
			pktHdr: hubPkt,
			msg:    authored,
			parsed: &ftn.ParsedBody{Kludges: []string{"FMPT 9"}},
			msgID:  "21:4/158.1 1a2b3c4d",
			want:   "21:4/158.1",
		},
		{
			name:   "echomail ignores an fmpt a transit system left behind",
			pktHdr: hubPkt,
			msg:    authored,
			parsed: &ftn.ParsedBody{Area: "FSX_GEN", Kludges: []string{"FMPT 3"}},
			msgID:  "21:4/158 1a2b3c4d",
			want:   "21:4/158",
		},
		{
			name:   "echomail ignores a stray fmpt with nothing else to go on",
			pktHdr: hubPkt,
			msg:    authored,
			parsed: &ftn.ParsedBody{Area: "FSX_GEN", Kludges: []string{"FMPT 3"}},
			want:   "21:4/158",
		},
		{
			name:   "relaying point's packet does not stamp its point on the author",
			pktHdr: &ftn.PacketHeader{OrigZone: 21, OrigNet: 4, OrigNode: 100, OrigPoint: 3},
			msg:    authored,
			parsed: &ftn.ParsedBody{},
			want:   "21:4/158",
		},
		{
			name:   "another node's msgid is ignored",
			pktHdr: hubPkt,
			msg:    authored,
			parsed: &ftn.ParsedBody{},
			msgID:  "21:4/999.2 1a2b3c4d",
			want:   "21:4/158",
		},
		{
			name:   "at-style msgid falls through without a point",
			pktHdr: hubPkt,
			msg:    authored,
			parsed: &ftn.ParsedBody{},
			msgID:  "1a2b3c4d@fidonet.org 1a2b3c4d",
			want:   "21:4/158",
		},
		{
			name:   "msgid wins over a stale origin line",
			pktHdr: hubPkt,
			msg:    authored,
			parsed: &ftn.ParsedBody{Text: " * Origin: My BBS (21:4/158.9)\r"},
			msgID:  "21:4/158.1 1a2b3c4d",
			want:   "21:4/158.1",
		},
		{
			name:   "zone falls back to the qmail field",
			pktHdr: &ftn.PacketHeader{QOrigZone: 21, OrigNet: 4, OrigNode: 100},
			msg:    authored,
			parsed: &ftn.ParsedBody{},
			msgID:  "21:4/158.1 1a2b3c4d",
			want:   "21:4/158.1",
		},
		{
			name:   "zone falls back to our own when the packet carries none",
			pktHdr: &ftn.PacketHeader{OrigNet: 4, OrigNode: 100},
			msg:    authored,
			parsed: &ftn.ParsedBody{},
			want:   "21:4/158",
		},
	}

	tos := pointTosser(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tos.resolveOrigAddr(tt.pktHdr, tt.msg, tt.parsed, tt.msgID)
			if got != tt.want {
				t.Errorf("resolveOrigAddr = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveOrigAddrTakesZoneFromINTL(t *testing.T) {
	// Netmail gated between zones: the packet header carries the gate's zone,
	// INTL carries the author's.
	tos := pointTosser(t)
	got := tos.resolveOrigAddr(
		&ftn.PacketHeader{OrigZone: 21, OrigNet: 4, OrigNode: 100},
		&ftn.PackedMessage{OrigNet: 105, OrigNode: 5, DestNet: 4, DestNode: 158},
		&ftn.ParsedBody{Kludges: []string{"INTL 21:4/158 1:105/5", "FMPT 2"}},
		"",
	)
	if want := "1:105/5.2"; got != want {
		t.Errorf("resolveOrigAddr = %q, want %q", got, want)
	}
}

func TestResolveDestAddr(t *testing.T) {
	tests := []struct {
		name   string
		pktHdr *ftn.PacketHeader
		msg    *ftn.PackedMessage
		parsed *ftn.ParsedBody
		want   string
	}{
		{
			name:   "plain node destination",
			pktHdr: &ftn.PacketHeader{OrigZone: 21, DestZone: 21, DestNet: 4, DestNode: 158},
			msg:    &ftn.PackedMessage{OrigNet: 4, OrigNode: 100, DestNet: 4, DestNode: 158},
			parsed: &ftn.ParsedBody{},
			want:   "21:4/158",
		},
		{
			name:   "topt names the destination point",
			pktHdr: &ftn.PacketHeader{DestZone: 21, DestNet: 4, DestNode: 158},
			msg:    &ftn.PackedMessage{OrigNet: 4, OrigNode: 100, DestNet: 4, DestNode: 158},
			parsed: &ftn.ParsedBody{Kludges: []string{"TOPT 1"}},
			want:   "21:4/158.1",
		},
		{
			name:   "packet header names the point when addressed straight to us",
			pktHdr: &ftn.PacketHeader{DestZone: 21, DestNet: 4, DestNode: 158, DestPoint: 1},
			msg:    &ftn.PackedMessage{OrigNet: 4, OrigNode: 100, DestNet: 4, DestNode: 158},
			parsed: &ftn.ParsedBody{},
			want:   "21:4/158.1",
		},
		{
			name:   "packet routed via a point does not stamp its point on the addressee",
			pktHdr: &ftn.PacketHeader{DestZone: 21, DestNet: 4, DestNode: 100, DestPoint: 3},
			msg:    &ftn.PackedMessage{OrigNet: 4, OrigNode: 100, DestNet: 4, DestNode: 158},
			parsed: &ftn.ParsedBody{},
			want:   "21:4/158",
		},
		{
			name:   "intl names the destination zone",
			pktHdr: &ftn.PacketHeader{DestZone: 21, DestNet: 280, DestNode: 1},
			msg:    &ftn.PackedMessage{OrigNet: 105, OrigNode: 5, DestNet: 280, DestNode: 1},
			parsed: &ftn.ParsedBody{Kludges: []string{"INTL 2:280/1 1:105/5"}},
			want:   "2:280/1",
		},
		{
			name:   "zone falls back to the qmail field",
			pktHdr: &ftn.PacketHeader{QDestZone: 21, DestNet: 4, DestNode: 158},
			msg:    &ftn.PackedMessage{OrigNet: 4, OrigNode: 100, DestNet: 4, DestNode: 158},
			parsed: &ftn.ParsedBody{},
			want:   "21:4/158",
		},
		{
			name:   "zone falls back to our own when the packet carries none",
			pktHdr: &ftn.PacketHeader{DestNet: 4, DestNode: 158},
			msg:    &ftn.PackedMessage{OrigNet: 4, OrigNode: 100, DestNet: 4, DestNode: 158},
			parsed: &ftn.ParsedBody{},
			want:   "21:4/158",
		},
		{
			name:   "echomail carries no destination",
			pktHdr: &ftn.PacketHeader{DestZone: 21, DestNet: 4, DestNode: 158},
			msg:    &ftn.PackedMessage{OrigNet: 4, OrigNode: 100},
			parsed: &ftn.ParsedBody{Area: "FSX_GEN"},
			want:   "",
		},
	}

	tos := pointTosser(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tos.resolveDestAddr(tt.pktHdr, tt.msg, tt.parsed)
			if got != tt.want {
				t.Errorf("resolveDestAddr = %q, want %q", got, tt.want)
			}
		})
	}
}
