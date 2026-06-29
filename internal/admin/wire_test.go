package admin

import (
	"bytes"
	"testing"
	"time"
)

func TestWireRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	out := []*Frame{
		{Kind: KindSnapshot, Snapshot: &SystemSnapshot{SystemName: "T", Time: time.Unix(1, 0).UTC()}},
		{Kind: KindEvent, Event: &Event{Type: EventMenuChanged, NodeID: 3, Message: "X"}},
		{Kind: KindResult, Result: &Result{OK: true}},
	}
	for _, f := range out {
		if err := WriteFrame(&buf, f); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	for i := range out {
		got, err := ReadFrame(&buf)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if got.Kind != out[i].Kind {
			t.Fatalf("frame %d kind mismatch: %q vs %q", i, got.Kind, out[i].Kind)
		}
	}
}
