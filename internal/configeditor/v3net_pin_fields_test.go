package configeditor

import (
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"testing"
)

func TestV3NetPinFieldValidatesAndClearsPending(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString(pub)
	pin, pending := "old", key
	f := v3netPinField("Coord Key", "help", 9, &pin, &pending)

	if err := f.Set("not a key"); err == nil {
		t.Error("accepted a value that is not a base64 ed25519 key")
	}
	if pin != "old" || pending != key {
		t.Errorf("a rejected value changed state: pin %q, pending %q", pin, pending)
	}

	if err := f.Set("  " + key + " "); err != nil {
		t.Fatalf("Set(valid key): %v", err)
	}
	if pin != key || pending != "" {
		t.Errorf("after accepting: pin %q, pending %q; want the key and none", pin, pending)
	}

	pending = "offered"
	if err := f.Set(""); err != nil {
		t.Fatalf("Set(blank): %v", err)
	}
	if pin != "" || pending != "" {
		t.Errorf("after blanking: pin %q, pending %q; want both blank", pin, pending)
	}

	g := v3netPendingField("Offered Coord", 11, &pin, &pending)
	// Blank pin and nothing pending: before first contact, or after the
	// URL or pin was cleared. Must not read as a verified match.
	if got := g.Get(); got != "(none) - nothing pinned yet" {
		t.Errorf("blank pin, empty pending shows %q", got)
	}
	pin = key
	if got := g.Get(); got != "(none) - no changed key offered" {
		t.Errorf("pinned, empty pending shows %q", got)
	}
	pending = key
	if got := g.Get(); got != "NEW: "+key {
		t.Errorf("offered key shows %q", got)
	}
	if n := len(g.Get()); n > g.Width {
		t.Errorf("offered key display is %d chars, wider than the %d-char field", n, g.Width)
	}
}

// The offered-key help explains a read-only warning field; losing its tail
// to the two-line ellipsis would drop the "paste it into the key" step.
func TestV3NetPendingFieldHelpFitsTwoLines(t *testing.T) {
	var pin, pending string
	help := fieldHelpText(v3netPendingField("Offered Hub", 11, &pin, &pending))
	l1, l2 := wrapHelpTwoLines(help, 71) // boxW(70)+1
	if strings.HasSuffix(l2, "…") || strings.HasSuffix(l1, "…") {
		t.Errorf("help is truncated: %q | %q", l1, l2)
	}
	if got := strings.Join(strings.Fields(l1+" "+l2), " "); got != help {
		t.Errorf("help text not preserved:\n got %q\nwant %q", got, help)
	}
}
