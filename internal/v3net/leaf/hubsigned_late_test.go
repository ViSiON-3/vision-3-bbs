package leaf

import "testing"

// A record first set on a retired leaf reaches its replacement, whichever comes
// first, along a chain of them, and is stored there. The sysop's clear, on the
// leaf or anywhere along the chain, stops it (#824).
func TestLateHubSignedHandOff(t *testing.T) {
	cleared := Pins{HubSignedClears: 1, HubSignedEdited: true}
	retireAndHand := func(from, to *Leaf, p Pins) {
		from.Retire(p)
		from.HandLateRepliesTo(to)
	}
	cases := []struct {
		name string
		run  func(old, mid, last *Leaf)
		want bool
	}{
		{name: "handed on before the reply", want: true, run: func(old, _, last *Leaf) {
			retireAndHand(old, last, Pins{})
			old.noteHubSigned()
		}},
		{name: "reply before it is handed on", want: true, run: func(old, _, last *Leaf) {
			old.Retire(Pins{})
			old.noteHubSigned()
			old.HandLateRepliesTo(last)
		}},
		{name: "along a chain", want: true, run: func(old, mid, last *Leaf) {
			retireAndHand(old, mid, Pins{})
			retireAndHand(mid, last, Pins{})
			old.noteHubSigned()
		}},
		{name: "cleared when retired", run: func(old, _, last *Leaf) {
			retireAndHand(old, last, cleared)
			old.noteHubSigned()
		}},
		{name: "cleared when the next one retired", run: func(old, mid, last *Leaf) {
			retireAndHand(old, mid, Pins{})
			retireAndHand(mid, last, cleared)
			old.noteHubSigned()
		}},
		{name: "cleared in place on the replacement", run: func(old, _, last *Leaf) {
			retireAndHand(old, last, Pins{})
			last.UpdatePins(nil, cleared) //nolint:staticcheck // nil ctx: the leaf is not running
			old.noteHubSigned()
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			old, _ := setupLeaf(t, "http://unused.invalid", &mockJAMWriter{})
			mid, _ := setupLeaf(t, "http://unused.invalid", &mockJAMWriter{})
			last, _ := setupLeaf(t, "http://unused.invalid", &mockJAMWriter{})
			rec := &fileRecorder{}
			rec.attach(last)

			c.run(old, mid, last)
			if last.HubSigned() != c.want || rec.file.Load() != c.want {
				t.Errorf("hubSigned=%v stored=%v, want %v", last.HubSigned(), rec.file.Load(), c.want)
			}
		})
	}
}
