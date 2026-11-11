package v3net

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/leaf"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// One event that reaches a board on two networks is handed to the sink
// once; another board's alert with the same ID is its own event.
func TestServiceAlertSinkOncePerEvent(t *testing.T) {
	s := &Service{}
	var got []protocol.CentralAlert
	s.receiveAlert(protocol.CentralAlert{NodeID: "a", ID: "x"}) // no sink yet: dropped, not remembered
	s.SetCentralAlertSink(func(a protocol.CentralAlert) { got = append(got, a) })
	s.receiveAlert(protocol.CentralAlert{NodeID: "a", ID: "x", Network: "one"})
	s.receiveAlert(protocol.CentralAlert{NodeID: "a", ID: "x", Network: "two"})
	s.receiveAlert(protocol.CentralAlert{NodeID: "b", ID: "x", Network: "one"})
	if len(got) != 2 || got[0].Network != "one" || got[1].NodeID != "b" {
		t.Errorf("sink got %+v", got)
	}
}

// The dedup forgets the oldest alerts once full, not the newest.
func TestAlertDedupBounded(t *testing.T) {
	var d alertDedup
	for i := range serviceAlertsSeen + 1 {
		d.claim("n", string(rune(0x4e00+i)))
	}
	if len(d.seen) != serviceAlertsSeen || len(d.order) != serviceAlertsSeen {
		t.Fatalf("kept %d/%d", len(d.seen), len(d.order))
	}
	if !d.claim("n", string(rune(0x4e00))) {
		t.Error("oldest still remembered")
	}
	if d.claim("n", string(rune(0x4e00+serviceAlertsSeen))) {
		t.Error("newest forgotten")
	}
}

// With no running leaf, or none whose sysop has the kind on, nothing is
// sent.
func TestSendCentralAlertNoLeaves(t *testing.T) {
	s := &Service{}
	if n := s.SendCentralAlert("", leaf.AlertSpec{Kind: protocol.CentralAlertLogon, Handle: "Alice"}); n != 0 {
		t.Errorf("handed to %d leaves", n)
	}
}
