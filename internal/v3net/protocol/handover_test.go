package protocol_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

var handoverIssued = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

// The signed payload is the documented string, so other implementations can
// build it from the docs.
func TestCoordHandoverSignedPayloadFormat(t *testing.T) {
	h := protocol.CoordHandover{
		Network:      "felonynet",
		OldKeyB64:    "b2xk",
		NewKeyB64:    "bmV3",
		Issued:       "2026-10-29T12:00:00Z",
		SignatureB64: "not covered",
	}
	got, err := h.SignedPayload()
	if err != nil {
		t.Fatal(err)
	}
	want := "V3NET-COORD-HANDOVER-v1\n" +
		`{"issued":"2026-10-29T12:00:00Z","network":"felonynet","new_key":"bmV3","old_key":"b2xk"}`
	if string(got) != want {
		t.Errorf("payload = %q, want %q", got, want)
	}
}

// A handover signed by the old key verifies, also after a JSON round trip.
func TestCoordHandoverRoundTrip(t *testing.T) {
	oldKey, newKey := loadKey(t, "old.key"), loadKey(t, "new.key")
	h, err := protocol.NewCoordHandover(oldKey, "felonynet", newKey.PubKeyBase64(), handoverIssued)
	if err != nil {
		t.Fatalf("NewCoordHandover: %v", err)
	}
	if h.OldKeyB64 != oldKey.PubKeyBase64() || h.NewKeyB64 != newKey.PubKeyBase64() || h.Issued != "2026-10-29T12:00:00Z" {
		t.Errorf("handover = %+v", h)
	}
	data, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	var back protocol.CoordHandover
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if err := back.Verify(); err != nil {
		t.Errorf("Verify after round trip: %v", err)
	}
}

// Changing any signed field, or the signature, breaks the handover; so do
// invalid keys and a handover to the same key.
func TestCoordHandoverTampered(t *testing.T) {
	oldKey, newKey, other := loadKey(t, "old.key"), loadKey(t, "new.key"), loadKey(t, "other.key")
	good, err := protocol.NewCoordHandover(oldKey, "felonynet", newKey.PubKeyBase64(), handoverIssued)
	if err != nil {
		t.Fatal(err)
	}
	otherSigned, err := protocol.NewCoordHandover(other, "felonynet", newKey.PubKeyBase64(), handoverIssued)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		change func(h *protocol.CoordHandover)
		want   error
	}{
		{"other network", func(h *protocol.CoordHandover) { h.Network = "othernet" }, protocol.ErrHandoverSignature},
		{"other new key", func(h *protocol.CoordHandover) { h.NewKeyB64 = other.PubKeyBase64() }, protocol.ErrHandoverSignature},
		{"other old key", func(h *protocol.CoordHandover) { h.OldKeyB64 = other.PubKeyBase64() }, protocol.ErrHandoverSignature},
		{"other issued", func(h *protocol.CoordHandover) { h.Issued = "2026-10-30T12:00:00Z" }, protocol.ErrHandoverSignature},
		{"signed by another key", func(h *protocol.CoordHandover) { h.SignatureB64 = otherSigned.SignatureB64 }, protocol.ErrHandoverSignature},
		{"no signature", func(h *protocol.CoordHandover) { h.SignatureB64 = "" }, protocol.ErrHandoverSignature},
		{"bad new key", func(h *protocol.CoordHandover) { h.NewKeyB64 = "bmV3" }, protocol.ErrHandoverKey},
		{"same key", func(h *protocol.CoordHandover) { h.NewKeyB64 = h.OldKeyB64 }, protocol.ErrHandoverKey},
		{"bad issued", func(h *protocol.CoordHandover) { h.Issued = "yesterday" }, protocol.ErrHandoverIssued},
	}
	for _, tc := range cases {
		h := *good
		tc.change(&h)
		if err := h.Verify(); !errors.Is(err, tc.want) {
			t.Errorf("%s: Verify = %v, want %v", tc.name, err, tc.want)
		}
	}

	if _, err := protocol.NewCoordHandover(oldKey, "felonynet", oldKey.PubKeyBase64(), handoverIssued); !errors.Is(err, protocol.ErrHandoverKey) {
		t.Errorf("NewCoordHandover to its own key = %v, want ErrHandoverKey", err)
	}
}
