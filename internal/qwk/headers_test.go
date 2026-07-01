package qwk

import (
	"testing"
	"time"
)

func TestSynthMessageID(t *testing.T) {
	if got := synthMessageID("VISION3", 1, 2); got != "<2.1@vision3>" {
		t.Errorf("synthMessageID = %q, want <2.1@vision3>", got)
	}
}

func TestSmbTimezone(t *testing.T) {
	if got := smbTimezone(-8 * 3600); got != "21e0" { // 480 min west
		t.Errorf("smbTimezone(-0800) = %q, want 21e0", got)
	}
	if got := smbTimezone(1 * 3600); got != "103c" { // 60 min east
		t.Errorf("smbTimezone(+0100) = %q, want 103c", got)
	}
}

func TestHeadersDAT_RoundTrip(t *testing.T) {
	in := []ExtHeader{
		{Offset: 384, MessageID: "<2.1@v>", Subject: "Second", To: "Bob", From: "Al", WhenWritten: "20260305150000-0800  21e0"},
		{Offset: 128, MessageID: "<1.1@v>", Subject: "First: with a colon", To: "Al", From: "Bob", WhenWritten: "20260305143000-0800  21e0"},
	}
	data := encodeHeadersDAT(in)
	out := parseHeadersDAT(data)

	if len(out) != 2 {
		t.Fatalf("want 2 sections, got %d", len(out))
	}
	h := out[128]
	if h.Subject != "First: with a colon" {
		t.Errorf("subject with colon not preserved: %q", h.Subject)
	}
	if h.To != "Al" || h.From != "Bob" || h.MessageID != "<1.1@v>" {
		t.Errorf("fields not preserved: %+v", h)
	}
	if _, ok := out[384]; !ok {
		t.Error("second section (offset 384) missing")
	}
}

func TestParseHeadersDAT_LenientOnGarbage(t *testing.T) {
	if got := parseHeadersDAT(nil); len(got) != 0 {
		t.Errorf("nil input: want empty map, got %v", got)
	}
	if got := parseHeadersDAT([]byte("not ini\nmore junk\n")); len(got) != 0 {
		t.Errorf("garbage input: want empty map, got %v", got)
	}
}

func TestFormatWhenWritten(t *testing.T) {
	tt := time.Date(2026, 3, 5, 14, 30, 0, 0, time.FixedZone("PST", -8*3600))
	if got := formatWhenWritten(tt); got != "20260305143000-0800  21e0" {
		t.Errorf("formatWhenWritten = %q, want 20260305143000-0800  21e0", got)
	}
}
